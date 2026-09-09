package vacancies_service

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	vacancies_models "github.com/annakonn200059/job-tracker-api/domains/vacancies"
	"github.com/annakonn200059/job-tracker-api/internal/infrastructure/database"
	companies_repo "github.com/annakonn200059/job-tracker-api/repos/companies"
	vacancies_repo "github.com/annakonn200059/job-tracker-api/repos/vacancies"
)

type Service struct {
	pool      *pgxpool.Pool
	vacancies *vacancies_repo.Repo
	companies *companies_repo.Repo
}

func NewVacanciesService(pool *pgxpool.Pool, v *vacancies_repo.Repo, c *companies_repo.Repo) *Service {
	return &Service{pool: pool, vacancies: v, companies: c}
}

func (s *Service) Get(ctx context.Context, userID, id int64) (*vacancies_models.Vacancy, error) {
	return s.vacancies.GetByID(ctx, userID, id)
}

func (s *Service) List(ctx context.Context, f vacancies_models.Filter) ([]vacancies_models.Vacancy, int64, error) {
	items, err := s.vacancies.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.vacancies.Count(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// CreateParams allows a company name instead of an ID: pasting a job posting
// shouldn't require creating the company first.
type CreateParams struct {
	Vacancy     vacancies_models.Vacancy
	CompanyName *string
}

// Create resolves the company name to an ID (creating the company if
// needed) and inserts the vacancy in one transaction, so a failed insert
// never leaves a vacancy pointing at a company from a half-finished call.
func (s *Service) Create(ctx context.Context, p CreateParams) (*vacancies_models.Vacancy, error) {
	v := p.Vacancy

	if err := v.Validate(); err != nil {
		return nil, err
	}

	err := database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		companies := s.companies.WithTx(tx)
		vacancies := s.vacancies.WithTx(tx)

		if v.CompanyID == nil && p.CompanyName != nil {
			name := strings.TrimSpace(*p.CompanyName)
			if name != "" {
				company, err := companies.GetOrCreateByName(ctx, v.UserID, name)
				if err != nil {
					return err
				}
				v.CompanyID = &company.ID
			}
		}

		return vacancies.Create(ctx, &v)
	})
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Service) Update(ctx context.Context, v *vacancies_models.Vacancy) (*vacancies_models.Vacancy, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return s.vacancies.Update(ctx, v)
}

// Delete refuses while an active application still references the vacancy —
// otherwise a card on the board would point at nothing.
func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	has, err := s.vacancies.HasActiveApplication(ctx, userID, id)
	if err != nil {
		return err
	}
	if has {
		return vacancies_models.ErrHasActiveApplication
	}
	return s.vacancies.SoftDelete(ctx, userID, id)
}

func (s *Service) Restore(ctx context.Context, userID, id int64) error {
	return s.vacancies.Restore(ctx, userID, id)
}
