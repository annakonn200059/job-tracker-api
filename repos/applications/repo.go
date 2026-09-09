package applications_repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	domain "github.com/annakonn200059/job-tracker-api/domains/applications"
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
	a.id, a.user_id, a.vacancy_id, a.stage, a.board_order, a.priority,
	a.applied_at, a.closed_at, a.notes, a.created_at, a.updated_at`

func scanRow(row pgx.Row, a *domain.Application) error {
	return row.Scan(
		&a.ID, &a.UserID, &a.VacancyID, &a.Stage, &a.BoardOrder, &a.Priority,
		&a.AppliedAt, &a.ClosedAt, &a.Notes, &a.CreatedAt, &a.UpdatedAt,
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
	case "23505": // unique_violation on ux_applications_vacancy_active
		return domain.ErrAlreadyApplied
	case "23503": // foreign_key_violation: vacancy missing or owned by another user
		return apperr.ErrNotFound
	case "23514": // check_violation on the stage CHECK
		return domain.ErrInvalidStage
	}
	return err
}

// ------------------------------------------------------------------ reads

func (r *Repo) GetByID(ctx context.Context, userID, id int64) (*domain.Application, error) {
	const q = `SELECT ` + columns + `
		FROM applications a
		WHERE a.id = $1 AND a.user_id = $2 AND a.deleted_at IS NULL`

	var a domain.Application
	err := scanRow(r.db.QueryRow(ctx, q, id, userID), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetByIDForUpdate locks the row for the duration of the surrounding
// transaction. Only meaningful on a tx-bound repo.
func (r *Repo) GetByIDForUpdate(ctx context.Context, userID, id int64) (*domain.Application, error) {
	const q = `SELECT ` + columns + `
		FROM applications a
		WHERE a.id = $1 AND a.user_id = $2 AND a.deleted_at IS NULL
		FOR UPDATE`

	var a domain.Application
	err := scanRow(r.db.QueryRow(ctx, q, id, userID), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// sortSQL whitelists ORDER BY expressions. ORDER BY cannot be parameterised,
// so this map is the injection boundary.
var sortSQL = map[domain.SortField]string{
	domain.SortBoard:     "a.stage, a.board_order",
	domain.SortCreatedAt: "a.created_at",
	domain.SortUpdatedAt: "a.updated_at",
	domain.SortPriority:  "a.priority",
}

const filterWhere = `
	WHERE a.user_id = $1
	  AND a.deleted_at IS NULL
	  AND (cardinality($2::text[])   = 0 OR a.stage      = ANY($2))
	  AND (cardinality($3::bigint[]) = 0 OR a.vacancy_id = ANY($3))
	  AND ($4::bigint   IS NULL OR v.company_id = $4)
	  AND ($5::smallint IS NULL OR a.priority  >= $5)
	  AND ($6::text     IS NULL OR v.title ILIKE '%' || $6 || '%'
	                            OR c.name  ILIKE '%' || $6 || '%')
	  AND (cardinality($7::bigint[]) = 0 OR EXISTS (
	        SELECT 1 FROM application_tags at
	        WHERE at.application_id = a.id AND at.tag_id = ANY($7)))`

const filterFrom = `
	FROM applications a
	JOIN vacancies v      ON v.id = a.vacancy_id AND v.deleted_at IS NULL
	LEFT JOIN companies c ON c.id = v.company_id AND c.deleted_at IS NULL`

func filterArgs(f domain.Filter) []any {
	return []any{
		f.UserID,
		stagesToText(f.Stages),
		int64Slice(f.VacancyIDs),
		f.CompanyID,
		f.MinPriority,
		f.Search,
		int64Slice(f.TagIDs),
	}
}

func (r *Repo) List(ctx context.Context, f domain.Filter) ([]domain.Application, error) {
	f.Normalize()

	order, ok := sortSQL[f.Sort]
	if !ok {
		return nil, domain.ErrInvalidSort
	}
	if f.Desc {
		order += " DESC"
	}

	q := `SELECT ` + columns + filterFrom + filterWhere +
		` ORDER BY ` + order + ` LIMIT $8 OFFSET $9`

	rows, err := r.db.Query(ctx, q, append(filterArgs(f), f.Limit, f.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Application, 0, f.Limit)
	for rows.Next() {
		var a domain.Application
		if err := scanRow(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) Count(ctx context.Context, f domain.Filter) (int64, error) {
	q := `SELECT count(*)` + filterFrom + filterWhere

	var n int64
	err := r.db.QueryRow(ctx, q, filterArgs(f)...).Scan(&n)
	return n, err
}

// Neighbours returns the board_order values immediately before and after the
// given position in a stage column. The service uses them to compute a
// midpoint when a card is dropped.
func (r *Repo) Neighbours(ctx context.Context, userID int64, stage domain.Stage, afterID, beforeID *int64) (prev, next *float64, err error) {
	const q = `
		SELECT
		    (SELECT board_order FROM applications
		      WHERE user_id = $1 AND id = $2 AND stage = $3 AND deleted_at IS NULL),
		    (SELECT board_order FROM applications
		      WHERE user_id = $1 AND id = $4 AND stage = $3 AND deleted_at IS NULL)`

	err = r.db.QueryRow(ctx, q, userID, afterID, stage, beforeID).Scan(&prev, &next)
	return prev, next, err
}

// MaxOrder returns the highest board_order in a stage column, or nil if empty.
func (r *Repo) MaxOrder(ctx context.Context, userID int64, stage domain.Stage) (*float64, error) {
	const q = `
		SELECT MAX(board_order) FROM applications
		WHERE user_id = $1 AND stage = $2 AND deleted_at IS NULL`

	var max *float64
	err := r.db.QueryRow(ctx, q, userID, stage).Scan(&max)
	return max, err
}

// MinGap reports the smallest distance between adjacent cards in a column.
// The service uses it to decide when a renumber is due.
func (r *Repo) MinGap(ctx context.Context, userID int64, stage domain.Stage) (float64, error) {
	const q = `
		WITH ordered AS (
		    SELECT board_order,
		           LEAD(board_order) OVER (ORDER BY board_order) AS next_order
		    FROM applications
		    WHERE user_id = $1 AND stage = $2 AND deleted_at IS NULL
		)
		SELECT COALESCE(MIN(next_order - board_order), 1000)
		FROM ordered WHERE next_order IS NOT NULL`

	var gap float64
	err := r.db.QueryRow(ctx, q, userID, stage).Scan(&gap)
	return gap, err
}

// ----------------------------------------------------------------- writes

// Create inserts a row exactly as given. Computing board_order and choosing
// the initial stage are the service's decisions.
func (r *Repo) Create(ctx context.Context, a *domain.Application) error {
	const q = `
		INSERT INTO applications
		    (user_id, vacancy_id, stage, board_order, priority, notes, applied_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, user_id, vacancy_id, stage, board_order, priority,
		          applied_at, closed_at, notes, created_at, updated_at`

	err := r.db.QueryRow(ctx, q,
		a.UserID, a.VacancyID, a.Stage, a.BoardOrder, a.Priority, a.Notes, a.AppliedAt,
	).Scan(&a.ID, &a.UserID, &a.VacancyID, &a.Stage, &a.BoardOrder, &a.Priority,
		&a.AppliedAt, &a.ClosedAt, &a.Notes, &a.CreatedAt, &a.UpdatedAt)

	return translate(err)
}

func (r *Repo) Update(ctx context.Context, userID, id int64, priority int16, notes *string) (*domain.Application, error) {
	const q = `
		UPDATE applications a
		SET priority = $3, notes = $4
		WHERE a.id = $1 AND a.user_id = $2 AND a.deleted_at IS NULL
		RETURNING ` + columns

	var out domain.Application
	err := scanRow(r.db.QueryRow(ctx, q, id, userID, priority, notes), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, translate(err)
	}
	return &out, nil
}

// SetStage writes stage, board_order and the derived timestamps. All four
// values are computed by the service; this method only persists them.
func (r *Repo) SetStage(ctx context.Context, userID, id int64, stage domain.Stage, order float64, appliedAt, closedAt *time.Time) (*domain.Application, error) {
	const q = `
		UPDATE applications a
		SET stage = $3, board_order = $4, applied_at = $5, closed_at = $6
		WHERE a.id = $1 AND a.user_id = $2 AND a.deleted_at IS NULL
		RETURNING ` + columns

	var out domain.Application
	err := scanRow(r.db.QueryRow(ctx, q, id, userID, stage, order, appliedAt, closedAt), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, translate(err)
	}
	return &out, nil
}

// Renumber rewrites board_order for one column with even 1000-unit gaps.
func (r *Repo) Renumber(ctx context.Context, userID int64, stage domain.Stage) error {
	const q = `
		WITH ordered AS (
		    SELECT id, row_number() OVER (ORDER BY board_order, id) * 1000 AS pos
		    FROM applications
		    WHERE user_id = $1 AND stage = $2 AND deleted_at IS NULL
		)
		UPDATE applications a
		SET board_order = o.pos
		FROM ordered o
		WHERE a.id = o.id`

	_, err := r.db.Exec(ctx, q, userID, stage)
	return err
}

func (r *Repo) SoftDelete(ctx context.Context, userID, id int64) error {
	const q = `
		UPDATE applications SET deleted_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *Repo) Restore(ctx context.Context, userID, id int64) error {
	const q = `
		UPDATE applications SET deleted_at = NULL
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NOT NULL`

	tag, err := r.db.Exec(ctx, q, id, userID)
	if err != nil {
		// An active application for the same vacancy may already exist.
		return translate(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- helpers

func stagesToText(stages []domain.Stage) []string {
	out := make([]string, len(stages))
	for i, s := range stages {
		out[i] = string(s)
	}
	return out
}

// int64Slice normalises nil to an empty slice: cardinality(NULL) is NULL,
// which would make the whole predicate NULL and silently drop every row.
func int64Slice(in []int64) []int64 {
	if in == nil {
		return []int64{}
	}
	return in
}
