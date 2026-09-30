-- +goose Up

-- =========================================================
-- EMAIL VERIFICATION
--
-- Set when an identity provider (currently Google) vouches for the
-- address. Password registration does not verify email, so an unverified
-- account can be claimed by whoever owns the address: signing in with
-- Google for the first time drops the password and revokes sessions of an
-- unverified account with the same email (see auth_service.LoginWithGoogle).
-- =========================================================

ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;

-- =========================================================
-- EXTERNAL IDENTITIES
--
-- One row per (provider, subject) linked to a user. The subject is the
-- provider's stable user ID (Google's "sub" claim), never the email: a
-- provider email can change, the subject cannot.
-- =========================================================

CREATE TABLE user_identities (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider   TEXT   NOT NULL CHECK (provider IN ('google')),
    subject    TEXT   NOT NULL,
    email      CITEXT,                        -- provider email at link time, informational
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (provider, subject),
    UNIQUE (user_id, provider)                -- one account per provider per user
);

-- =========================================================
-- SESSIONS
--
-- Opaque bearer tokens. Only the SHA-256 of the token is stored, so a
-- database leak does not hand out live sessions. Logout deletes the row;
-- expired rows are ignored by lookups and pruned on login.
-- =========================================================

CREATE TABLE sessions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA  NOT NULL UNIQUE,
    user_agent TEXT,
    ip         TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX ix_sessions_user ON sessions(user_id);


-- +goose Down

DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS user_identities;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
