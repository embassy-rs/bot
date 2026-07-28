package bot

import (
	"context"
	"slices"
	"testing"
	"time"

	"embassy.dev/bot/internal/models"
	"embassy.dev/bot/toolkit/pgtypes"
	"github.com/sqlbunny/sqlbunny/runtime/qm"
)

// Filtering by labels ORs them: a PR is in the filtered queue if it carries any
// one of them.
func TestQueueLabelFilter(t *testing.T) {
	ctx := testCtx(t)
	b := &Bot{config: &Config{}}

	repo := &models.Repo{ID: -2, InstallationID: 1, Owner: "test", Name: "labels"}
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

	queue := func(number int64, labels ...string) {
		t.Helper()
		pr := &models.PullRequest{
			RepoID:    repo.ID,
			Number:    number,
			State:     models.PRStates.Open,
			CiState:   models.CiStates.Success,
			CreatedAt: time.Now(),
			Labels:    pgtypes.StringArray(labels),
		}
		err := pr.Insert(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = b.reconcile(ctx, pr)
		if err != nil {
			t.Fatal(err)
		}
	}

	queue(1, "embassy-nrf")
	queue(2, "embassy-stm32", "ci")
	queue(3)

	for _, test := range []struct {
		name   string
		labels []string
		want   []int64
	}{
		{"no filter", nil, []int64{1, 2, 3}},
		{"one label", []string{"embassy-nrf"}, []int64{1}},
		{"two labels are ORed", []string{"embassy-nrf", "ci"}, []int64{1, 2}},
		{"a label nobody has", []string{"nonexistent"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := queueNumbers(t, ctx, repo.ID, test.labels)
			if !slices.Equal(got, test.want) {
				t.Errorf("filter %v: got PRs %v, want %v", test.labels, got, test.want)
			}
		})
	}

	// The filter bar's offering: every label on the queue, deduplicated.
	labels, err := QueueLabels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ci", "embassy-nrf", "embassy-stm32"} {
		if !slices.Contains(labels, want) {
			t.Errorf("QueueLabels: missing %q, got %v", want, labels)
		}
	}
	if !slices.IsSorted(labels) {
		t.Errorf("QueueLabels: not sorted: %v", labels)
	}
}

// queueNumbers runs the queue query and keeps only this test's repo: the dev
// database it runs against holds whatever else has been synced into it.
func queueNumbers(t *testing.T, ctx context.Context, repoID int64, labels []string) []int64 {
	t.Helper()

	prs, err := Queue(ctx, labels)
	if err != nil {
		t.Fatal(err)
	}

	var numbers []int64
	for _, pr := range prs {
		if pr.RepoID == repoID {
			numbers = append(numbers, pr.Number)
		}
	}
	return numbers
}
