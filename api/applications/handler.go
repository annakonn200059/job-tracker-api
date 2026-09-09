package applications_api

import (
	"net/http"
	"net/url"
	"time"

	applications_models "github.com/annakonn200059/job-tracker-api/domains/applications"
	apihttp "github.com/annakonn200059/job-tracker-api/http"
	applications_service "github.com/annakonn200059/job-tracker-api/services/applications"
)

type Handler struct {
	svc *applications_service.Service
}

func NewHandler(svc *applications_service.Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts the application routes on mux. The literal "/board"
// segment takes precedence over the "{id}" wildcard at the same position,
// so route order here doesn't matter.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /applications", h.list)
	mux.HandleFunc("GET /applications/board", h.board)
	mux.HandleFunc("POST /applications", h.apply)
	mux.HandleFunc("GET /applications/{id}", h.get)
	mux.HandleFunc("PATCH /applications/{id}", h.update)
	mux.HandleFunc("PATCH /applications/{id}/stage", h.changeStage)
	mux.HandleFunc("POST /applications/{id}/move", h.move)
	mux.HandleFunc("DELETE /applications/{id}", h.delete)
}

// ------------------------------------------------------------------ wire types

type applyRequest struct {
	VacancyID int64                      `json:"vacancy_id"`
	Stage     *applications_models.Stage `json:"stage"` // defaults to "saved"
}

type updateRequest struct {
	Priority int16   `json:"priority"`
	Notes    *string `json:"notes"`
}

type changeStageRequest struct {
	To applications_models.Stage `json:"to"`
}

type moveRequest struct {
	Stage    applications_models.Stage `json:"stage"`
	AfterID  *int64                    `json:"after_id"`
	BeforeID *int64                    `json:"before_id"`
}

type applicationResponse struct {
	ID         int64                     `json:"id"`
	UserID     int64                     `json:"user_id"`
	VacancyID  int64                     `json:"vacancy_id"`
	Stage      applications_models.Stage `json:"stage"`
	BoardOrder float64                   `json:"board_order"`
	Priority   int16                     `json:"priority"`
	AppliedAt  *time.Time                `json:"applied_at,omitempty"`
	ClosedAt   *time.Time                `json:"closed_at,omitempty"`
	Notes      *string                   `json:"notes,omitempty"`
	CreatedAt  time.Time                 `json:"created_at"`
	UpdatedAt  time.Time                 `json:"updated_at"`
}

func toApplicationResponse(a applications_models.Application) applicationResponse {
	return applicationResponse{
		ID:         a.ID,
		UserID:     a.UserID,
		VacancyID:  a.VacancyID,
		Stage:      a.Stage,
		BoardOrder: a.BoardOrder,
		Priority:   a.Priority,
		AppliedAt:  a.AppliedAt,
		ClosedAt:   a.ClosedAt,
		Notes:      a.Notes,
		CreatedAt:  a.CreatedAt,
		UpdatedAt:  a.UpdatedAt,
	}
}

func toApplicationResponses(in []applications_models.Application) []applicationResponse {
	out := make([]applicationResponse, len(in))
	for i, a := range in {
		out[i] = toApplicationResponse(a)
	}
	return out
}

type listResponse struct {
	Items []applicationResponse `json:"items"`
	Total int64                 `json:"total"`
}

type boardResponse map[applications_models.Stage][]applicationResponse

func toBoardResponse(b applications_service.Board) boardResponse {
	out := make(boardResponse, len(b))
	for stage, apps := range b {
		out[stage] = toApplicationResponses(apps)
	}
	return out
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
		Items: toApplicationResponses(items),
		Total: total,
	})
}

func (h *Handler) board(w http.ResponseWriter, r *http.Request) {
	userID, err := apihttp.UserID(r)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	b, err := h.svc.Board(r.Context(), userID)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusOK, toBoardResponse(b))
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

	a, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusOK, toApplicationResponse(*a))
}

func (h *Handler) apply(w http.ResponseWriter, r *http.Request) {
	userID, err := apihttp.UserID(r)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	var body applyRequest
	if err := apihttp.DecodeJSON(w, r, &body); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	stage := applications_models.StageSaved
	if body.Stage != nil {
		stage = *body.Stage
	}

	a, err := h.svc.Apply(r.Context(), userID, body.VacancyID, stage)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusCreated, toApplicationResponse(*a))
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

	var body updateRequest
	if err := apihttp.DecodeJSON(w, r, &body); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	a, err := h.svc.Update(r.Context(), userID, id, body.Priority, body.Notes)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusOK, toApplicationResponse(*a))
}

func (h *Handler) changeStage(w http.ResponseWriter, r *http.Request) {
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

	var body changeStageRequest
	if err := apihttp.DecodeJSON(w, r, &body); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	a, err := h.svc.ChangeStage(r.Context(), userID, id, body.To)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusOK, toApplicationResponse(*a))
}

func (h *Handler) move(w http.ResponseWriter, r *http.Request) {
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

	var body moveRequest
	if err := apihttp.DecodeJSON(w, r, &body); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	a, err := h.svc.MoveCard(r.Context(), userID, id, body.Stage, body.AfterID, body.BeforeID)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}

	apihttp.WriteJSON(w, http.StatusOK, toApplicationResponse(*a))
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

// ------------------------------------------------------------------ filter parsing

func parseFilter(q url.Values, userID int64) (applications_models.Filter, error) {
	f := applications_models.Filter{UserID: userID}

	companyID, err := apihttp.QueryInt64(q, "company_id")
	if err != nil {
		return f, err
	}
	f.CompanyID = companyID

	minPriority, err := apihttp.QueryInt16(q, "min_priority")
	if err != nil {
		return f, err
	}
	f.MinPriority = minPriority

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

	vacancyIDs, err := apihttp.QueryCSVInt64(q, "vacancy_id")
	if err != nil {
		return f, err
	}
	f.VacancyIDs = vacancyIDs

	tagIDs, err := apihttp.QueryCSVInt64(q, "tag_id")
	if err != nil {
		return f, err
	}
	f.TagIDs = tagIDs

	f.Stages = toStages(apihttp.QueryCSV(q, "stage"))
	f.Search = apihttp.QueryString(q, "q")
	f.Sort = applications_models.SortField(q.Get("sort"))

	return f, nil
}

func toStages(in []string) []applications_models.Stage {
	if in == nil {
		return nil
	}
	out := make([]applications_models.Stage, len(in))
	for i, s := range in {
		out[i] = applications_models.Stage(s)
	}
	return out
}
