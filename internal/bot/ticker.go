package bot

import (
	"context"
	"time"

	"embassy.dev/bot/internal/models"
	"embassy.dev/bot/toolkit/log"
	gh "github.com/google/go-github/v88/github"
	"github.com/sqlbunny/errors"
	"github.com/sqlbunny/sqlbunny/runtime/bunny"
	"github.com/sqlbunny/sqlbunny/runtime/qm"
	"github.com/sqlbunny/sqlbunny/types/null"
)

// RunTicker drives the one thing that isn't webhook-driven: noticing that a PR's
// CI grace period has elapsed while it's still red.
func (b *Bot) RunTicker(ctx context.Context) error {
	t := time.NewTicker(b.config.TickInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			err := b.tick(ctx)
			if err != nil {
				// A failed tick is not fatal: the work is still queued in the DB
				// and the next tick picks it up.
				log.Errorf(ctx, "tick failed", log.Fields{"err": errors.StackTrace(err)})
			}
		}
	}
}

func (b *Bot) tick(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	prs, err := models.PullRequests(
		qm.Where("ci_check_due_at <= ?", time.Now()),
		qm.Load("repo"),
	).All(ctx)
	if err != nil {
		return err
	}

	for _, pr := range prs {
		err := b.runCICheck(ctx, pr)
		if err != nil {
			return err
		}
	}
	return nil
}

// runCICheck is the grace-period check armed when CI goes red: if the PR is
// still red a grace period later, say so, once.
func (b *Bot) runCICheck(ctx context.Context, pr *models.PullRequest) error {
	repo := pr.R.Repo()
	ctx = log.With(ctx, log.Fields{"pr": pr.HTMLURL})

	// Fires once, whatever the outcome, so a PR can't get stuck re-checking
	// every tick forever. Going red again arms a fresh check.
	pr.CiCheckDueAt = null.Time{}

	// Re-read the status rather than trusting our stored copy: a `status` event
	// may have been missed while we were down, and nagging about a PR that's
	// actually green would be worse than staying quiet.
	state, err := b.fetchCIState(ctx, repo, pr.HeadSha)
	if err != nil {
		log.Errorf(ctx, "ci status refresh failed", log.Fields{"err": errors.StackTrace(err)})
	} else {
		pr.CiState = state
	}

	// Red when the clock was armed isn't enough: a rerun may have put CI back to
	// pending, or a fix may have landed. It has to still be red now.
	nag := ciRed(pr) && !pr.CiNotifiedAt.Valid

	if nag {
		err := b.comment(ctx, repo, pr, b.ciText())
		if err != nil {
			// Give up rather than retry every tick. Log it and move on; the PR
			// still gets queued the moment CI goes green.
			log.Errorf(ctx, "ci notice failed", log.Fields{"err": errors.StackTrace(err)})
		} else {
			pr.CiNotifiedAt = null.TimeFrom(time.Now())
		}
	}

	return b.reconcile(ctx, pr)
}

// SyncRepo pulls in the repo's currently-open PRs. Used when the app is
// installed on a repo, and by the `sync` command to recover from missed events.
//
// Backfilled PRs deliberately get no ci_check_due_at: the grace period is meant
// to catch a PR we watched go red, not to nag every red PR in the backlog the
// moment the app is installed. The next status event on one arms it normally.
func (b *Bot) SyncRepo(ctx context.Context, repo *models.Repo) error {
	client, err := b.client(repo)
	if err != nil {
		return err
	}

	opts := &gh.PullRequestListOptions{
		State:       "open",
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	for {
		ghprs, resp, err := client.PullRequests.List(ctx, repo.Owner, repo.Name, opts)
		if err != nil {
			return errors.WithStack(err)
		}

		for _, ghpr := range ghprs {
			err := b.syncPR(ctx, repo, ghpr)
			if err != nil {
				return err
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	log.Infof(ctx, "synced repo", log.Fields{"repo": repo.Owner + "/" + repo.Name})
	return nil
}

func (b *Bot) syncPR(ctx context.Context, repo *models.Repo, ghpr *gh.PullRequest) error {
	number := int64(ghpr.GetNumber())

	pr, err := models.FindPullRequest(ctx, repo.ID, number)
	if bunny.IsErrNoRows(err) {
		pr = &models.PullRequest{
			RepoID:    repo.ID,
			Number:    number,
			CreatedAt: ghpr.GetCreatedAt().Time,
		}
		err := pr.Insert(ctx)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	pr.Title = ghpr.GetTitle()
	pr.Author = ghpr.GetUser().GetLogin()
	pr.HTMLURL = ghpr.GetHTMLURL()
	pr.HeadSha = ghpr.GetHead().GetSHA()
	pr.IsDraft = ghpr.GetDraft()
	pr.State = prState(ghpr.GetState())

	err = syncLabels(ctx, repo, pr, ghpr)
	if err != nil {
		return err
	}

	state, err := b.fetchCIState(ctx, repo, pr.HeadSha)
	if err != nil {
		return err
	}
	pr.CiState = state

	return b.reconcile(ctx, pr)
}

// Sync refreshes every repo we know about. Exposed as a command so a missed
// webhook doesn't need a database poke to fix.
func (b *Bot) Sync(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	repos, err := models.Repos().All(ctx)
	if err != nil {
		return err
	}

	for _, repo := range repos {
		err := b.SyncRepo(ctx, repo)
		if err != nil {
			return err
		}
	}
	return nil
}
