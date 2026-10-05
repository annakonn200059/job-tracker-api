# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go REST API backend for tracking job applications (companies, vacancies, applications moving through a
Kanban-style pipeline, a timeline of events per application, contacts, tags). Module path:
`github.com/annakonn200059/job-tracker-api`. Postgres via `pgx/v5`, migrations via `goose`.

The project is early-stage. `cmd/api/main.go` wires up `/healthz`, `/readyz`, and the `auth`/`applications`/`vacancies`
routes; `events`, `companies`, `contacts`, and `tags` have no HTTP surface yet (events/companies only have repos,
no service; contacts/tags have neither). Check `api/`, `services/`, and `cmd/api/main.go` before assuming a route
or service method exists.

## Commands

```sh
go build -o bin/api ./cmd/api   # build (same as `make build`)
make build                      # -> bin/api
make run                        # build + run bin/api
go vet ./...
go test ./...
go test ./api/vacancies -run TestCreateVacancy -v   # single test
npx @redocly/cli lint api/openapi.yaml               # lint the API spec (config: redocly.yaml)
```

Migrations use `goose` (not a Go module dependency — install with
`go install github.com/pressly/goose/v3/cmd/goose@latest` if the `goose` binary isn't on PATH). The Makefile
auto-loads `.env` and exports it, so `DATABASE_URL` doesn't need to be set manually:

```sh
make migrate-new name=add_something   # migrations/<timestamp>_add_something.sql
make migrate-up
make migrate-down
make migrate-status
```

Migrations live in `migrations/`; there is currently one file (`20260819172202_init.sql`) that creates the full
initial schema.

Config comes entirely from environment variables (see `config/config.go`). `DATABASE_URL` is required; everything else has a default. Copy `.env.example` to `.env` for local
dev (`HTTP_PORT`, `LOG_LEVEL`, and pool tuning vars `DB_MAX_CONNS`, `DB_MIN_CONNS`, `DB_MAX_CONN_LIFETIME`,
`DB_MAX_CONN_IDLE_TIME`, `DB_CONNECT_TIMEOUT` are all optional).

## Architecture

Layering, outer to inner:

```
cmd/api          composition root: loads config, opens the pool, wires repos/services/handlers, starts http.Server
api/<name>       HTTP handlers, one package per aggregate — decode request, call the service, encode response
http/            shared HTTP-facing helpers used by every api/<name> package (see below)
services/<name>  business logic, transaction boundaries, orchestration across repos
repos/<name>     SQL, one package per aggregate, talk to Postgres via pgx
domains/<name>   pure Go types/enums/validation — no I/O, imported by every other layer
internal/infrastructure/database   connection pool + the DBTX abstraction + InTx helper
```

Package naming convention: domain packages are `<name>_models` (import alias e.g. `applications_models`,
`vacancies_models`, `events_models`, `errors_models`, `companies_models`), repo packages are `<name>_repo`
(`applications_repo`, `events_repo`, `vacancies_repo`, `companies_repo`), service packages are `<name>_service`,
and HTTP handler packages under `api/` are `<name>_api` (`applications_api`, `vacancies_api`) — each exposes a
`Handler` with a `NewHandler(svc)` constructor and a `Register(mux *http.ServeMux)` method that `main.go` calls.

`applications_repo.Repo`, `events_repo.Repo`, `vacancies_repo.Repo`, and `companies_repo.Repo` all depend on
`database.DBTX` (satisfied by both `*pgxpool.Pool` and `pgx.Tx`) and expose `WithTx(tx)` so a service can rebind a
repo into an in-flight transaction — this is the pattern to follow for any new repo.

### HTTP layer (`api/`, `http/`)

Routing uses the standard library's Go 1.22+ `http.ServeMux` pattern syntax directly (`"GET /vacancies/{id}"`) —
no router dependency. Each `api/<name>` package defines its own request/response DTOs (never reuses a `domains/*`
struct for JSON, to keep the domain layer free of encoding concerns) and converts to/from the service's domain
types.

The `http` package (note: package name `http`, so other packages must import it under an alias, e.g.
`apihttp "github.com/annakonn200059/job-tracker-api/http"`, to avoid colliding with `net/http`) provides the
helpers every handler uses: `WriteError` (maps an error to a status/JSON body — see below), `WriteJSON`,
`DecodeJSON` (rejects unknown fields, caps body size), `UserID`, `PathID`, the `Query*`/`QueryCSV*` family for
parsing filters, and `Optional[T]` for PATCH bodies. Add new query/body parsing helpers here rather than duplicating them per handler package.

**Authentication.** `http.Authenticate` wraps the whole mux: it reads a session token (`Authorization: Bearer`
first, else the HttpOnly `session` cookie), resolves it via `auth_service.Service.ResolveSession`, and stores the
user ID in the request context. Inside it, `http.RequireAuth` is **deny-by-default**: every request without a valid
session gets 401, except the exact paths in `publicPaths` in `cmd/api/main.go` (health checks, login/register/
google/logout) and CORS preflights. A new public route must be added to that list; anything else is protected
automatically. Handlers still get the caller via `apihttp.UserID(r)`. The frontend treats a 401 from any
non-`/auth/*` call as "logged out" and redirects to the landing page, so keep "not logged in" as 401 (not 403). Sessions are opaque random tokens; only their SHA-256 is stored (`sessions` table), so
logout/revocation is a row delete. `auth_service` handles password auth (bcrypt) and Google sign-in (the frontend
posts the Google Identity Services ID token to `POST /auth/google`; it is verified with go-oidc against
`GOOGLE_CLIENT_ID`, and linked via `user_identities` on the stable `sub`, never the email). When Google proves
ownership of an email whose account was registered with a password but never verified, the password is dropped and
sessions revoked before linking (pre-account-takeover defence) — keep that invariant if touching
`LoginWithGoogle`. `CORS_ALLOWED_ORIGINS` must list the frontend origin for cross-origin cookie requests.

**Every domain-specific sentinel error needs an entry in `http.WriteError`'s switch, or it silently becomes a
500.** Repos translate Postgres constraint violations into distinct sentinel values per domain package (e.g.
`applications_models.ErrInvalidStage`, `vacancies_models.ErrHasActiveApplication`) rather than wrapping the
shared `errors_models.ErrValidation`/`ErrConflict`, so `errors.Is` against the generic sentinels alone won't catch
them. When a new domain error is introduced, add it to the relevant status-code case in `http/errors.go`.

### API contract (`api/openapi.yaml`)

`api/openapi.yaml` is a **hand-written** OpenAPI 3.0 spec and the source of truth for the HTTP surface; the
frontend generates its TypeScript types from it. Nothing generates it from Go, so it only stays honest because of
the contract tests: `api/<name>/contract_test.go` runs the real handlers (behind the real auth middleware, via
`contracttest.Authenticated`) against a fake service and sends every request through `contracttest.Do`, which
fails the test if the request or response doesn't match the spec. So far `GET`/`PATCH /vacancies/{id}`,
`POST /vacancies`, `PATCH /applications/{id}` and `GET /applications/board` are covered. For a handler package to
be testable this way, its `NewHandler` takes a small `Service` interface rather than the concrete service (see
`vacancies_api.Service`, `applications_api.Service`).

When changing an endpoint, change the spec in the same commit. Keep these conventions or the tests stop catching
drift:
- Every object schema has `additionalProperties: false`, so a renamed/added Go JSON field is reported.
- Response fields Go always emits are `required`; `omitempty` fields are optional; pointers without `omitempty`
  are `nullable`.
- Test fixtures populate every field (an `omitempty` field left nil never reaches the validator), and request
  bodies send every field the spec allows (so a renamed request tag hits `DecodeJSON`'s unknown-field 400).
- Tests assert the expected status: a documented error response matches the spec too.

**PATCH is JSON merge-patch**: a field left out is unchanged, `null` clears it, a value sets it. Request DTOs use
`apihttp.Optional[T]` per field (`Optional[*T]` when it can be cleared; a non-pointer `T` rejects null), and the
service's `Update(ctx, userID, id, apply func(*Model))` locks the row (`GetByIDForUpdate`), runs `apply`,
validates and writes back in one transaction — so concurrent edits to different fields don't overwrite each
other. Don't add full-replace PATCH endpoints.

### Multi-tenancy

Every table (except the `application_tags` link table) carries `user_id`, and cross-tenant foreign keys are
composite: `FOREIGN KEY (user_id, child_fk) REFERENCES parent(user_id, id)`, so a row can only ever reference a
parent owned by the same user. Every repo query filters by `user_id` explicitly — there is no session-level
tenant scoping, so a new query that omits the `user_id` predicate is a cross-tenant leak, not just a bug.

### Soft delete

Rows carry `deleted_at`; uniqueness constraints (e.g. one active application per vacancy, unique company name per
user) are **partial unique indexes** scoped to `WHERE deleted_at IS NULL`, so a name/slot can be reused after
deletion. `active_*` views exist per table for read paths that don't want to remember the filter, but all writes
go through the base tables. `application_events` is the one table with **no** `deleted_at` — it's an immutable
audit log and the source of truth for funnel/stage-duration analytics (see
`events_repo.Repo.StageDurations`), so it must never be soft-deleted or mutated after insert.

### Error translation

Domain sentinel errors live in `domains/errors` (`ErrNotFound`, `ErrConflict`, `ErrValidation`, `ErrForbidden`)
plus per-domain sentinels (e.g. `applications_models.ErrAlreadyApplied`, `ErrInvalidStage`, `ErrSameStage`).
Repos translate raw Postgres constraint violations (`pgconn.PgError` codes) into these sentinels — see
`applications_repo.translate()` and the `switch pgErr.Code` in `events_repo.Create` — so the pgx/Postgres error
types never leak past the repo layer. `http/errors.go` maps sentinels back to HTTP status/JSON via `errors.Is`
switches. When adding a new constraint, add its Postgres code to the relevant repo's translate step rather than
handling raw pg errors in services or handlers.

### Transactions

`database.InTx(ctx, pool, fn)` runs `fn` in a transaction, committing on success and rolling back on error or
panic (see `internal/infrastructure/database/db.go`). Services call it and rebind each repo they need via
`repo.WithTx(tx)` inside the closure — see `applications_service.Service.ChangeStage` and `MoveCard` for the
pattern (lock the row with `GetByIDForUpdate`, mutate, write a corresponding `application_events` row, all in one
tx so the audit log can't desync from the pipeline state). `vacancies_service.Service.Create` uses the same
pattern for a lighter case: resolving `CreateParams.CompanyName` to a company row via
`companies_repo.GetOrCreateByName` (an upsert against the partial unique index on `(user_id, name)`) and inserting
the vacancy happen in one transaction, so a failed vacancy insert can't leave things inconsistent.

### Kanban ordering (`applications_service`)

`board_order` is a `float64` used as a fractional-indexing sort key per `(user_id, stage)` column:
- New cards / stage changes append at `max(board_order) + orderGap` (`orderGap = 1000`).
- Dragging a card between two neighbours computes the midpoint of their `board_order` values
  (`Service.computeOrder`).
- When the gap between neighbours falls below `minGapBeforeRenumber` (`0.001`), the column is renumbered to even
  1000-unit spacing (`Repo.Renumber`) before recomputing the midpoint.
- Moving a card to a different stage is logged as a `stage_change` event; reordering within the same stage is
  not.

### Filtering / sorting (`applications_models.Filter`)

`Filter.Normalize()` clamps `Limit` (default 50, max 200) and defaults `Sort`. `applications_repo.sortSQL` is an
explicit whitelist mapping `SortField` to an `ORDER BY` expression — `ORDER BY` can't be parameterized, so any new
sortable field must be added to that map rather than interpolated from user input directly.
