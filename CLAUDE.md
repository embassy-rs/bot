# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A GitHub App ("embassy bot") that greets first-time contributors, nudges draft /
red-CI pull requests, and maintains a review queue served as a small
server-rendered web page. See [README.md](README.md) for the behaviour spec and
deployment.

## Build & Development Commands

All commands run from the repo root using the `./d` script:

```bash
./d gen                 # Generate SQLBunny models (run after schema changes)
./d genmigrations       # Generate new database migration
./d mergemigrations     # Merge multiple migration heads
./d migrate             # Run pending database migrations
./d run server          # Run the server
./d serve               # Run server with auto-reload on file changes
./d sql                 # Open PostgreSQL shell
./d reset               # Destroy and recreate the local database. IMPORTANT: DO NOT RUN THIS UNLESS THE USER EXPLICITLY ASKED YOU TO. YOU DON'T NEED THIS FOR DAY-TO-DAY DEVELOPMENT WORK.
```

## Architecture Overview

Single Go module, one binary with subcommands (`server`, `migrate`, `sync`),
services configured via `CONFIG_PORTS`.

```
├── internal/
│   ├── bot/             # The actual bot: webhook handling, queue reconcile, comments, ticker
│   ├── github/          # GitHub App auth (JWT -> installation tokens) + client factory
│   ├── web/             # Server-rendered review queue page
│   ├── models/          # Auto-generated SQLBunny ORM models (DO NOT EDIT)
│   ├── migrations/      # Database migrations
│   ├── sqlbunny/        # Model generation configuration (the schema lives here)
│   ├── migrate/         # `migrate` CLI subcommand
│   ├── server/          # Wires everything together and runs the processes
│   └── toolkit/ (../toolkit)  # Shared libs (db, log, server, process, commands)
```

### Configuration

All config via environment variables prefixed with `CONFIG_`, read with
`envconfig` into a per-package `Config` struct with a `ConfigFromEnv()`
constructor. See the `./d` script for local development defaults, and
[deploy.yaml](deploy.yaml) for production values.

Dependency injection is handwritten in `wire.go`. Edit it by hand; nothing
generates it.

### Code Generation

**SQLBunny** generates all database models from the schema definition in
`internal/sqlbunny/main.go`. Never edit files in `internal/models/` directly —
they end in `.gen.go` and your changes will be overwritten.

Generated code is gitignored, so use a system `grep`, not `git grep`, when
looking for generated fields or functions.

---

## SQLBunny ORM

### Schema Definition

Models are defined in `internal/sqlbunny/main.go` using a DSL, NOT as standard
Go structs.

**After editing the schema, you MUST run:**
```bash
./d gen
```

### Model Definition Pattern

```go
Model("pull_request",
    Field("repo_id", "int64", ForeignKey("repo")),
    Field("number", "int64"),
    PrimaryKey("repo_id", "number"),      // model-level: composite primary key

    Field("title", "string"),
    Field("head_sha", "string", Index),   // field-level: single-column index
    Field("state", "pr_state"),
    Field("first_reviewable_at", "time", Null),  // Null makes it nullable

    Index("is_reviewable", "first_reviewable_at"),  // model-level: composite index
),
```

`PrimaryKey`, `Index`, `Unique` and `ForeignKey` work both as a field flag
(single column) and as a model-level item taking column names (composite).

This repo keys rows on GitHub's own numeric ids rather than minting `bunnyid`
prefixed IDs, so there are no custom ID types.

### Enum Types

```go
Type("ci_state", Enum{
    0: "unknown",   // Values are integers, names are strings
    1: "pending",
    2: "success",
    3: "failure",
}),
```

**Usage in code:**
```go
// CORRECT:
if pr.CiState == models.CiStates.Success { ... }

// WRONG:
if pr.CiState == "success" { ... }
if pr.CiState == models.CiStateSuccess { ... }
```

### Migrations

To generate a migration run `./d genmigrations`. This creates a new migration
file in `internal/migrations/migration_xxxxxxxx.go`.

Generating a migration diffs the old and new models and constructs the right
create/delete tables/fields/indexes to migrate the database. Generally you don't
need to edit the migration, but there are a few exceptions:

- When renaming fields or tables: edit the migration to change "delete, create" to "rename".
- Running custom SQL, for example to fill a newly-added column. For this, use `operations.SQL`. A handy pattern is adding the new column as nullable, then running SQL to fill it, then altering it to make it non-nullable.

### Querying

```go
// Get single record
pr, err := models.PullRequests(
    qm.Where("repo_id=?", repoID),
    qm.Where("number=?", number),
).One(ctx)
```

Prefer `.One()` over `.First()`, the latter silently ignores multiple results.

