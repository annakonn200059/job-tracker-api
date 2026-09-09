-- +goose Up

CREATE EXTENSION IF NOT EXISTS citext;

-- =========================================================
-- SOFT DELETE CONVENTION
--
-- Business uniqueness is enforced by PARTIAL unique indexes scoped to
-- active rows, so a name can be reused after its owner deletes it.
--
-- UNIQUE (user_id, id) stays a FULL constraint: a partial index cannot
-- be the target of a foreign key. It never conflicts with soft delete
-- because id is unique regardless of state.
--
-- Physical FK actions (CASCADE / SET NULL) are kept as a safety net for
-- real deletion (GDPR erasure, admin cleanup). Soft-delete cascading is
-- the application's responsibility.
--
-- application_events has NO deleted_at: it is an immutable audit log and
-- the source of truth for funnel analytics.
-- =========================================================

-- =========================================================
-- USERS
-- =========================================================

CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         CITEXT NOT NULL,
    password_hash TEXT,                      -- NULL when the account is OAuth-only
    display_name  TEXT,
    locale        TEXT   NOT NULL DEFAULT 'en',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE UNIQUE INDEX ux_users_email_active
    ON users(email) WHERE deleted_at IS NULL;

-- =========================================================
-- COMPANIES
-- =========================================================

CREATE TABLE companies (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT   NOT NULL,
    website    TEXT,
    location   TEXT,
    notes      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,

    UNIQUE (user_id, id)          -- target for tenant-scoped foreign keys
);

CREATE UNIQUE INDEX ux_companies_name_active
    ON companies(user_id, name) WHERE deleted_at IS NULL;

-- =========================================================
-- VACANCIES
-- =========================================================

CREATE TABLE vacancies (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    company_id      BIGINT,
    title           TEXT NOT NULL,
    url             TEXT,
    description     TEXT,
    location        TEXT,

    work_mode       TEXT
        CHECK (work_mode IN ('onsite', 'hybrid', 'remote')),

    employment_type TEXT
        CHECK (employment_type IN (
            'full_time', 'part_time', 'contract', 'internship'
        )),

    language        TEXT,

    salary_min      INTEGER,
    salary_max      INTEGER,
    salary_currency TEXT,
    salary_period   TEXT
        CHECK (salary_period IN ('hour', 'day', 'month', 'year')),

    source          TEXT,
    posted_at       DATE,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,

    UNIQUE (user_id, id),

    -- Vacancy and company must belong to the same user.
    -- Column-scoped SET NULL: only company_id is cleared, user_id stays.
    FOREIGN KEY (user_id, company_id)
        REFERENCES companies(user_id, id)
        ON DELETE SET NULL (company_id),

    CHECK (
        salary_min IS NULL
        OR salary_max IS NULL
        OR salary_min <= salary_max
    )
);

CREATE INDEX idx_vacancies_user_active
    ON vacancies(user_id) WHERE deleted_at IS NULL;

CREATE INDEX idx_vacancies_company_active
    ON vacancies(company_id) WHERE deleted_at IS NULL;

-- =========================================================
-- CONTACTS
-- =========================================================

CREATE TABLE contacts (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    company_id   BIGINT,
    full_name    TEXT   NOT NULL,
    job_title    TEXT,
    email        CITEXT,
    phone        TEXT,
    linkedin_url TEXT,
    notes        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ,

    UNIQUE (user_id, id),

    FOREIGN KEY (user_id, company_id)
        REFERENCES companies(user_id, id)
        ON DELETE SET NULL (company_id)
);

CREATE INDEX idx_contacts_user_active
    ON contacts(user_id) WHERE deleted_at IS NULL;

CREATE INDEX idx_contacts_company_active
    ON contacts(company_id) WHERE deleted_at IS NULL;

-- =========================================================
-- APPLICATIONS
-- =========================================================

CREATE TABLE applications (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    vacancy_id  BIGINT NOT NULL,

    stage       TEXT NOT NULL DEFAULT 'saved'
        CHECK (stage IN (
            'saved', 'applied', 'screening', 'interview',
            'final', 'offer', 'rejected', 'withdrawn'
        )),

    board_order DOUBLE PRECISION NOT NULL DEFAULT 0,
    priority    SMALLINT NOT NULL DEFAULT 0,

    applied_at  TIMESTAMPTZ,
    closed_at   TIMESTAMPTZ,
    notes       TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,

    UNIQUE (user_id, id),

    FOREIGN KEY (user_id, vacancy_id)
        REFERENCES vacancies(user_id, id)
        ON DELETE CASCADE
);

-- One ACTIVE application per vacancy per user; re-applying after
-- deletion is allowed.
CREATE UNIQUE INDEX ux_applications_vacancy_active
    ON applications(user_id, vacancy_id) WHERE deleted_at IS NULL;

