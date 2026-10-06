# photon-server

A generic media server for films and television, in Go. Plex, Jellyfin and Emby are the references
for behaviour; no client's or other server's vocabulary shapes the wire.

## Workflow

- **One concern per branch, one branch per PR.** Branch off `main` as `feature/…`, `fix/…` or
  `chore/…`. If a PR grows a second concern, the second concern becomes its own branch.
- **A PR is a short series of small commits**, each one step a reviewer can read on its own:
  a model, then the code that uses it, then the route that exposes it. Each commit builds and
  passes the tests by itself. A refactor or a toolchain bump is its own commit, and usually its
  own PR.
- **Commit messages are Conventional Commits**: `type(scope): summary`, lower case, imperative,
  no full stop, under 72 characters. `type` is one of `feat`, `fix`, `refactor`, `perf`, `test`,
  `docs`, `build`, `ci`, `chore`; `scope` is the package or area touched (`httpapi`, `store`,
  `ci`). A body, when needed, says why.
- **PRs are squash-merged**, so `main` has one commit per PR. The PR title is that commit's
  subject and follows the same format.
- **No branch name, commit message or trailer mentions Claude.**
- **Review before merge**, against `.github/pull_request_template.md`: does it need to exist, is
  it already here, is it idiomatic, is it small.

## Code

- **No technical debt.** No compatibility shims, no second path for the same job, no scaffolding
  for later, no stage meant to be rewritten, nothing deprecated. A known limit is a named constant
  or a refusal reason, not a gap.
- **Enums over booleans.** A mode is a typed string constant set, switched on exhaustively.
- **Standard library first.** A dependency earns its place in the PR that adds it.
- **Tests test behaviour** a client or operator would notice. No test that only restates the code.
- **Comments only where something is surprising.** No doc comment that repeats the name.
- Format and lint with `golangci-lint fmt` and `golangci-lint run` (v2.14). Both must be silent.
- **A migration never holds a large table.** `photon-server migrate` runs beside the old server, and
  goose wraps each migration in one transaction, so every lock it takes lasts to the end. An index
  on a table that exists is `CREATE INDEX CONCURRENTLY IF NOT EXISTS` in a migration marked
  `-- +goose NO TRANSACTION` (its Down `DROP INDEX CONCURRENTLY IF EXISTS`); should a build fail,
  drop the invalid index it leaves before migrating again. A CHECK or foreign key on one is added
  `NOT VALID` and then validated by `VALIDATE CONSTRAINT` as a statement of its own in such a
  migration, which reads the table without stopping its writes. Nothing rewrites one: no stored
  generated column, no change of a column's type.
