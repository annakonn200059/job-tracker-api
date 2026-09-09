package vacancies_api

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	errors_models "github.com/annakonn200059/job-tracker-api/domains/errors"
	vacancies_models "github.com/annakonn200059/job-tracker-api/domains/vacancies"
	apihttp "github.com/annakonn200059/job-tracker-api/http"
	vacancies_service "github.com/annakonn200059/job-tracker-api/services/vacancies"
)

// postedAtLayout is date-only to match the DATE column: a plain "2026-09-01"
// rather than a full RFC3339 timestamp.
const postedAtLayout = "2006-01-02"

type Handler struct {
	svc *vacancies_service.Service
}

func NewHandler(svc *vacancies_service.Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts the vacancy routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /vacancies", h.list)
	mux.HandleFunc("POST /vacancies", h.create)
	mux.HandleFunc("GET /vacancies/{id}", h.get)
	mux.HandleFunc("PATCH /vacancies/{id}", h.update)
	mux.HandleFunc("DELETE /vacancies/{id}", h.delete)
	mux.HandleFunc("POST /vacancies/{id}/restore", h.restore)
}

// ------------------------------------------------------------------ wire types

type vacancyRequest struct {
	CompanyID      *int64                           `json:"company_id"`
	CompanyName    *string                          `json:"company_name"` // create only; resolved to CompanyID
	Title          string                           `json:"title"`
	URL            *string                          `json:"url"`
	Description    *string                          `json:"description"`
	Location       *string                          `json:"location"`
	WorkMode       *vacancies_models.WorkMode       `json:"work_mode"`
	EmploymentType *vacancies_models.EmploymentType `json:"employment_type"`
	Language       *string                          `json:"language"`
	SalaryMin      *int32                           `json:"salary_min"`
	SalaryMax      *int32                           `json:"salary_max"`
	SalaryCurrency *string                          `json:"salary_currency"`
	SalaryPeriod   *vacancies_models.SalaryPeriod   `json:"salary_period"`
	Source         *string                          `json:"source"`
	PostedAt       *string                          `json:"posted_at"` // "YYYY-MM-DD"
}

func (b vacancyRequest) toVacancy(userID int64) (vacancies_models.Vacancy, error) {
	postedAt, err := parsePostedAt(b.PostedAt)
	if err != nil {
		return vacancies_models.Vacancy{}, err
	}
	return vacancies_models.Vacancy{
		UserID:         userID,
		CompanyID:      b.CompanyID,
		Title:          b.Title,
		URL:            b.URL,
		Description:    b.Description,
		Location:       b.Location,
		WorkMode:       b.WorkMode,
		EmploymentType: b.EmploymentType,
		Language:       b.Language,
		SalaryMin:      b.SalaryMin,
		SalaryMax:      b.SalaryMax,
		SalaryCurrency: b.SalaryCurrency,
		SalaryPeriod:   b.SalaryPeriod,
		Source:         b.Source,
		PostedAt:       postedAt,
	}, nil
}

func parsePostedAt(raw *string) (*time.Time, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	t, err := time.Parse(postedAtLayout, *raw)
	if err != nil {
		return nil, fmt.Errorf("%w: posted_at must be YYYY-MM-DD", errors_models.ErrValidation)
	}
	return &t, nil
}

type vacancyResponse struct {
	ID             int64                            `json:"id"`
	UserID         int64                            `json:"user_id"`
	CompanyID      *int64                           `json:"company_id,omitempty"`
	Title          string                           `json:"title"`
	URL            *string                          `json:"url,omitempty"`
	Description    *string                          `json:"description,omitempty"`
	Location       *string                          `json:"location,omitempty"`
	WorkMode       *vacancies_models.WorkMode       `json:"work_mode,omitempty"`
	EmploymentType *vacancies_models.EmploymentType `json:"employment_type,omitempty"`
	Language       *string                          `json:"language,omitempty"`
	SalaryMin      *int32                           `json:"salary_min,omitempty"`
	SalaryMax      *int32                           `json:"salary_max,omitempty"`
	SalaryCurrency *string                          `json:"salary_currency,omitempty"`
	SalaryPeriod   *vacancies_models.SalaryPeriod   `json:"salary_period,omitempty"`
	Source         *string                          `json:"source,omitempty"`
	PostedAt       *string                          `json:"posted_at,omitempty"` // "YYYY-MM-DD"
	CreatedAt      time.Time                        `json:"created_at"`
	UpdatedAt      time.Time                        `json:"updated_at"`
}

func formatPostedAt(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(postedAtLayout)
	return &s
}