CREATE INDEX idx_applications_board_active
    ON applications(user_id, stage, board_order) WHERE deleted_at IS NULL;

CREATE INDEX idx_applications_vacancy_active
    ON applications(vacancy_id) WHERE deleted_at IS NULL;

-- =========================================================
-- APPLICATION EVENTS  (immutable audit log: no updated_at, no deleted_at)
-- =========================================================

CREATE TABLE application_events (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id        BIGINT NOT NULL,
    application_id BIGINT NOT NULL,
    contact_id     BIGINT,

    kind           TEXT NOT NULL
        CHECK (kind IN (
            'stage_change', 'email_sent', 'email_received',
            'call', 'interview', 'task', 'note'
        )),

    from_stage     TEXT
        CHECK (from_stage IN (
            'saved', 'applied', 'screening', 'interview',
            'final', 'offer', 'rejected', 'withdrawn'
        )),
    to_stage       TEXT
        CHECK (to_stage IN (
            'saved', 'applied', 'screening', 'interview',
            'final', 'offer', 'rejected', 'withdrawn'
        )),

    title          TEXT,
    body           TEXT,

    created_by     TEXT NOT NULL DEFAULT 'user'
        CHECK (created_by IN ('user', 'ai', 'system')),

    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    FOREIGN KEY (user_id, application_id)
        REFERENCES applications(user_id, id)
        ON DELETE CASCADE,

    FOREIGN KEY (user_id, contact_id)
        REFERENCES contacts(user_id, id)
        ON DELETE SET NULL (contact_id),

    -- A stage_change must record where it moved from and to.
    CHECK (
        kind <> 'stage_change'
        OR (from_stage IS NOT NULL AND to_stage IS NOT NULL)
    )
);

CREATE INDEX idx_events_application ON application_events(application_id, occurred_at DESC);
CREATE INDEX idx_events_contact     ON application_events(contact_id);

-- =========================================================
-- TAGS
-- =========================================================

CREATE TABLE tags (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,

    UNIQUE (user_id, id)
);

CREATE UNIQUE INDEX ux_tags_name_active
    ON tags(user_id, name) WHERE deleted_at IS NULL;

CREATE INDEX idx_tags_user_active
    ON tags(user_id) WHERE deleted_at IS NULL;

-- Link table: hard delete only, nothing to soft-delete here.
CREATE TABLE application_tags (
    user_id        BIGINT NOT NULL,
    application_id BIGINT NOT NULL,
    tag_id         BIGINT NOT NULL,

    PRIMARY KEY (application_id, tag_id),

    FOREIGN KEY (user_id, application_id)
        REFERENCES applications(user_id, id)
        ON DELETE CASCADE,

    FOREIGN KEY (user_id, tag_id)
        REFERENCES tags(user_id, id)
        ON DELETE CASCADE
);

CREATE INDEX idx_application_tags_tag ON application_tags(tag_id);

-- =========================================================
-- UPDATED_AT TRIGGER
-- =========================================================

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_users_updated
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_companies_updated
    BEFORE UPDATE ON companies
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_vacancies_updated
    BEFORE UPDATE ON vacancies
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_contacts_updated
    BEFORE UPDATE ON contacts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_applications_updated
    BEFORE UPDATE ON applications
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- =========================================================
-- CONVENIENCE VIEWS FOR ACTIVE ROWS
--
-- Read paths may select from these instead of remembering the filter.
-- Writes always go to the base tables.
-- =========================================================

CREATE VIEW active_companies    AS SELECT * FROM companies    WHERE deleted_at IS NULL;
CREATE VIEW active_vacancies    AS SELECT * FROM vacancies    WHERE deleted_at IS NULL;
CREATE VIEW active_contacts     AS SELECT * FROM contacts     WHERE deleted_at IS NULL;
CREATE VIEW active_applications AS SELECT * FROM applications WHERE deleted_at IS NULL;
CREATE VIEW active_tags         AS SELECT * FROM tags         WHERE deleted_at IS NULL;


-- +goose Down

DROP VIEW IF EXISTS active_tags;
DROP VIEW IF EXISTS active_applications;
DROP VIEW IF EXISTS active_contacts;
DROP VIEW IF EXISTS active_vacancies;
DROP VIEW IF EXISTS active_companies;

DROP TABLE IF EXISTS application_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS application_events;
DROP TABLE IF EXISTS applications;
DROP TABLE IF EXISTS contacts;
DROP TABLE IF EXISTS vacancies;
DROP TABLE IF EXISTS companies;
DROP TABLE IF EXISTS users;

DROP FUNCTION IF EXISTS set_updated_at();