package vacancies_models

type SortField string

const (
	SortCreatedAt SortField = "created_at"
	SortUpdatedAt SortField = "updated_at"
	SortPostedAt  SortField = "posted_at"
	SortTitle     SortField = "title"
	SortSalary    SortField = "salary"
)

type Filter struct {
	UserID          int64
	CompanyID       *int64
	WorkModes       []WorkMode
	EmploymentTypes []EmploymentType
	Languages       []string
	Sources         []string
	Search          *string // title or description
	SalaryFrom      *int32
	HasApplication  *bool // nil = any, true = applied to, false = not yet
	Sort            SortField
	Desc            bool
	Limit           int32
	Offset          int32
}

func (f *Filter) Normalize() {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Sort == "" {
		f.Sort = SortCreatedAt
		f.Desc = true
	}
}