```go
// Get all matching records
prs, err := models.PullRequests(
    qm.Where("is_reviewable = true"),
    qm.OrderBy("first_reviewable_at asc"),
    qm.Load("repo"),        // Eager load relations, then read via pr.R.Repo()
).All(ctx)

// By primary key
pr, err := models.FindPullRequest(ctx, repoID, number)

// Count / Exists
count, err := models.PullRequests(qm.Where("repo_id=?", repoID)).Count(ctx)
exists, err := models.PullRequests(qm.Where("repo_id=?", repoID)).Exists(ctx)
```

Accessing a relation that wasn't eager-loaded panics — `qm.Load("repo")` and
`pr.R.Repo()` go together.

**Check for "no rows" errors:**
```go
if bunny.IsErrNoRows(err) {
    // not found
}
```

**Check for "multiple rows" errors**, useful when using `.One()` and the query
actually returned multiple results:
```go
if bunny.IsErrMultipleRows(err) {
    // something
}
```

### Atomic Transactions

```go
err = bunny.Atomic(ctx, func(ctx context.Context) error {
    // All database operations here are in a transaction
    pr, err := models.FindPullRequest(ctx, repoID, number)
    if err != nil {
        return err  // Will rollback
    }
    pr.IsReviewable = true
    return pr.Update(ctx, models.PullRequestColumns.IsReviewable)
})
```

IMPORTANT: Inside the transaction, ONLY do database queries. DO NOT do any
external API calls. Reasons:
- the function inside can get re-run if there are serialization conflicts.

Instead, do one transaction to update state, then do the external API call, then
do another transaction to update more state with the result of the API call.

Take into account multiple transactions aren't atomic anymore, so handle errors
appropriately.

---

## Type System

### Nullable Types

```go
import "github.com/sqlbunny/sqlbunny/types/null"

// For primitive types (int64, string, time, etc.):
type PullRequest struct {
    FirstReviewableAt null.Time
    WelcomedAt        null.Time
}
```

NEVER use pointers (`*int64`, `*time.Time`) to represent nullable fields. Always
use `null.Int64`, `null.Time`, etc.

### Arrays

A postgres `text[]` column is declared with the `string_array` type and read in
Go as a `pgtypes.StringArray` (see [toolkit/pgtypes](toolkit/pgtypes)), which is
`[]string` with the scanning and quoting attached. A nil slice writes as an
empty array, not NULL, so the column can stay NOT NULL.

Query with postgres's array operators, casting the parameter because lib/pq
sends arrays untyped: `qm.Where("labels && ?::text[]", pgtypes.StringArray(l))`
for "overlaps any of", `@>` for "contains all of".

**Working with nullable types:**
```go
// Check if set:
if pr.FirstReviewableAt.Valid {
    t := pr.FirstReviewableAt.Time
}

// Set a value:
pr.FirstReviewableAt = null.TimeFrom(time.Now())

// Set to NULL:
pr.FirstReviewableAt = null.Time{}
```

---

## Error Handling

Keep error handling simple. In most cases, do not wrap errors with extra
context. The top level already has a stack trace which is enough to see where
the error comes from.

### Error stack tracing

```go
import "github.com/sqlbunny/errors"

// Add a stack trace to an error.
// NOTE only use it for errors coming from external libs. Errors thrown by
// sqlbunny already have stack traces.
return errors.WithStack(err)

// Stack traces in logs:
log.Errorf(ctx, "Operation failed", log.Fields{
    "err": errors.StackTrace(err),
})
```

Use `github.com/sqlbunny/errors` instead of `github.com/pkg/errors`.

---

## General style

Use `map[x]struct{}` instead of `map[x]bool` for sets. This is very important.
`map[x]bool` IS NOT IDIOMATIC because each element has 3 states: not present,
false, true. THIS IS NOT A SET!!! it's an abomination of data structure.
`map[x]struct{}` is correct, each element has 2 states: present or not present.
As a set should have. DO NOT FORGET THIS.

Avoid relying on the return value if a function errored. For example this

```go
o, err := models.Foo(qm.Where(...)).One(ctx)
if bunny.IsErrNoRows(err) {
    // ignore
} else if err != nil {
    return err
} else {
    // use o
}
```

is better than

```go
o, err := models.Foo(qm.Where(...)).One(ctx)
if err != nil && !bunny.IsErrNoRows(err) {
    return err
}
if o != nil {
    // use o
}
```

Don't put calls in error check ifs:

```go
if err := foo(fooArgument1, &SomethingLong{
    that: 4,
    has: 6,
    multiple: 6,
    lines: 9,
}); err != nil { // too long, multi-line, hard to read.
    return err
}
```

prefer a separate statement:

```go
err := foo(fooArgument1, &SomethingLong{
    that: 4,
    has: 6,
    multiple: 6,
    lines: 9,
})
if err != nil { // obvious what this does.
    return err
}
```

## Git Commits

Do not add `Co-Authored-By` lines to commit messages.
