package vacancies_api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	vacancies_api "github.com/annakonn200059/job-tracker-api/api/vacancies"
	errors_models "github.com/annakonn200059/job-tracker-api/domains/errors"
	vacancies_models "github.com/annakonn200059/job-tracker-api/domains/vacancies"
	"github.com/annakonn200059/job-tracker-api/internal/contracttest"
	vacancies_service "github.com/annakonn200059/job-tracker-api/services/vacancies"
)

// fakeService returns vacancy (or err) from every method and records what
// the handler passed in, so tests can check request decoding too.
type fakeService struct {
	vacancy *vacancies_models.Vacancy
	err     error

	gotCreate *vacancies_service.CreateParams
	gotGet    struct{ userID, id int64 }
}

func (f *fakeService) List(context.Context, vacancies_models.Filter) ([]vacancies_models.Vacancy, int64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	return []vacancies_models.Vacancy{*f.vacancy}, 1, nil
}

func (f *fakeService) Get(_ context.Context, userID, id int64) (*vacancies_models.Vacancy, error) {
	f.gotGet.userID, f.gotGet.id = userID, id
	return f.vacancy, f.err
}

func (f *fakeService) Create(_ context.Context, p vacancies_service.CreateParams) (*vacancies_models.Vacancy, error) {
	f.gotCreate = &p
	return f.vacancy, f.err
}

// Update runs apply on a copy of vacancy, as the real service does on the
// locked row.
func (f *fakeService) Update(_ context.Context, _, _ int64, apply func(*vacancies_models.Vacancy)) (*vacancies_models.Vacancy, error) {
	if f.err != nil {
		return nil, f.err
	}
	v := *f.vacancy
	apply(&v)
	return &v, nil
}

func (f *fakeService) Delete(context.Context, int64, int64) error  { return f.err }
func (f *fakeService) Restore(context.Context, int64, int64) error { return f.err }

func ptr[T any](v T) *T { return &v }

// fullVacancy sets every field. Optional response fields are omitempty in
// Go, so a field left nil here would never reach the validator and a rename
// of it would go unnoticed.
func fullVacancy() *vacancies_models.Vacancy {
	ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return &vacancies_models.Vacancy{
		ID:             7,
		UserID:         contracttest.UserID,
		CompanyID:      ptr(int64(3)),
		Title:          "Backend Engineer",
		URL:            ptr("https://example.com/jobs/1"),
		Description:    ptr("Go and Postgres"),
		Location:       ptr("Berlin"),
		WorkMode:       ptr(vacancies_models.WorkHybrid),
		EmploymentType: ptr(vacancies_models.EmploymentFullTime),
		Language:       ptr("en"),
		SalaryMin:      ptr(int32(70000)),
		SalaryMax:      ptr(int32(90000)),
		SalaryCurrency: ptr("EUR"),
		SalaryPeriod:   ptr(vacancies_models.SalaryYear),
		Source:         ptr("linkedin"),
		PostedAt:       ptr(time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)),
		CreatedAt:      ts,
		UpdatedAt:      ts,
	}
}

func newServer(svc *fakeService) http.Handler {
	mux := http.NewServeMux()
	vacancies_api.NewHandler(svc).Register(mux)
	return contracttest.Authenticated(mux)
}

func request(method, path, body string, authed bool) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if authed {
		req.Header.Set("Authorization", "Bearer "+contracttest.Token)
	}
	return req
}

func TestGetVacancy(t *testing.T) {
	tests := []struct {
		name   string
		authed bool
		err    error
		want   int
	}{
		{name: "found", authed: true, want: http.StatusOK},
		{name: "not found", authed: true, err: errors_models.ErrNotFound, want: http.StatusNotFound},
		{name: "no session", authed: false, want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{vacancy: fullVacancy(), err: tt.err}

			rec := contracttest.Do(t, newServer(svc), request(http.MethodGet, "/vacancies/7", "", tt.authed))

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tt.want, rec.Body)
			}
			if tt.authed && (svc.gotGet.userID != contracttest.UserID || svc.gotGet.id != 7) {
				t.Errorf("service got user %d, id %d; want user %d, id 7",
					svc.gotGet.userID, svc.gotGet.id, contracttest.UserID)
			}
		})
	}
}

