package applications_api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	applications_api "github.com/annakonn200059/job-tracker-api/api/applications"
	applications_models "github.com/annakonn200059/job-tracker-api/domains/applications"
	errors_models "github.com/annakonn200059/job-tracker-api/domains/errors"
	"github.com/annakonn200059/job-tracker-api/internal/contracttest"
	applications_service "github.com/annakonn200059/job-tracker-api/services/applications"
)

// fakeService implements only what these tests reach; the rest panic so a
// test that wanders into them fails loudly.
type fakeService struct {
	applications_api.Service

	app *applications_models.Application
	err error
}

// Update runs apply on a copy of app, as the real service does on the
// locked row.
func (f *fakeService) Update(_ context.Context, _, _ int64, apply func(*applications_models.Application)) (*applications_models.Application, error) {
	if f.err != nil {
		return nil, f.err
	}
	a := *f.app
	apply(&a)
	return &a, nil
}

func (f *fakeService) Board(context.Context, int64) (applications_service.Board, error) {
	return applications_service.Board{f.app.Stage: {*f.app}}, f.err
}

// fullApplication sets every field. Optional response fields are omitempty
// in Go, so a field left nil here would never reach the validator.
func fullApplication() *applications_models.Application {
	ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return &applications_models.Application{
		ID:         5,
		UserID:     contracttest.UserID,
		VacancyID:  7,
		Stage:      applications_models.StageOffer,
		BoardOrder: 1000,
		Priority:   2,
		AppliedAt:  new(ts),
		ClosedAt:   new(ts.Add(48 * time.Hour)),
		Notes:      new("recruiter call on Monday"),
		CreatedAt:  ts,
		UpdatedAt:  ts,
	}
}

func newServer(svc *fakeService) http.Handler {
	mux := http.NewServeMux()
	applications_api.NewHandler(svc).Register(mux)
	return contracttest.Authenticated(mux)
}

func patch(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+contracttest.Token)
	return req
}

func TestUpdateApplication(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantPriority float64
		wantNotes    any // nil = absent from the response
	}{
		{name: "priority only keeps notes", body: `{"priority": 3}`, wantPriority: 3, wantNotes: "recruiter call on Monday"},
		{name: "notes only keeps priority", body: `{"notes": "offer in writing"}`, wantPriority: 2, wantNotes: "offer in writing"},
		{name: "null clears notes", body: `{"notes": null}`, wantPriority: 2, wantNotes: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{app: fullApplication()}

			rec := contracttest.Do(t, newServer(svc), patch("/applications/5", tt.body))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body)
			}
			var got map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["priority"] != tt.wantPriority {
				t.Errorf("priority = %v, want %v", got["priority"], tt.wantPriority)
			}
			if got["notes"] != tt.wantNotes {
				t.Errorf("notes = %v, want %v", got["notes"], tt.wantNotes)
			}
		})
	}

	t.Run("not found", func(t *testing.T) {
		svc := &fakeService{err: errors_models.ErrNotFound}

		rec := contracttest.Do(t, newServer(svc), patch("/applications/5", `{"priority": 1}`))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body)
		}
	})
}

func TestBoard(t *testing.T) {
	svc := &fakeService{app: fullApplication()}
	req := httptest.NewRequest(http.MethodGet, "/applications/board", nil)
	req.Header.Set("Authorization", "Bearer "+contracttest.Token)

	rec := contracttest.Do(t, newServer(svc), req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body)
	}
}
