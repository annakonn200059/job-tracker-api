# Job Tracker API

Go REST API for a self-hosted job search tracker: a Kanban pipeline of applications,
LLM-based parsing of job postings, and semantic matching against versioned CVs.

This is one of four repositories:

| Repository | Contents |
|---|---|
| `job-tracker-web` | Next.js landing page and application |
| **`job-tracker-api`** | **this one — Go REST API, owner of the database schema** |
| `job-tracker-worker` | Python worker: posting parsing, LLM calls, embeddings |
| `job-tracker-infra` | Terraform, Ansible, Helm chart, Argo CD manifests |

---

## Stack

- **Go** with `net/http` (method-based routing, Go 1.22+)
- **PostgreSQL 16** with `pgvector` and `citext`
- **pgx/v5** — driver and connection pool, no ORM
- **goose** — migrations
- **slog** — structured JSON logging

---

## Quick start

Prerequisites: Go 1.22+, Docker (or OrbStack), `goose`.

```bash
# 1. Start PostgreSQL (lives in the infra repository)
cd ../job-tracker-infra
make up
make ps                 # wait for "healthy"

# 2. Configure
cd ../job-tracker-api
cp .env.example .env

# 3. Apply the schema
make migrate-up
make migrate-status

# 4. Run
make run
```

Verify:

```bash
curl -i localhost:8080/healthz    # 200 — process is alive
curl -i localhost:8080/readyz     # 200 — database reachable
```

The two probes are deliberately different. `/healthz` checks nothing but the
process; `/readyz` pings the database. If liveness checked the database, a brief
outage would cause Kubernetes to restart every healthy pod at once and turn a
blip into a full outage.

---

## Configuration

All configuration is read from the environment once at startup and validated
there. A missing or malformed value fails immediately with a named error rather
than surfacing as odd behaviour under load.

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | — | **required**, PostgreSQL DSN |
| `HTTP_PORT` | `8080` | listen port |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `SHUTDOWN_WAIT` | `15s` | grace period for in-flight requests |
| `DB_MAX_CONNS` | `10` | pool ceiling |
| `DB_MIN_CONNS` | `0` | idle connections held open |
| `DB_MAX_CONN_LIFETIME` | `1h` | connection recycle interval |
| `DB_MAX_CONN_IDLE_TIME` | `30m` | idle connection timeout |
| `DB_CONNECT_TIMEOUT` | `5s` | initial connection timeout |

`DB_MAX_CONNS` needs attention when scaling: with 5 replicas at 10 connections
each you need 50 slots against a PostgreSQL default of 100.

---

## Layout

```
cmd/api/            entry point, wiring only
config/             environment loading and validation
domains/            types, domain errors, filters — imports nothing
  errors/           shared errors: NotFound, Conflict, Validation
  applications/     Application, Stage, Filter
  vacancies/        Vacancy, WorkMode, EmploymentType, Filter
  events/           Event, Kind, Author
repos/              SQL, one package per table
services/           business rules, transactions
http/               handlers, middleware, error mapping
internal/
  infrastructure/
    database/       connection pool, DBTX, InTx
migrations/         goose migrations — the source of truth for the schema
api/                OpenAPI specification
docs/adr/           architecture decision records
```

Dependencies point one way: `http → services → repos → domains`. Domain errors
travel up unchanged, and `http` is the only layer that knows about HTTP status
codes. A repository returns `apperr.ErrNotFound`, never `pgx.ErrNoRows`, so the
driver never leaks above the repository.

Repositories hold a `database.DBTX` rather than a concrete pool. Both
`*pgxpool.Pool` and `pgx.Tx` satisfy it, so a service can bind several
repositories to one transaction via `WithTx`.

---

## Database schema

Six tables plus a link table. Everything is scoped to a user; there is no shared
catalogue of vacancies.

```
users
  ├── companies
  │     ├── vacancies ──── applications ──── application_events
  │     └── contacts ───────────────────────────────┘
  └── tags ──── application_tags ──── applications
```

### users

Account and credentials.

