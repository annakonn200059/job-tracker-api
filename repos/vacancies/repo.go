package vacancies_repo

import (
	"context"
	"errors"

	errors_models "github.com/annakonn200059/job-tracker-api/domains/errors"
	vacancies_models "github.com/annakonn200059/job-tracker-api/domains/vacancies"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type VacancyRepo struct {
	pool *pgxpool.Pool
}

func NewVacancyRepo(pool *pgxpool.Pool) *VacancyRepo {
	return &VacancyRepo{pool: pool}
}

const vacancyColumns = `
	id, user_id, company_id, title, url, description,
	location, work_mode, language, salary_min, salary_max,
	source, created_at, updated_at`

func (r *VacancyRepo) GetByID(ctx context.Context, userID, id int64) (*vacancies_models.Vacancy, error) {
	const q = `SELECT ` + vacancyColumns + `
		FROM vacancies
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`

	var v vacancies_models.Vacancy
	err := r.pool.QueryRow(ctx, q, id, userID).Scan(
		&v.ID, &v.UserID, &v.CompanyID, &v.Title, &v.URL, &v.Description,
		&v.Location, &v.WorkMode, &v.Language, &v.SalaryMin, &v.SalaryMax,
		&v.Source, &v.CreatedAt, &v.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors_models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *VacancyRepo) List(ctx context.Context, userID int64, limit, offset int32) ([]vacancies_models.Vacancy, error) {
	const q = `SELECT ` + vacancyColumns + `
		FROM vacancies
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []vacancies_models.Vacancy
	for rows.Next() {
		var v vacancies_models.Vacancy
		if err := rows.Scan(
			&v.ID, &v.UserID, &v.CompanyID, &v.Title, &v.URL, &v.Description,
			&v.Location, &v.WorkMode, &v.Language, &v.SalaryMin, &v.SalaryMax,
			&v.Source, &v.CreatedAt, &v.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *VacancyRepo) Create(ctx context.Context, v *vacancies_models.Vacancy) error {
	const q = `
		INSERT INTO vacancies (user_id, company_id, title, url, description,
		                       location, work_mode, language, salary_min,
		                       salary_max, source)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, created_at, updated_at`

	return r.pool.QueryRow(ctx, q,
		v.UserID, v.CompanyID, v.Title, v.URL, v.Description,
		v.Location, v.WorkMode, v.Language, v.SalaryMin, v.SalaryMax, v.Source,
	).Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt)
}

func (r *VacancyRepo) SoftDelete(ctx context.Context, userID, id int64) error {
	const q = `
		UPDATE vacancies SET deleted_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`

	tag, err := r.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors_models.ErrNotFound
	}
	return nil
}
