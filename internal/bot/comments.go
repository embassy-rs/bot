package bot

import (
	"context"
	"fmt"
	"strings"

	"embassy.dev/bot/internal/models"
	"embassy.dev/bot/toolkit/log"
	gh "github.com/google/go-github/v88/github"
	"github.com/sqlbunny/errors"
)

func (b *Bot) welcomeText(author string) string {
	return fmt.Sprintf(
		"👋 Welcome, @%s, and thanks for opening your first pull request here!\n\n"+
			"If you haven't already, please give the [contributor guide](%s) a read.",
		author, b.config.ContributorGuideURL,
	)
}

func (b *Bot) draftText() string {
	return fmt.Sprintf(
		"This pull request is a **draft**, so it isn't in the [review queue](%s) yet. "+
			"Mark it as **ready for review** when you'd like someone to look at it.",
		b.config.PublicURL,
	)
}

func (b *Bot) ciText() string {
	return fmt.Sprintf(
		"CI on this pull request is failing, so it isn't in the [review queue](%s) yet. "+
			"A pull request needs passing CI to be queued for review — push a fix and it'll be "+
			"added automatically.",
		b.config.PublicURL,
	)
}

// comment posts a single comment on a PR. Callers combine several paragraphs
// into one body rather than posting several comments, so a first-time
// contributor opening a draft gets one notification instead of two.
func (b *Bot) comment(ctx context.Context, repo *models.Repo, pr *models.PullRequest, parts ...string) error {
	body := strings.Join(parts, "\n\n")

	if b.config.DryRun {
		log.Infof(ctx, "dry run, not commenting", log.Fields{
			"pr":   pr.HTMLURL,
			"body": body,
		})
		return nil
	}

	client, err := b.client(repo)
	if err != nil {
		return err
	}

	_, _, err = client.Issues.CreateComment(ctx, repo.Owner, repo.Name, int(pr.Number), &gh.IssueComment{
		Body: gh.Ptr(body),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	log.Infof(ctx, "commented", log.Fields{"pr": pr.HTMLURL})
	return nil
}

// isFirstPR reports whether this is the author's first PR on the repo.
//
// We ask the search API rather than trusting our own table, which only knows
// about PRs opened since the app was installed. The just-opened PR may not be
// indexed yet, so the test is "does any *other* PR by this author exist",
// which is correct whether or not it shows up.
func (b *Bot) isFirstPR(ctx context.Context, repo *models.Repo, pr *models.PullRequest) (bool, error) {
	client, err := b.client(repo)
	if err != nil {
		return false, err
	}

	query := fmt.Sprintf("repo:%s/%s type:pr author:%s", repo.Owner, repo.Name, pr.Author)
	res, _, err := client.Search.Issues(ctx, query, &gh.SearchOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return false, errors.WithStack(err)
	}

	for _, issue := range res.Issues {
		if int64(issue.GetNumber()) != pr.Number {
			return false, nil
		}
	}
	return true, nil
}