func toVacancyResponse(v vacancies_models.Vacancy) vacancyResponse {
	return vacancyResponse{
		ID:             v.ID,
		UserID:         v.UserID,
		CompanyID:      v.CompanyID,
		Title:          v.Title,
		URL:            v.URL,
		Description:    v.Description,
		Location:       v.Location,
		WorkMode:       v.WorkMode,
		EmploymentType: v.EmploymentType,
		Language:       v.Language,
		SalaryMin:      v.SalaryMin,
		SalaryMax:      v.SalaryMax,
		SalaryCurrency: v.SalaryCurrency,
		SalaryPeriod:   v.SalaryPeriod,
		Source:         v.Source,
		PostedAt:       formatPostedAt(v.PostedAt),
		CreatedAt:      v.CreatedAt,
		UpdatedAt:      v.UpdatedAt,
	}
}

func toVacancyResponses(in []vacancies_models.Vacancy) []vacancyResponse {
	out := make([]vacancyResponse, len(in))
	for i, v := range in {
		out[i] = toVacancyResponse(v)
	}
	return out
}

type listResponse struct {
	Items []vacancyResponse `json:"items"`
	Total int64             `json:"total"`
}

// ------------------------------------------------------------------ handlers

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, err := apihttp.UserID(r)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	f, err := parseFilter(r.URL.Query(), userID)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	items, total, err := h.svc.List(r.Context(), f)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusOK, listResponse{
		Items: toVacancyResponses(items),
		Total: total,
	})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, err := apihttp.UserID(r)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	id, err := apihttp.PathID(r, "id")
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	v, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusOK, toVacancyResponse(*v))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, err := apihttp.UserID(r)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	var body vacancyRequest
	if err := apihttp.DecodeJSON(w, r, &body); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	vacancy, err := body.toVacancy(userID)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	v, err := h.svc.Create(r.Context(), vacancies_service.CreateParams{
		Vacancy:     vacancy,
		CompanyName: body.CompanyName,
	})
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusCreated, toVacancyResponse(*v))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID, err := apihttp.UserID(r)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	id, err := apihttp.PathID(r, "id")
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	var body vacancyRequest
	if err := apihttp.DecodeJSON(w, r, &body); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	vacancy, err := body.toVacancy(userID)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	vacancy.ID = id

	v, err := h.svc.Update(r.Context(), &vacancy)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusOK, toVacancyResponse(*v))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, err := apihttp.UserID(r)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	id, err := apihttp.PathID(r, "id")
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	userID, err := apihttp.UserID(r)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	id, err := apihttp.PathID(r, "id")
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	if err := h.svc.Restore(r.Context(), userID, id); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------------ filter parsing

func parseFilter(q url.Values, userID int64) (vacancies_models.Filter, error) {
	f := vacancies_models.Filter{UserID: userID}

	companyID, err := apihttp.QueryInt64(q, "company_id")
	if err != nil {
		return f, err
	}
	f.CompanyID = companyID

	salaryFrom, err := apihttp.QueryInt32(q, "salary_from")
	if err != nil {
		return f, err
	}
	f.SalaryFrom = salaryFrom

	hasApplication, err := apihttp.QueryBool(q, "has_application")
	if err != nil {
		return f, err
	}
	f.HasApplication = hasApplication

	desc, err := apihttp.QueryBool(q, "desc")
	if err != nil {
		return f, err
	}
	if desc != nil {
		f.Desc = *desc
	}

	limit, err := apihttp.QueryInt32(q, "limit")
	if err != nil {
		return f, err
	}
	if limit != nil {
		f.Limit = *limit
	}

	offset, err := apihttp.QueryInt32(q, "offset")
	if err != nil {
		return f, err
	}
	if offset != nil {
		f.Offset = *offset
	}

	f.WorkModes = toWorkModes(apihttp.QueryCSV(q, "work_mode"))
	f.EmploymentTypes = toEmploymentTypes(apihttp.QueryCSV(q, "employment_type"))
	f.Languages = apihttp.QueryCSV(q, "language")
	f.Sources = apihttp.QueryCSV(q, "source")
	f.Search = apihttp.QueryString(q, "q")
	f.Sort = vacancies_models.SortField(q.Get("sort"))

	return f, nil
}

func toWorkModes(in []string) []vacancies_models.WorkMode {
	if in == nil {
		return nil
	}
	out := make([]vacancies_models.WorkMode, len(in))
	for i, s := range in {
		out[i] = vacancies_models.WorkMode(s)
	}
	return out
}

func toEmploymentTypes(in []string) []vacancies_models.EmploymentType {
	if in == nil {
		return nil
	}
	out := make([]vacancies_models.EmploymentType, len(in))
	for i, s := range in {
		out[i] = vacancies_models.EmploymentType(s)
	}
	return out
}
