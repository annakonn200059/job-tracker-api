package vacancies_repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	apperr "github.com/annakonn200059/job-tracker-api/domains/errors"
	vacancies_models "github.com/annakonn200059/job-tracker-api/domains/vacancies"
	"github.com/annakonn200059/job-tracker-api/internal/infrastructure/database"
)

type Repo struct {
	db database.DBTX
}

func NewRepo(db database.DBTX) *Repo { return &Repo{db: db} }

func (r *Repo) WithTx(tx pgx.Tx) *Repo { return &Repo{db: tx} }

const columns = `
	v.id, v.user_id, v.company_id, v.title, v.url, v.description, v.location,
	v.work_mode, v.employment_type, v.language, v.salary_min, v.salary_max,
	v.salary_currency, v.salary_period, v.source, v.posted_at,
	v.created_at, v.updated_at`

func scanRow(row pgx.Row, v *vacancies_models.Vacancy) error {
	return row.Scan(
		&v.ID, &v.UserID, &v.CompanyID, &v.Title, &v.URL, &v.Description, &v.Location,
		&v.WorkMode, &v.EmploymentType, &v.Language, &v.SalaryMin, &v.SalaryMax,
		&v.SalaryCurrency, &v.SalaryPeriod, &v.Source, &v.PostedAt,
		&v.CreatedAt, &v.UpdatedAt,
	)
}

func translate(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503": // company_id points at a company that isn't this user's
		return apperr.ErrNotFound
	case "23514": // work_mode / employment_type / salary range CHECK
		return apperr.ErrValidation
	}
	return err
}

// ------------------------------------------------------------------ reads

