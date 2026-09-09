package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	applications_api "github.com/annakonn200059/job-tracker-api/api/applications"
	vacancies_api "github.com/annakonn200059/job-tracker-api/api/vacancies"
	"github.com/annakonn200059/job-tracker-api/config"
	apihttp "github.com/annakonn200059/job-tracker-api/http"
	"github.com/annakonn200059/job-tracker-api/internal/infrastructure/database"
	applications_repo "github.com/annakonn200059/job-tracker-api/repos/applications"
	companies_repo "github.com/annakonn200059/job-tracker-api/repos/companies"
	events_repo "github.com/annakonn200059/job-tracker-api/repos/events"
	vacancies_repo "github.com/annakonn200059/job-tracker-api/repos/vacancies"
	applications_service "github.com/annakonn200059/job-tracker-api/services/applications"
	vacancies_service "github.com/annakonn200059/job-tracker-api/services/vacancies"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	ctx := context.Background()

	pool, err := database.NewPool(ctx, database.PoolConfig{
		DSN:             cfg.DatabaseURL,
		MaxConns:        cfg.DBMaxConns,
		MinConns:        cfg.DBMinConns,
		MaxConnLifetime: cfg.DBMaxConnLifetime,
		MaxConnIdleTime: cfg.DBMaxConnIdleTime,
		ConnectTimeout:  cfg.DBConnectTimeout,
	})
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("database connected", "max_conns", cfg.DBMaxConns)

	appsRepo := applications_repo.NewRepo(pool)
	eventsRepo := events_repo.NewRepo(pool)
	vacanciesRepo := vacancies_repo.NewRepo(pool)
	companiesRepo := companies_repo.NewRepo(pool)

	appsService := applications_service.NewService(pool, appsRepo, eventsRepo)
	vacanciesService := vacancies_service.NewVacanciesService(pool, vacanciesRepo, companiesRepo)

	mux := http.NewServeMux()

	applications_api.NewHandler(appsService).Register(mux)
	vacancies_api.NewHandler(vacanciesService).Register(mux)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			logger.Warn("readiness failed", "err", err)
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           apihttp.Logging(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("starting", "addr", srv.Addr, "log_level", cfg.LogLevel)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func parseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}
