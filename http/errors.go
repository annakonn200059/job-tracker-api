package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	errors_models "github.com/annakonn200059/job-tracker-api/domains/errors"
)

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusInternalServerError, "internal_error"

	switch {
	case errors.Is(err, errors_models.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, errors_models.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, errors_models.ErrValidation):
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
