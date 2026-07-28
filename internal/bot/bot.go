package bot

import (
	"context"
	"sync"
	"time"

	ghapp "embassy.dev/bot/internal/github"
	"embassy.dev/bot/internal/models"
	"embassy.dev/bot/toolkit/log"
	"embassy.dev/bot/toolkit/pgtypes"
	gh "github.com/google/go-github/v88/github"
	"github.com/sqlbunny/errors"
	"github.com/sqlbunny/sqlbunny/runtime/bunny"
	"github.com/sqlbunny/sqlbunny/runtime/qm"
	"github.com/sqlbunny/sqlbunny/types/null"
)

type Bot struct {
	config *Config
	app    *ghapp.App

	// See HandleEvent for why event handling is serialized.
	mu sync.Mutex
}

func New(config *Config, app *ghapp.App) *Bot {
	return &Bot{
		config: config,
		app:    app,
	}
}

// upsertRepo records the repo and, importantly, its installation id: that's the
// only way to get a token for it later from the ticker, where there's no webhook
// payload to read it from.
func (b *Bot) upsertRepo(ctx context.Context, r *gh.Repository, installationID int64) (*models.Repo, error) {
	owner, name := repoName(r)
	repo := &models.Repo{
		ID:             r.GetID(),
		InstallationID: installationID,
		Owner:          owner,
		Name:           name,
	}
	err := repo.Upsert(ctx, "(id)", map[models.RepoColumn]string{
		models.RepoColumns.InstallationID: "EXCLUDED.installation_id",
		models.RepoColumns.Owner:          "EXCLUDED.owner",
		models.RepoColumns.Name:           "EXCLUDED.name",
	})
	if err != nil {
		return nil, err
	}
	return repo, nil
}

// repoName pulls owner and name out of a repo payload. Installation events send
// a trimmed-down repo object with only full_name, no owner block.
func repoName(r *gh.Repository) (owner, name string) {
	if o := r.GetOwner().GetLogin(); o != "" {
		return o, r.GetName()
	}
	full := r.GetFullName()
	for i := range full {
		if full[i] == '/' {
			return full[:i], full[i+1:]
		}
	}
	return "", full
}

func (b *Bot) client(repo *models.Repo) (*gh.Client, error) {
	return b.app.Client(repo.InstallationID)
}

// fetchCIState reads the combined state of the old-style commit statuses on a
// sha. Deliberately not check runs: embassy's CI reports via the statuses API.
// A commit with no statuses at all comes back as "pending", which is the
// behaviour we want -- not green, so not queued.
func (b *Bot) fetchCIState(ctx context.Context, repo *models.Repo, sha string) (models.CiState, error) {
	if sha == "" {
		return models.CiStates.Unknown, nil
	}

	client, err := b.client(repo)
	if err != nil {
		return models.CiStates.Unknown, err
	}

	status, _, err := client.Repositories.GetCombinedStatus(ctx, repo.Owner, repo.Name, sha, &gh.ListOptions{PerPage: 1})
	if err != nil {
		return models.CiStates.Unknown, errors.WithStack(err)
	}

	switch status.GetState() {
	case "success":
		return models.CiStates.Success, nil
	case "pending":
		return models.CiStates.Pending, nil
	case "failure", "error":
		return models.CiStates.Failure, nil
	default:
		return models.CiStates.Unknown, nil
	}
}

// reviewable is the single definition of "belongs in the review queue": open,
// out of draft, and green.
func reviewable(pr *models.PullRequest) bool {
	return pr.State == models.PRStates.Open &&
		!pr.IsDraft &&
		pr.CiState == models.CiStates.Success
}

// ciRed is what the CI notice is about: an open, ready-for-review PR whose CI
// has actually failed. Pending deliberately doesn't count -- a build that's
// still running isn't green either, but nothing has gone wrong yet.
func ciRed(pr *models.PullRequest) bool {
	return pr.State == models.PRStates.Open &&
		!pr.IsDraft &&
		pr.CiState == models.CiStates.Failure
}

