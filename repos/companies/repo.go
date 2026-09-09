package companies_repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	companies_models "github.com/annakonn200059/job-tracker-api/domains/companies"
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

const columns = `
	id, user_id, name, website, location, notes, created_at, updated_at`

func scanRow(row pgx.Row, c *companies_models.Company) error {
	return row.Scan(
		&c.ID, &c.UserID, &c.Name, &c.Website, &c.Location, &c.Notes,
		&c.CreatedAt, &c.UpdatedAt,
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
	case "23503": // foreign_key_violation: user_id doesn't reference a real user
		return apperr.ErrNotFound
	}
	return err
}

func (r *Repo) GetByID(ctx context.Context, userID, id int64) (*companies_models.Company, error) {
	const q = `SELECT ` + columns + `
		FROM companies
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`

	var c companies_models.Company
	err := scanRow(r.db.QueryRow(ctx, q, id, userID), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetOrCreateByName returns the user's active company with this name,
// creating it if it doesn't exist yet. The upsert targets the same partial
// unique index (user_id, name) WHERE deleted_at IS NULL that enforces
// uniqueness, so this is safe under concurrent calls.
func (r *Repo) GetOrCreateByName(ctx context.Context, userID int64, name string) (*companies_models.Company, error) {
	const q = `
		INSERT INTO companies (user_id, name)
		VALUES ($1, $2)
		ON CONFLICT (user_id, name) WHERE deleted_at IS NULL
		DO UPDATE SET name = EXCLUDED.name
		RETURNING ` + columns

	var c companies_models.Company
	err := scanRow(r.db.QueryRow(ctx, q, userID, name), &c)
	if err != nil {
		return nil, translate(err)
	}
	return &c, nil
}