func (r *Repo) GetByID(ctx context.Context, userID, id int64) (*vacancies_models.Vacancy, error) {
	const q = `SELECT ` + columns + `
		FROM vacancies v
		WHERE v.id = $1 AND v.user_id = $2 AND v.deleted_at IS NULL`

	var v vacancies_models.Vacancy
	err := scanRow(r.db.QueryRow(ctx, q, id, userID), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

var sortSQL = map[vacancies_models.SortField]string{
	vacancies_models.SortCreatedAt: "v.created_at",
	vacancies_models.SortUpdatedAt: "v.updated_at",
	vacancies_models.SortPostedAt:  "v.posted_at",
	vacancies_models.SortTitle:     "v.title",
	vacancies_models.SortSalary:    "v.salary_max",
}

const filterFrom = `
	FROM vacancies v
	LEFT JOIN companies c ON c.id = v.company_id AND c.deleted_at IS NULL`

const filterWhere = `
	WHERE v.user_id = $1
	  AND v.deleted_at IS NULL
	  AND ($2::bigint IS NULL OR v.company_id = $2)
	  AND (cardinality($3::text[]) = 0 OR v.work_mode       = ANY($3))
	  AND (cardinality($4::text[]) = 0 OR v.employment_type = ANY($4))
	  AND (cardinality($5::text[]) = 0 OR v.language        = ANY($5))
	  AND (cardinality($6::text[]) = 0 OR v.source          = ANY($6))
	  AND ($7::text  IS NULL OR v.title       ILIKE '%' || $7 || '%'
	                         OR v.description ILIKE '%' || $7 || '%'
	                         OR c.name        ILIKE '%' || $7 || '%')
	  AND ($8::int IS NULL OR v.salary_max >= $8)
	  AND ($9::bool IS NULL OR $9 = EXISTS (
	        SELECT 1 FROM applications a
	        WHERE a.vacancy_id = v.id AND a.deleted_at IS NULL))`

func filterArgs(f vacancies_models.Filter) []any {
	return []any{
		f.UserID,
		f.CompanyID,
		workModesToText(f.WorkModes),
		employmentToText(f.EmploymentTypes),
		textSlice(f.Languages),
		textSlice(f.Sources),
		f.Search,
		f.SalaryFrom,
		f.HasApplication,
	}
}

func (r *Repo) List(ctx context.Context, f vacancies_models.Filter) ([]vacancies_models.Vacancy, error) {
	f.Normalize()

	order, ok := sortSQL[f.Sort]
	if !ok {
		return nil, vacancies_models.ErrInvalidSort
	}
	if f.Desc {
		order += " DESC NULLS LAST"
	} else {
		order += " ASC NULLS LAST"
	}

	q := `SELECT ` + columns + filterFrom + filterWhere +
		` ORDER BY ` + order + `, v.id LIMIT $10 OFFSET $11`

	rows, err := r.db.Query(ctx, q, append(filterArgs(f), f.Limit, f.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]vacancies_models.Vacancy, 0, f.Limit)
	for rows.Next() {
		var v vacancies_models.Vacancy
		if err := scanRow(rows, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repo) Count(ctx context.Context, f vacancies_models.Filter) (int64, error) {
	q := `SELECT count(*)` + filterFrom + filterWhere

	var n int64
	err := r.db.QueryRow(ctx, q, filterArgs(f)...).Scan(&n)
	return n, err
}

// HasActiveApplication reports whether an application still points here.
// The service uses it to refuse deletion that would orphan a card.
func (r *Repo) HasActiveApplication(ctx context.Context, userID, vacancyID int64) (bool, error) {
	const q = `
		SELECT EXISTS (
		    SELECT 1 FROM applications
		    WHERE user_id = $1 AND vacancy_id = $2 AND deleted_at IS NULL)`

	var exists bool
	err := r.db.QueryRow(ctx, q, userID, vacancyID).Scan(&exists)
	return exists, err
}

// ----------------------------------------------------------------- writes

func (r *Repo) Create(ctx context.Context, v *vacancies_models.Vacancy) error {
	const q = `
		INSERT INTO vacancies
		    (user_id, company_id, title, url, description, location,
		     work_mode, employment_type, language, salary_min, salary_max,
		     salary_currency, salary_period, source, posted_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, user_id, company_id, title, url, description, location,
		          work_mode, employment_type, language, salary_min, salary_max,
		          salary_currency, salary_period, source, posted_at,
		          created_at, updated_at`

	err := r.db.QueryRow(ctx, q,
		v.UserID, v.CompanyID, v.Title, v.URL, v.Description, v.Location,
		v.WorkMode, v.EmploymentType, v.Language, v.SalaryMin, v.SalaryMax,
		v.SalaryCurrency, v.SalaryPeriod, v.Source, v.PostedAt,
	).Scan(
		&v.ID, &v.UserID, &v.CompanyID, &v.Title, &v.URL, &v.Description, &v.Location,
		&v.WorkMode, &v.EmploymentType, &v.Language, &v.SalaryMin, &v.SalaryMax,
		&v.SalaryCurrency, &v.SalaryPeriod, &v.Source, &v.PostedAt,
		&v.CreatedAt, &v.UpdatedAt,
	)
	return translate(err)
}

func (r *Repo) Update(ctx context.Context, v *vacancies_models.Vacancy) (*vacancies_models.Vacancy, error) {
	const q = `
		UPDATE vacancies v SET
		    company_id      = $3,
		    title           = $4,
		    url             = $5,
		    description     = $6,
		    location        = $7,
		    work_mode       = $8,
		    employment_type = $9,
		    language        = $10,
		    salary_min      = $11,
		    salary_max      = $12,
		    salary_currency = $13,
		    salary_period   = $14,
		    source          = $15,
		    posted_at       = $16
		WHERE v.id = $1 AND v.user_id = $2 AND v.deleted_at IS NULL
		RETURNING ` + columns

	var out vacancies_models.Vacancy
	err := scanRow(r.db.QueryRow(ctx, q,
		v.ID, v.UserID, v.CompanyID, v.Title, v.URL, v.Description, v.Location,
		v.WorkMode, v.EmploymentType, v.Language, v.SalaryMin, v.SalaryMax,
		v.SalaryCurrency, v.SalaryPeriod, v.Source, v.PostedAt,
	), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, translate(err)
	}
	return &out, nil
}

func (r *Repo) SoftDelete(ctx context.Context, userID, id int64) error {
	const q = `
		UPDATE vacancies SET deleted_at = now()
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
		UPDATE vacancies SET deleted_at = NULL
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NOT NULL`

	tag, err := r.db.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- helpers

func workModesToText(in []vacancies_models.WorkMode) []string {
	out := make([]string, len(in))
	for i, w := range in {
		out[i] = string(w)
	}
	return out
}

func employmentToText(in []vacancies_models.EmploymentType) []string {
	out := make([]string, len(in))
	for i, e := range in {
		out[i] = string(e)
	}
	return out
}

// textSlice normalises nil to empty: cardinality(NULL) is NULL, which would
// make the whole predicate NULL and silently drop every row.
func textSlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
