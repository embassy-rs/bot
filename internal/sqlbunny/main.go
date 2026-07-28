package main

import (
	"path"
	"runtime"

	"embassy.dev/bot/internal/migrations"
	. "github.com/sqlbunny/sqlbunny/gen/core"
	"github.com/sqlbunny/sqlbunny/gen/migration"
	"github.com/sqlbunny/sqlbunny/gen/stdtypes"
)

func main() {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("Caller failed")
	}
	baseDir := path.Dir(path.Dir(filename))

	Run(
		&Config{
			ModelsPackagePath: baseDir + "/models",
		},
		&migration.Plugin{
			Store:       &migrations.Store,
			PackagePath: baseDir + "/migrations",
		},
		&stdtypes.Plugin{},

		// A postgres text[]. The Go type does the scanning and quoting.
		Type("string_array", BaseType{
			Go: "embassy.dev/bot/toolkit/pgtypes.StringArray",
			Postgres: SQLType{
				Type:      "text[]",
				ZeroValue: "'{}'",
			},
		}),

		// A repo the app is installed on. The id is GitHub's, so the row survives
		// a rename of the owner or the repo.
		Model("repo",
			Field("id", "int64", PrimaryKey),
			Field("installation_id", "int64"),
			Field("owner", "string"),
			Field("name", "string"),
		),

		// A label, recorded only for its color: PRs carry their label names
		// themselves, but the queue page wants to paint them the way GitHub does.
		// Keyed per repo, since two repos can use one name with different colors.
		Model("label",
			Field("repo_id", "int64", ForeignKey("repo")),
			Field("name", "string"),
			PrimaryKey("repo_id", "name"),

			// Six hex digits, no leading '#', as GitHub gives it.
			Field("color", "string"),
		),

		Type("pr_state", Enum{
			0: "open",
			1: "closed",
		}),

		// Combined state of the *old style* commit statuses on the head commit
		// (the `status` API, not check runs). A commit with no statuses at all
		// reads back as "pending", which is what we want: not green, not queued.
		Type("ci_state", Enum{
			0: "unknown",
			1: "pending",
			2: "success",
			3: "failure",
		}),

		Model("pull_request",
			Field("repo_id", "int64", ForeignKey("repo")),
			Field("number", "int64"),
			PrimaryKey("repo_id", "number"),

			Field("title", "string"),
			Field("author", "string"),
			Field("html_url", "string"),

			// Statuses arrive keyed by commit sha, not by PR, so we look PRs up by it.
			Field("head_sha", "string", Index),

			Field("state", "pr_state"),
			Field("is_draft", "bool"),
			Field("ci_state", "ci_state"),

			// The PR's labels, denormalized onto the row: the queue page filters
			// on them. Their colors live in the label model.
			Field("labels", "string_array"),

			Field("created_at", "time"),
			Field("updated_at", "time"),

			// The queue. is_reviewable tracks the live conditions;
			// first_reviewable_at is set the first time they're all met and then
			// never changes, so a PR that drops out and comes back keeps its
			// place in line.
			Field("is_reviewable", "bool"),
			Field("first_reviewable_at", "time", Null),
			Index("is_reviewable", "first_reviewable_at"),

			// Set once when the corresponding comment is posted. Both a
			// de-duplicator against webhook redeliveries and a record of what
			// this PR's author has already been told.
			Field("welcomed_at", "time", Null),
			Field("draft_notified_at", "time", Null),
			Field("ci_notified_at", "time", Null),

			// When to check whether this PR is ready-for-review but still red.
			// Null once the check has run. See internal/bot/reconcile.go.
			Field("ci_check_due_at", "time", Null, Index),
		),
	)
}
