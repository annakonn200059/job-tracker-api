package vacancies_models

import (
	"time"
)

type Vacancy struct {
	ID          int64
	UserID      int64
	CompanyID   *int64
	Title       string
	URL         *string
	Description *string
	Location    *string
	WorkMode    *string
	Language    *string
	SalaryMin   *int32
	SalaryMax   *int32
	Source      *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
