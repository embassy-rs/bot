package bot

import (
	"context"
	"os"
	"testing"
	"time"

	"embassy.dev/bot/internal/models"
	"embassy.dev/bot/toolkit/db"
	"github.com/sqlbunny/sqlbunny/runtime/qm"
)

// testCtx connects to the local dev database. Skips rather than fails when
// there isn't one, so `go test ./...` works on a bare checkout.
func testCtx(t *testing.T) context.Context {
	t.Helper()

	if os.Getenv("CONFIG_DB_HOST") == "" {
		t.Skip("no CONFIG_DB_HOST; run under ./d or start the dev postgres")
	}

	config, err := db.ConfigFromEnv()
	if err != nil {
		t.Skipf("no db config: %v", err)
	}
	sqlDB, err := db.New(config)
	if err != nil {
		t.Skipf("no db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	return db.Context(context.Background(), sqlDB)
}

// The queue is ordered by when a PR *first* became reviewable, so a PR that
// goes red and green again must come back to the same place in line rather than
// to the end of it.
func TestKeepsQueuePositionAfterDropout(t *testing.T) {
	ctx := testCtx(t)
	b := &Bot{config: &Config{}}

	repo := &models.Repo{ID: -1, InstallationID: 1, Owner: "test", Name: "repo"}
	err := repo.Upsert(ctx, "(id)", map[models.RepoColumn]string{
		models.RepoColumns.Owner: "EXCLUDED.owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		prs, _ := models.PullRequests(qm.Where("repo_id = ?", repo.ID)).All(ctx)
		_ = prs.DeleteAll(ctx)
		_ = repo.Delete(ctx)
	})

	pr := &models.PullRequest{
		RepoID:    repo.ID,
		Number:    1,
		State:     models.PRStates.Open,
		CiState:   models.CiStates.Success,
		CreatedAt: time.Now(),
	}
	err = pr.Insert(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Green and ready: joins the queue, position stamped.
	err = b.reconcile(ctx, pr)
	if err != nil {
		t.Fatal(err)
	}
	if !pr.IsReviewable {
		t.Fatal("want reviewable after going green")
	}
	position := pr.FirstReviewableAt
	if !position.Valid {
		t.Fatal("want first_reviewable_at stamped")
	}

	// CI goes red: drops out, but the position is remembered.
	pr.CiState = models.CiStates.Failure
	err = b.reconcile(ctx, pr)
	if err != nil {
		t.Fatal(err)
	}
	if pr.IsReviewable {
		t.Fatal("want not reviewable while red")
	}

	// Back to green: same place in line, not the back of it.
	time.Sleep(10 * time.Millisecond)
	pr.CiState = models.CiStates.Success
	err = b.reconcile(ctx, pr)
	if err != nil {
		t.Fatal(err)
	}
	if !pr.IsReviewable {
		t.Fatal("want reviewable after going green again")
	}
	if !pr.FirstReviewableAt.Time.Equal(position.Time) {
		t.Fatalf("lost queue position: was %v, now %v", position.Time, pr.FirstReviewableAt.Time)
	}

	// And the same after a round trip through the database. Postgres keeps
	// timestamps to the microsecond and rounds, so allow a hair of slack -- a
	// re-stamp would be at least the sleep above away, not sub-microsecond.
	err = pr.Reload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	drift := pr.FirstReviewableAt.Time.Sub(position.Time).Abs()
	if drift > time.Millisecond {
		t.Fatalf("lost queue position in db: was %v, now %v", position.Time, pr.FirstReviewableAt.Time)
	}
}

// Draft and closed PRs are out of the queue regardless of CI.
func TestReviewableConditions(t *testing.T) {
	green := func() *models.PullRequest {
		return &models.PullRequest{
			State:   models.PRStates.Open,
			CiState: models.CiStates.Success,
		}
	}

	if !reviewable(green()) {
		t.Error("open + ready + green should be reviewable")
	}

	pr := green()
	pr.IsDraft = true
	if reviewable(pr) {
		t.Error("draft should not be reviewable")
	}

	pr = green()
	pr.State = models.PRStates.Closed
	if reviewable(pr) {
		t.Error("closed should not be reviewable")
	}

	for _, state := range []models.CiState{models.CiStates.Unknown, models.CiStates.Pending, models.CiStates.Failure} {
		pr = green()
		pr.CiState = state
		if reviewable(pr) {
			t.Errorf("ci %s should not be reviewable", state)
		}
	}
}