// armCICheck starts or stops the "is it still red?" clock. The grace period
// runs from CI going red, not from the PR being opened: a two-hour build that
// fails at the end gets its hour from the failure, and one that's merely still
// running never gets nagged at all.
//
// The clock is dropped rather than paused as soon as the PR stops qualifying,
// so a red -> green -> red round trip, or a draft marked ready for review, gets
// a fresh hour.
func (b *Bot) armCICheck(pr *models.PullRequest) {
	if !ciRed(pr) || pr.CiNotifiedAt.Valid {
		pr.CiCheckDueAt = null.Time{}
		return
	}
	// Already counting down: leave the deadline alone, so a second failing
	// status on the same commit doesn't keep pushing it back.
	if !pr.CiCheckDueAt.Valid {
		pr.CiCheckDueAt = null.TimeFrom(time.Now().Add(b.config.CIGracePeriod))
	}
}

// reconcile recomputes a PR's queue membership and saves it. FirstReviewableAt
// is stamped the first time the PR qualifies and never cleared, so a PR that
// goes red and green again keeps the place in line it originally earned.
func (b *Bot) reconcile(ctx context.Context, pr *models.PullRequest) error {
	now := time.Now()

	pr.IsReviewable = reviewable(pr)
	if pr.IsReviewable && !pr.FirstReviewableAt.Valid {
		pr.FirstReviewableAt = null.TimeFrom(now)
	}
	pr.UpdatedAt = now

	err := pr.Update(ctx)
	if err != nil {
		return err
	}

	log.Infof(ctx, "reconciled", log.Fields{
		"pr":                  pr.HTMLURL,
		"state":               pr.State.String(),
		"draft":               pr.IsDraft,
		"ci":                  pr.CiState.String(),
		"reviewable":          pr.IsReviewable,
		"first_reviewable_at": pr.FirstReviewableAt.Time,
	})
	return nil
}

// Queue returns the review queue: every reviewable PR, oldest-qualifying first.
// Given labels, only the PRs carrying at least one of them.
func Queue(ctx context.Context, labels []string) (models.PullRequestSlice, error) {
	mods := []qm.QueryMod{
		qm.Where("is_reviewable = true"),
		qm.OrderBy("first_reviewable_at asc"),
		qm.Load("repo"),
	}
	if len(labels) != 0 {
		// && is "the arrays overlap", i.e. the labels are ORed. The cast is
		// needed because lib/pq sends the array as an untyped literal, and
		// `anyarray && anyarray` leaves postgres nothing to infer it from.
		mods = append(mods, qm.Where("labels && ?::text[]", pgtypes.StringArray(labels)))
	}
	return models.PullRequests(mods...).All(ctx)
}

// QueueLabels returns every label present on the queue, alphabetically.
//
// Deliberately unfiltered: this is the set of filters the page offers, so
// picking one label mustn't make all the others unreachable.
func QueueLabels(ctx context.Context) ([]string, error) {
	rows, err := bunny.Query(ctx, "select distinct unnest(labels) as label from pull_request where is_reviewable order by label")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer rows.Close()

	var labels []string
	for rows.Next() {
		var label string
		err := rows.Scan(&label)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		labels = append(labels, label)
	}
	err = rows.Err()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return labels, nil
}

// LabelColors returns every label we know of, so the queue page can paint them
// the colors GitHub paints them. Ordered, so a name used by more than one repo
// in more than one color at least resolves the same way on every page load.
func LabelColors(ctx context.Context) (models.LabelSlice, error) {
	return models.Labels(qm.OrderBy("repo_id asc, name asc")).All(ctx)
}

// syncLabels copies a PR's labels onto its row and records their colors. Label
// edits made repo-wide aren't webhooked to us, so a color goes stale until some
// PR carrying it is touched, or until the next `bot sync`.
func syncLabels(ctx context.Context, repo *models.Repo, pr *models.PullRequest, ghpr *gh.PullRequest) error {
	pr.Labels = nil
	for _, l := range ghpr.Labels {
		pr.Labels = append(pr.Labels, l.GetName())

		label := &models.Label{
			RepoID: repo.ID,
			Name:   l.GetName(),
			Color:  l.GetColor(),
		}
		err := label.Upsert(ctx, "(repo_id, name)", map[models.LabelColumn]string{
			models.LabelColumns.Color: "EXCLUDED.color",
		})
		if err != nil {
			return err
		}
	}
	return nil
}
