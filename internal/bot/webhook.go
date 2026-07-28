package bot

import (
	"context"
	"net/http"
	"strings"
	"time"

	"embassy.dev/bot/internal/models"
	"embassy.dev/bot/toolkit/log"
	gh "github.com/google/go-github/v88/github"
	"github.com/sqlbunny/errors"
	"github.com/sqlbunny/sqlbunny/runtime/bunny"
	"github.com/sqlbunny/sqlbunny/runtime/qm"
	"github.com/sqlbunny/sqlbunny/types/null"
)

// Webhook returns the handler for GitHub's deliveries.
//
// It replies 200 as long as the event was understood; a handler error is logged
// and turned into a 500, which shows up as a failed delivery in the app's
// settings page and can be redelivered by hand. Everything a handler does is
// idempotent, so redelivery is safe.
func (b *Bot) Webhook(secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		payload, err := gh.ValidatePayload(r, []byte(secret))
		if err != nil {
			log.Warnf(ctx, "webhook signature validation failed", log.Fields{"err": err.Error()})
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		eventType := gh.WebHookType(r)
		event, err := gh.ParseWebHook(eventType, payload)
		if err != nil {
			log.Warnf(ctx, "webhook parse failed", log.Fields{"err": err.Error(), "event": eventType})
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		ctx = log.With(ctx, log.Fields{
			"event":    eventType,
			"delivery": gh.DeliveryID(r),
		})

		if err := b.HandleEvent(ctx, event); err != nil {
			log.Error(ctx, errors.StackTrace(err))
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// HandleEvent dispatches one parsed webhook payload.
//
// Handling is serialized: several events for the same PR (say, a status landing
// at the same moment as a ready_for_review) would otherwise read-modify-write
// the same row concurrently and lose an update. The event rate here is a
// handful per minute, so a plain mutex is cheaper than getting row locking
// right. It does mean one slow GitHub API call holds up the next event.
func (b *Bot) HandleEvent(ctx context.Context, event any) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch e := event.(type) {
	case *gh.PullRequestEvent:
		return b.handlePullRequest(ctx, e)
	case *gh.StatusEvent:
		return b.handleStatus(ctx, e)
	case *gh.InstallationEvent:
		return b.handleInstallation(ctx, e)
	case *gh.InstallationRepositoriesEvent:
		return b.handleInstallationRepositories(ctx, e)
	case *gh.PingEvent:
		log.Info(ctx, "ping")
		return nil
	default:
		log.Info(ctx, "ignoring event")
		return nil
	}
}

func (b *Bot) handlePullRequest(ctx context.Context, e *gh.PullRequestEvent) error {
	repo, err := b.upsertRepo(ctx, e.GetRepo(), e.GetInstallation().GetID())
	if err != nil {
		return err
	}

	ghpr := e.GetPullRequest()
	number := int64(ghpr.GetNumber())

	ctx = log.With(ctx, log.Fields{"action": e.GetAction(), "pr": ghpr.GetHTMLURL()})

	pr, err := models.FindPullRequest(ctx, repo.ID, number)
	isNew := false
	switch {
	case bunny.IsErrNoRows(err):
		isNew = true
		pr = &models.PullRequest{
			RepoID:    repo.ID,
			Number:    number,
			CreatedAt: ghpr.GetCreatedAt().Time,
		}
	case err != nil:
		return err
	}

	shaChanged := pr.HeadSha != ghpr.GetHead().GetSHA()

	pr.Title = ghpr.GetTitle()
	pr.Author = ghpr.GetUser().GetLogin()
	pr.HTMLURL = ghpr.GetHTMLURL()
	pr.HeadSha = ghpr.GetHead().GetSHA()
	pr.IsDraft = ghpr.GetDraft()
	pr.State = prState(ghpr.GetState())
	pr.UpdatedAt = time.Now()

	if isNew {
		// The clock for "is this thing still red an hour later?" starts when the
		// PR is opened, draft or not.
		pr.CiCheckDueAt = null.TimeFrom(pr.CreatedAt.Add(b.config.CIGracePeriod))
		err := pr.Insert(ctx)
		if err != nil {
			return err
		}
	}

	err = syncLabels(ctx, repo, pr, ghpr)
	if err != nil {
		return err
	}

	if isNew || shaChanged || pr.CiState == models.CiStates.Unknown {
		state, err := b.fetchCIState(ctx, repo, pr.HeadSha)
		if err != nil {
			return err
		}
		pr.CiState = state
	}

	switch e.GetAction() {
	case "opened":
		// A failure here shouldn't cost us the queue update below, and there's
		// nothing to retry against: greet only runs on `opened`.
		if err := b.greet(ctx, repo, pr, ghpr); err != nil {
			log.Errorf(ctx, "greeting failed", log.Fields{"err": errors.StackTrace(err)})
		}

	case "ready_for_review":
		// Opened as a draft and marked ready after the grace period had already
		// elapsed: give it a fresh hour rather than nagging on the spot.
		if !pr.CiNotifiedAt.Valid && !pr.CiCheckDueAt.Valid {
			pr.CiCheckDueAt = null.TimeFrom(time.Now().Add(b.config.CIGracePeriod))
		}
	}

	return b.reconcile(ctx, pr)
}

// greet posts the welcome and/or draft notice. Both go in a single comment when
// both apply, so a first-time contributor opening a draft isn't greeted twice.
func (b *Bot) greet(ctx context.Context, repo *models.Repo, pr *models.PullRequest, ghpr *gh.PullRequest) error {
	if isBot(ghpr.GetUser()) {
		return nil
	}

	var (
		parts   []string
		welcome bool
		draft   bool
	)

	if !pr.WelcomedAt.Valid {
		first, err := b.isFirstPR(ctx, repo, pr)
		if err != nil {
			// Search is flaky and rate-limited; a missed welcome is better than
			// a wrong one, and better than dropping the draft notice too.
			log.Errorf(ctx, "first-PR check failed", log.Fields{"err": errors.StackTrace(err)})
		} else if first {
			parts = append(parts, b.welcomeText(pr.Author))
			welcome = true
		}
	}

	if pr.IsDraft && !pr.DraftNotifiedAt.Valid {
		parts = append(parts, b.draftText())
		draft = true
	}

	if len(parts) == 0 {
		return nil
	}

	if err := b.comment(ctx, repo, pr, parts...); err != nil {
		return err
	}

	// Only marked as said once it actually got said.
	now := time.Now()
	if welcome {
		pr.WelcomedAt = null.TimeFrom(now)
	}
	if draft {
		pr.DraftNotifiedAt = null.TimeFrom(now)
	}
	return nil
}

// handleStatus reacts to old-style commit statuses. The event carries one
// context's result, but queue membership depends on the combined state, so we
// re-read that rather than trusting the single status in the payload.
func (b *Bot) handleStatus(ctx context.Context, e *gh.StatusEvent) error {
	repo, err := b.upsertRepo(ctx, e.GetRepo(), e.GetInstallation().GetID())
	if err != nil {
		return err
	}

	sha := e.GetSHA()
	prs, err := models.PullRequests(
		qm.Where("repo_id = ? and head_sha = ?", repo.ID, sha),
	).All(ctx)
	if err != nil {
		return err
	}
	if len(prs) == 0 {
		return nil
	}

	state, err := b.fetchCIState(ctx, repo, sha)
	if err != nil {
		return err
	}

	for _, pr := range prs {
		pr.CiState = state
		if err := b.reconcile(ctx, pr); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) handleInstallation(ctx context.Context, e *gh.InstallationEvent) error {
	switch e.GetAction() {
	case "created", "unsuspend", "new_permissions_accepted":
		for _, r := range e.Repositories {
			if err := b.addRepo(ctx, r, e.GetInstallation().GetID()); err != nil {
				return err
			}
		}
	case "deleted", "suspend":
		for _, r := range e.Repositories {
			if err := b.forgetRepo(ctx, r.GetID()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Bot) handleInstallationRepositories(ctx context.Context, e *gh.InstallationRepositoriesEvent) error {
	for _, r := range e.RepositoriesAdded {
		if err := b.addRepo(ctx, r, e.GetInstallation().GetID()); err != nil {
			return err
		}
	}
	for _, r := range e.RepositoriesRemoved {
		if err := b.forgetRepo(ctx, r.GetID()); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) addRepo(ctx context.Context, r *gh.Repository, installationID int64) error {
	repo, err := b.upsertRepo(ctx, r, installationID)
	if err != nil {
		return err
	}
	return b.SyncRepo(ctx, repo)
}

func (b *Bot) forgetRepo(ctx context.Context, repoID int64) error {
	repo, err := models.FindRepo(ctx, repoID)
	if bunny.IsErrNoRows(err) {
		return nil
	} else if err != nil {
		return err
	}

	prs, err := models.PullRequests(qm.Where("repo_id = ?", repoID)).All(ctx)
	if err != nil {
		return err
	}
	err = prs.DeleteAll(ctx)
	if err != nil {
		return err
	}

	labels, err := models.Labels(qm.Where("repo_id = ?", repoID)).All(ctx)
	if err != nil {
		return err
	}
	err = labels.DeleteAll(ctx)
	if err != nil {
		return err
	}

	err = repo.Delete(ctx)
	if err != nil {
		return err
	}

	log.Infof(ctx, "forgot repo", log.Fields{"repo": repo.Owner + "/" + repo.Name})
	return nil
}

func prState(s string) models.PRState {
	if s == "closed" {
		return models.PRStates.Closed
	}
	return models.PRStates.Open
}

func isBot(u *gh.User) bool {
	return u.GetType() == "Bot" || strings.HasSuffix(u.GetLogin(), "[bot]")
}
