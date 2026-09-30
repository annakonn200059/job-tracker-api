package users_repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	apperr "github.com/annakonn200059/job-tracker-api/domains/errors"
	users_models "github.com/annakonn200059/job-tracker-api/domains/users"
	"github.com/annakonn200059/job-tracker-api/internal/infrastructure/database"
)

// users is the tenant root, so unlike other repos its queries are keyed by
// the user's own id/email rather than filtered by a user_id column.
type Repo struct {
	db database.DBTX
}

func NewRepo(db database.DBTX) *Repo {
	return &Repo{db: db}
}

// WithTx returns a copy bound to tx, so a service can run several repos
// inside one transaction.
func (r *Repo) WithTx(tx pgx.Tx) *Repo {
	return &Repo{db: tx}
}

const columns = `
	id, email, password_hash, display_name, locale, email_verified_at,
	created_at, updated_at`

func scanRow(row pgx.Row, u *users_models.User) error {
	return row.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Locale,
		&u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt,
	)
}

// translate maps Postgres constraint violations onto domain errors so the
// driver never leaks above this layer.
func translate(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23505": // unique_violation
		switch pgErr.ConstraintName {
		case "ux_users_email_active":
			return users_models.ErrEmailTaken
		default: // user_identities (provider, subject) / (user_id, provider)
			return apperr.ErrConflict
		}
	case "23503": // foreign_key_violation
		return apperr.ErrNotFound
	}
	return err
}

func (r *Repo) one(ctx context.Context, q string, args ...any) (*users_models.User, error) {
	var u users_models.User
	err := scanRow(r.db.QueryRow(ctx, q, args...), &u)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, translate(err)
	}
	return &u, nil
}

// Create inserts a user. PasswordHash and EmailVerifiedAt may be nil.
func (r *Repo) Create(ctx context.Context, u users_models.User) (*users_models.User, error) {
	const q = `
		INSERT INTO users (email, password_hash, display_name, email_verified_at)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + columns
	return r.one(ctx, q, u.Email, u.PasswordHash, u.DisplayName, u.EmailVerifiedAt)
}

func (r *Repo) GetByID(ctx context.Context, id int64) (*users_models.User, error) {
	const q = `SELECT ` + columns + `
		FROM users
		WHERE id = $1 AND deleted_at IS NULL`
	return r.one(ctx, q, id)
}

func (r *Repo) GetByEmail(ctx context.Context, email string) (*users_models.User, error) {
	const q = `SELECT ` + columns + `
		FROM users
		WHERE email = $1 AND deleted_at IS NULL`
	return r.one(ctx, q, email)
}

// GetByEmailForUpdate is GetByEmail with a row lock, for account linking.
func (r *Repo) GetByEmailForUpdate(ctx context.Context, email string) (*users_models.User, error) {
	const q = `SELECT ` + columns + `
		FROM users
		WHERE email = $1 AND deleted_at IS NULL
		FOR UPDATE`
	return r.one(ctx, q, email)
}

// GetByIdentity returns the active user linked to (provider, subject).
func (r *Repo) GetByIdentity(ctx context.Context, provider users_models.Provider, subject string) (*users_models.User, error) {
	const q = `SELECT ` + prefixed + `
		FROM users u
		JOIN user_identities i ON i.user_id = u.id
		WHERE i.provider = $1 AND i.subject = $2 AND u.deleted_at IS NULL`
	return r.one(ctx, q, provider, subject)
}

const prefixed = `
	u.id, u.email, u.password_hash, u.display_name, u.locale, u.email_verified_at,
	u.created_at, u.updated_at`

// MarkEmailVerified sets email_verified_at. When dropPassword is true it
// also clears password_hash — used when a provider proves ownership of an
// address that was registered, unverified, with a password.
func (r *Repo) MarkEmailVerified(ctx context.Context, id int64, dropPassword bool) (*users_models.User, error) {
	const q = `
		UPDATE users
		SET email_verified_at = COALESCE(email_verified_at, now()),
		    password_hash     = CASE WHEN $2 THEN NULL ELSE password_hash END
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING ` + columns
	return r.one(ctx, q, id, dropPassword)
}

func (r *Repo) CreateIdentity(ctx context.Context, i users_models.Identity) error {
	const q = `
		INSERT INTO user_identities (user_id, provider, subject, email)
		VALUES ($1, $2, $3, $4)`
	_, err := r.db.Exec(ctx, q, i.UserID, i.Provider, i.Subject, i.Email)
	return translate(err)
}
