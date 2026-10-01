package sessions_repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	auth_models "github.com/annakonn200059/job-tracker-api/domains/auth"
	apperr "github.com/annakonn200059/job-tracker-api/domains/errors"
	"github.com/annakonn200059/job-tracker-api/internal/infrastructure/database"
)

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

func (r *Repo) Create(ctx context.Context, s auth_models.Session) (*auth_models.Session, error) {
	const q = `
		INSERT INTO sessions (user_id, token_hash, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`
	err := r.db.QueryRow(ctx, q, s.UserID, s.TokenHash, s.UserAgent, s.IP, s.ExpiresAt).
		Scan(&s.ID, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UserIDByTokenHash resolves an unexpired session to its user, skipping
// soft-deleted users so deleting an account locks it out immediately.
func (r *Repo) UserIDByTokenHash(ctx context.Context, hash []byte) (int64, error) {
	const q = `
		SELECT s.user_id
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now() AND u.deleted_at IS NULL`
	var userID int64
	err := r.db.QueryRow(ctx, q, hash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, apperr.ErrNotFound
	}
	return userID, err
}

func (r *Repo) DeleteByTokenHash(ctx context.Context, hash []byte) error {
	_, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hash)
	return err
}

// DeleteByUser revokes every session of the user.
func (r *Repo) DeleteByUser(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

// DeleteExpiredByUser prunes the user's expired sessions.
func (r *Repo) DeleteExpiredByUser(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM sessions WHERE user_id = $1 AND expires_at <= now()`, userID)
	return err
}
