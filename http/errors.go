package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	applications_models "github.com/annakonn200059/job-tracker-api/domains/applications"
	auth_models "github.com/annakonn200059/job-tracker-api/domains/auth"
	companies_models "github.com/annakonn200059/job-tracker-api/domains/companies"
	errors_models "github.com/annakonn200059/job-tracker-api/domains/errors"
	users_models "github.com/annakonn200059/job-tracker-api/domains/users"
	vacancies_models "github.com/annakonn200059/job-tracker-api/domains/vacancies"
)

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// WriteError maps an error returned by a service/repo to an HTTP status and
// writes it as JSON. Repos translate Postgres constraint violations into
// domain sentinels (e.g. applications_models.ErrInvalidStage), and those
// sentinels are distinct error values, not wrapped copies of
// errors_models.ErrValidation/ErrConflict — so each one needs its own entry
// below, or it silently falls through to 500.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusInternalServerError, "internal_error"

	switch {
	case errors.Is(err, errors_models.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"

	case errors.Is(err, errors_models.ErrUnauthorized),
		errors.Is(err, auth_models.ErrInvalidCredentials),
		errors.Is(err, auth_models.ErrInvalidGoogleToken):
		status, code = http.StatusUnauthorized, "unauthorized"

	case errors.Is(err, errors_models.ErrForbidden):
		status, code = http.StatusForbidden, "forbidden"

	case errors.Is(err, errors_models.ErrConflict),
		errors.Is(err, applications_models.ErrAlreadyApplied),
		errors.Is(err, applications_models.ErrSameStage),
		errors.Is(err, vacancies_models.ErrHasActiveApplication),
		errors.Is(err, users_models.ErrEmailTaken):
		status, code = http.StatusConflict, "conflict"

	case errors.Is(err, errors_models.ErrValidation),
		errors.Is(err, applications_models.ErrInvalidStage),
		errors.Is(err, applications_models.ErrInvalidSort),
		errors.Is(err, vacancies_models.ErrTitleRequired),
		errors.Is(err, vacancies_models.ErrInvalidWorkMode),
		errors.Is(err, vacancies_models.ErrInvalidEmploymentType),
		errors.Is(err, vacancies_models.ErrInvalidSalaryPeriod),
		errors.Is(err, vacancies_models.ErrInvalidSalaryRange),
		errors.Is(err, vacancies_models.ErrInvalidSort),
		errors.Is(err, companies_models.ErrNameRequired),
		errors.Is(err, users_models.ErrInvalidEmail),
		errors.Is(err, auth_models.ErrPasswordTooShort),
		errors.Is(err, auth_models.ErrPasswordTooLong),
		errors.Is(err, auth_models.ErrGoogleEmailUnverified):
		status, code = http.StatusBadRequest, "validation_failed"
	}

	if status >= 500 {
		slog.ErrorContext(r.Context(), "request failed", "err", err, "path", r.URL.Path)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: code, Message: publicMessage(status, err)})
}

func publicMessage(status int, err error) string {
	if status >= 500 {
		return "internal server error"
	}
	return err.Error()
}
