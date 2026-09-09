package applications_models

type SortField string

const (
	SortBoard     SortField = "board"
	SortCreatedAt SortField = "created_at"
	SortUpdatedAt SortField = "updated_at"
	SortPriority  SortField = "priority"
)

type Filter struct {
	UserID      int64   // always required
	Stages      []Stage // empty = all
	VacancyIDs  []int64
	CompanyID   *int64
	TagIDs      []int64
	Search      *string // matches vacancy title / company name
	MinPriority *int16
	Sort        SortField
	Desc        bool
	Limit       int32
	Offset      int32
}

func (f *Filter) Normalize() {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Sort == "" {
		f.Sort = SortBoard
	}
}