| Column | Type | Notes |
|---|---|---|
| `id` | `bigint` | identity, primary key |
| `email` | `citext` | case-insensitive — `Anna@x.ch` and `anna@x.ch` are the same account |
| `password_hash` | `text` | nullable: OAuth-only accounts have none |
| `display_name` | `text` | |
| `locale` | `text` | default `en` |
| `created_at`, `updated_at`, `deleted_at` | `timestamptz` | |

### companies

| Column | Type | Notes |
|---|---|---|
| `id` | `bigint` | identity, primary key |
| `user_id` | `bigint` | → `users`, cascade |
| `name` | `text` | unique per user among active rows |
| `website`, `location`, `notes` | `text` | |

### vacancies

A job posting as the user recorded it. Personal, not shared: the user pastes a
URL or fills the details in, so edits and notes belong to them alone.

| Column | Type | Notes |
|---|---|---|
| `id` | `bigint` | identity, primary key |
| `user_id` | `bigint` | → `users`, cascade |
| `company_id` | `bigint` | nullable, → `companies` |
| `title` | `text` | required |
| `url`, `description`, `location` | `text` | |
| `work_mode` | `text` | `onsite` / `hybrid` / `remote` |
| `employment_type` | `text` | `full_time` / `part_time` / `contract` / `internship` |
| `language` | `text` | posting language — matters for the Swiss market |
| `salary_min`, `salary_max` | `integer` | `CHECK (min <= max)` |
| `salary_currency`, `salary_period` | `text` | `hour` / `day` / `month` / `year` |
| `source` | `text` | where the posting was found |
| `posted_at` | `date` | |

### applications

The user's application to a vacancy, and its position on the board.

| Column | Type | Notes |
|---|---|---|
| `id` | `bigint` | identity, primary key |
| `user_id` | `bigint` | → `users`, cascade |
| `vacancy_id` | `bigint` | → `vacancies`, cascade |
| `stage` | `text` | `saved` → `applied` → `screening` → `interview` → `final` → `offer` / `rejected` / `withdrawn` |
| `board_order` | `double precision` | position within the stage column |
| `priority` | `smallint` | |
| `applied_at` | `timestamptz` | set once, on first entry to `applied` |
| `closed_at` | `timestamptz` | set on `offer` / `rejected` / `withdrawn` |
| `notes` | `text` | |

One **active** application per vacancy per user. Re-applying after deletion is
allowed, because the uniqueness rule is scoped to active rows.

### application_events

Append-only history. **No `updated_at`, no `deleted_at`** — this table is the
source of truth for funnel analytics, and rewriting history would make those
numbers lie. Corrections are new events, not edits.

| Column | Type | Notes |
|---|---|---|
| `id` | `bigint` | identity, primary key |
| `user_id` | `bigint` | |
| `application_id` | `bigint` | → `applications`, cascade |
| `contact_id` | `bigint` | nullable, → `contacts` |
| `kind` | `text` | `stage_change`, `email_sent`, `email_received`, `call`, `interview`, `task`, `note` |
| `from_stage`, `to_stage` | `text` | required when `kind = 'stage_change'` |
| `title`, `body` | `text` | |
| `created_by` | `text` | `user` / `ai` / `system` |
| `occurred_at` | `timestamptz` | when it happened, not when it was recorded |

### contacts, tags, application_tags

Recruiters and hiring managers; user-defined labels; the link between labels and
applications. `application_tags` is a link table and is hard-deleted.

---

## Design decisions

### Tenant isolation enforced by the database

Every table carries `user_id`, and cross-table references use **composite
foreign keys**:

```sql
FOREIGN KEY (user_id, company_id)
    REFERENCES companies(user_id, id)
    ON DELETE SET NULL (company_id)
```

This makes it impossible to attach one user's company to another user's vacancy,
even with a bug in the application layer. Each referenced table therefore carries
a full `UNIQUE (user_id, id)` constraint — a foreign key requires a unique index
on its target.

The column-scoped `SET NULL (company_id)` matters: a plain `ON DELETE SET NULL`
on a composite key nulls *every* referencing column, including the `NOT NULL`
`user_id`, and deleting a company would fail at runtime. The column list is a
PostgreSQL 15+ feature.

### Soft delete with partial unique indexes

`deleted_at IS NULL` means active. Business uniqueness is enforced by partial
indexes:

```sql
CREATE UNIQUE INDEX ux_companies_name_active
    ON companies(user_id, name) WHERE deleted_at IS NULL;
```

A plain `UNIQUE` would block recreating a company after its predecessor was
soft-deleted, since the old row is still physically present. Partial indexes
apply only to active rows, so names become reusable while duplicates among live
rows stay forbidden.

Two consequences:

- **Every read must filter `deleted_at IS NULL`.** Views `active_companies`,
  `active_vacancies`, `active_contacts`, `active_applications`, `active_tags`
  exist so read paths need not remember. Writes always target base tables.
- **Cascading a soft delete is the application's job.** Physical `CASCADE` and
  `SET NULL` remain as a safety net for real erasure (GDPR, admin cleanup) but
  do not fire on a soft delete.

`UNIQUE (user_id, id)` stays a full constraint: a partial index cannot be a
foreign-key target, and it never conflicts with soft delete because `id` is
unique regardless of state.

### Stage stored twice, on purpose

`applications.stage` holds the current value; `application_events` holds the
transition history. Formally denormalised, but the board needs a fast query on a
single column while analytics needs the timeline. Both are written in one
transaction — a stage change with a missing event would corrupt every funnel
metric derived from it.

### Fractional board ordering

`board_order` is a float. Dropping a card between two neighbours writes the
midpoint, so no other row is touched. New cards land at `MAX + 1000`.

Repeated midpoint insertion eventually exhausts float precision; when the
smallest gap in a column falls below a threshold, the service renumbers that
column with even 1000-unit spacing.

### CHECK constraints instead of ENUM

Adding a value to a `CHECK` is an ordinary migration. Removing a value from a
PostgreSQL `ENUM` type is not possible at all. For sets that will change —
stages, work modes, event kinds — the weaker constraint is the practical one.

### One active application per vacancy

Enforced by `ux_applications_vacancy_active`. Re-applying six months later means
a new posting, and therefore a new vacancy row. This is a product decision rather
than a technical constraint; see `docs/adr/`.

---

## Migrations

```bash
make migrate-new name=add_something   # create
make migrate-up                       # apply
make migrate-down                     # roll back one
make migrate-status                   # what is applied
```

`goose` records applied versions in `goose_db_version` **inside the database**,
not in the files. Two consequences:

- Rolling back removes the row rather than marking it unapplied, so gaps in the
  `id` sequence are normal.
- Editing an already-applied migration and running `down` executes the *new*
  down section against the *old* schema. Roll back first, then edit.

While nothing is committed, the fastest reset is to drop the database entirely:

```bash
cd ../job-tracker-infra && make reset && cd -
make migrate-up
```

---

## Testing

```bash
make test
```

Repository tests run against a real PostgreSQL through testcontainers — schema
behaviour, constraint violations and query plans cannot be verified against a
mock.

**Every read method has a tenant-isolation test**: create as user 1, fetch as
user 2, expect `ErrNotFound`. A forgotten `user_id` filter is a data leak, and
attention alone does not catch it reliably.

---

## Conventions

- Repositories return domain errors, never driver errors
- Handlers never construct SQL; services never write HTTP status codes
- 5xx responses are logged, 4xx are not — a 404 is ordinary traffic and logging
  it turns the error dashboard into noise
- Error bodies never carry the original error text for 5xx: a driver message can
  leak table and column names
- `ORDER BY` cannot be parameterised, so sort fields go through a whitelist map;
  an unrecognised key is rejected rather than defaulted
- Nil slices are normalised to empty before reaching SQL: `cardinality(NULL)` is
  `NULL`, which makes the whole predicate `NULL` and silently drops every row

---

## Roadmap

| Stage | Adds |
|---|---|
| now | CRUD, board, soft delete |
| auth | JWT, sessions, `user_id` from token instead of the dev constant |
| worker | posting parsing, LLM extraction, embeddings in `pgvector` |
| analytics | funnel, time-to-first-response, conversion by source |

---

## Documentation

- `docs/adr/` — architecture decision records
- `docs/incidents/` — post-mortems of deliberate breakage: context, symptom,
  cause, fix
- `api/openapi.yaml` — API specification, source for the frontend's generated
  types