func TestCreateVacancy(t *testing.T) {
	// Every request field the spec allows, so a renamed Go JSON tag makes
	// DecodeJSON reject the body as an unknown field.
	const body = `{
		"company_name": "Acme",
		"title": "Backend Engineer",
		"url": "https://example.com/jobs/1",
		"description": "Go and Postgres",
		"location": "Berlin",
		"work_mode": "hybrid",
		"employment_type": "full_time",
		"language": "en",
		"salary_min": 70000,
		"salary_max": 90000,
		"salary_currency": "EUR",
		"salary_period": "year",
		"source": "linkedin",
		"posted_at": "2026-08-30"
	}`

	t.Run("created", func(t *testing.T) {
		svc := &fakeService{vacancy: fullVacancy()}

		rec := contracttest.Do(t, newServer(svc), request(http.MethodPost, "/vacancies", body, true))

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body)
		}
		got := svc.gotCreate
		if got == nil {
			t.Fatal("service Create was not called")
		}
		if got.CompanyName == nil || *got.CompanyName != "Acme" {
			t.Errorf("CompanyName = %v, want Acme", got.CompanyName)
		}
		if got.Vacancy.UserID != contracttest.UserID || got.Vacancy.Title != "Backend Engineer" {
			t.Errorf("UserID, Title = %d, %q", got.Vacancy.UserID, got.Vacancy.Title)
		}
		if got.Vacancy.PostedAt == nil || got.Vacancy.PostedAt.Format("2006-01-02") != "2026-08-30" {
			t.Errorf("PostedAt = %v, want 2026-08-30", got.Vacancy.PostedAt)
		}
	})

	t.Run("validation error", func(t *testing.T) {
		svc := &fakeService{err: vacancies_models.ErrInvalidSalaryRange}

		rec := contracttest.Do(t, newServer(svc), request(http.MethodPost, "/vacancies", body, true))

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body)
		}
	})

	t.Run("no session", func(t *testing.T) {
		svc := &fakeService{vacancy: fullVacancy()}

		rec := contracttest.Do(t, newServer(svc), request(http.MethodPost, "/vacancies", body, false))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body)
		}
		if svc.gotCreate != nil {
			t.Error("service Create was called without a session")
		}
	})
}

func TestUpdateVacancy(t *testing.T) {
	t.Run("merge patch", func(t *testing.T) {
		svc := &fakeService{vacancy: fullVacancy()}
		// title set, location cleared, everything else left out.
		body := `{"title": "Senior Backend Engineer", "location": null}`

		rec := contracttest.Do(t, newServer(svc), request(http.MethodPatch, "/vacancies/7", body, true))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body)
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["title"] != "Senior Backend Engineer" {
			t.Errorf("title = %v, want the new value", got["title"])
		}
		if _, ok := got["location"]; ok {
			t.Errorf("location = %v, want it cleared", got["location"])
		}
		if got["description"] != "Go and Postgres" || got["salary_period"] != "year" {
			t.Errorf("fields left out of the patch changed: description %v, salary_period %v",
				got["description"], got["salary_period"])
		}
	})

	t.Run("null posted_at clears it", func(t *testing.T) {
		svc := &fakeService{vacancy: fullVacancy()}

		rec := contracttest.Do(t, newServer(svc), request(http.MethodPatch, "/vacancies/7", `{"posted_at": null}`, true))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "posted_at") {
			t.Errorf("posted_at still set: %s", rec.Body)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc := &fakeService{err: errors_models.ErrNotFound}

		rec := contracttest.Do(t, newServer(svc), request(http.MethodPatch, "/vacancies/7", `{"title": "x"}`, true))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body)
		}
	})
}
