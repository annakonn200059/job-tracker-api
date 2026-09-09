package events_repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	appdomain "github.com/annakonn200059/job-tracker-api/domains/applications"
	apperr "github.com/annakonn200059/job-tracker-api/domains/errors"
	events_models "github.com/annakonn200059/job-tracker-api/domains/events"
	"github.com/annakonn200059/job-tracker-api/internal/infrastructure/database"
)

type Repo struct {
	db database.DBTX
}

func NewRepo(db database.DBTX) *Repo {
	return &Repo{db: db}
}

func (r *Repo) WithTx(tx pgx.Tx) *Repo {
	return &Repo{db: tx}
}

const columns = `
	id, user_id, application_id, contact_id, kind, from_stage, to_stage,
	title, body, created_by, occurred_at, created_at`

func scanRow(row pgx.Row, e *events_models.Event) error {
	return row.Scan(
		&e.ID, &e.UserID, &e.ApplicationID, &e.ContactID, &e.Kind,
		&e.FromStage, &e.ToStage, &e.Title, &e.Body,
		&e.CreatedBy, &e.OccurredAt, &e.CreatedAt,
	)
}

func (r *Repo) Create(ctx context.Context, e *events_models.Event) error {
	const q = `
		INSERT INTO application_events
		    (user_id, application_id, contact_id, kind, from_stage, to_stage,
		     title, body, created_by, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10, now()))
		RETURNING ` + columns

	var occurredAt *time.Time
	if !e.OccurredAt.IsZero() {
		occurredAt = &e.OccurredAt
	}

	err := scanRow(r.db.QueryRow(ctx, q,
		e.UserID, e.ApplicationID, e.ContactID, e.Kind,
		e.FromStage, e.ToStage, e.Title, e.Body, e.CreatedBy, occurredAt,
	), e)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503": // application or contact missing, or not this user's
			return apperr.ErrNotFound
		case "23514": // kind outside the allowed set, or stage_change without from/to
			return apperr.ErrValidation
		}
	}
	return err
}

// ListByApplication returns the timeline for one application, newest first.
func (r *Repo) ListByApplication(ctx context.Context, userID, applicationID int64) ([]events_models.Event, error) {
	const q = `SELECT ` + columns + `
		FROM application_events
		WHERE user_id = $1 AND application_id = $2
		ORDER BY occurred_at DESC, id DESC`

	rows, err := r.db.Query(ctx, q, userID, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []events_models.Event
	for rows.Next() {
		var e events_models.Event
		if err := scanRow(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// StageDurations reports how long each application spent in each stage,
// derived from consecutive stage_change events. This is the raw material
// for the funnel view.
func (r *Repo) StageDurations(ctx context.Context, userID int64, from, to time.Time) ([]StageDuration, error) {
	const q = `
		WITH transitions AS (
		    SELECT application_id,
		           to_stage AS stage,
		           occurred_at AS entered_at,
		           LEAD(occurred_at) OVER (
		               PARTITION BY application_id ORDER BY occurred_at
		           ) AS left_at
		    FROM application_events
		    WHERE user_id = $1
		      AND kind = 'stage_change'
		      AND occurred_at BETWEEN $2 AND $3
		)
		SELECT stage,
		       count(*) AS entered_count,
		       COALESCE(
		           EXTRACT(EPOCH FROM avg(left_at - entered_at)),
		           0
		       ) AS avg_seconds
		FROM transitions
		WHERE stage IS NOT NULL
		GROUP BY stage
		ORDER BY stage`

	rows, err := r.db.Query(ctx, q, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StageDuration
	for rows.Next() {
		var d StageDuration
		if err := rows.Scan(&d.Stage, &d.EnteredCount, &d.AvgSeconds); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type StageDuration struct {
	Stage        appdomain.Stage
	EnteredCount int64
	AvgSeconds   float64
}
