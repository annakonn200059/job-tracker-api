package vacancies_models

import (
	"errors"
	"time"
)

var (
	ErrTitleRequired         = errors.New("title is required")
	ErrInvalidWorkMode       = errors.New("invalid work mode")
	ErrInvalidEmploymentType = errors.New("invalid employment type")
	ErrInvalidSalaryPeriod   = errors.New("invalid salary period")
	ErrInvalidSalaryRange    = errors.New("salary_min exceeds salary_max")
	ErrInvalidSort           = errors.New("invalid sort field")
	ErrHasActiveApplication  = errors.New("vacancy has an active application")
)

type WorkMode string

const (
	WorkOnsite WorkMode = "onsite"
	WorkHybrid WorkMode = "hybrid"
	WorkRemote WorkMode = "remote"
)

func (w WorkMode) Valid() bool {
	switch w {
	case WorkOnsite, WorkHybrid, WorkRemote:
		return true
	}
	return false
}

type EmploymentType string

const (
	EmploymentFullTime   EmploymentType = "full_time"
	EmploymentPartTime   EmploymentType = "part_time"
	EmploymentContract   EmploymentType = "contract"
	EmploymentInternship EmploymentType = "internship"
)

func (e EmploymentType) Valid() bool {
	switch e {
	case EmploymentFullTime, EmploymentPartTime, EmploymentContract, EmploymentInternship:
		return true
	}
	return false
}

type SalaryPeriod string

const (
	SalaryHour  SalaryPeriod = "hour"
	SalaryDay   SalaryPeriod = "day"
	SalaryMonth SalaryPeriod = "month"
	SalaryYear  SalaryPeriod = "year"
)

func (p SalaryPeriod) Valid() bool {
	switch p {
	case SalaryHour, SalaryDay, SalaryMonth, SalaryYear:
		return true
	}
	return false
}

type Vacancy struct {
	ID             int64
	UserID         int64
	CompanyID      *int64
	Title          string
	URL            *string
	Description    *string
	Location       *string
	WorkMode       *WorkMode
	EmploymentType *EmploymentType
	Language       *string
	SalaryMin      *int32
	SalaryMax      *int32
	SalaryCurrency *string
	SalaryPeriod   *SalaryPeriod
	Source         *string
	PostedAt       *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Validate mirrors the CHECK constraints so bad input fails before it
// reaches the database, with a message that names the field.
func (v *Vacancy) Validate() error {
	if v.Title == "" {
		return ErrTitleRequired
	}
	if v.WorkMode != nil && !v.WorkMode.Valid() {
		return ErrInvalidWorkMode
	}
	if v.EmploymentType != nil && !v.EmploymentType.Valid() {
		return ErrInvalidEmploymentType
	}
	if v.SalaryPeriod != nil && !v.SalaryPeriod.Valid() {
		return ErrInvalidSalaryPeriod
	}
	if v.SalaryMin != nil && v.SalaryMax != nil && *v.SalaryMin > *v.SalaryMax {
		return ErrInvalidSalaryRange
	}
	return nil
}
