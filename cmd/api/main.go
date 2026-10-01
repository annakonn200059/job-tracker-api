package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	applications_api "github.com/annakonn200059/job-tracker-api/api/applications"
	auth_api "github.com/annakonn200059/job-tracker-api/api/auth"
	vacancies_api "github.com/annakonn200059/job-tracker-api/api/vacancies"
	"github.com/annakonn200059/job-tracker-api/config"
	apihttp "github.com/annakonn200059/job-tracker-api/http"
	"github.com/annakonn200059/job-tracker-api/internal/infrastructure/database"
	applications_repo "github.com/annakonn200059/job-tracker-api/repos/applications"
	companies_repo "github.com/annakonn200059/job-tracker-api/repos/companies"
	events_repo "github.com/annakonn200059/job-tracker-api/repos/events"
	sessions_repo "github.com/annakonn200059/job-tracker-api/repos/sessions"
	users_repo "github.com/annakonn200059/job-tracker-api/repos/users"
	vacancies_repo "github.com/annakonn200059/job-tracker-api/repos/vacancies"
	applications_service "github.com/annakonn200059/job-tracker-api/services/applications"
	auth_service "github.com/annakonn200059/job-tracker-api/services/auth"
	vacancies_service "github.com/annakonn200059/job-tracker-api/services/vacancies"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// publicPaths are reachable without a session; every other route requires
// one (see apihttp.RequireAuth). Logout is public so a client with an
// already-expired session can still clear its cookie.
var publicPaths = []string{
	"/healthz",
	"/readyz",
	"/auth/register",
	"/auth/login",
	"/auth/google",
	"/auth/logout",
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

	// Cancelled on SIGINT or SIGTERM. stop() restores default handling, so a
	// second Ctrl-C kills the process outright instead of being swallowed —
	// useful when shutdown itself hangs.
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
	usersRepo := users_repo.NewRepo(pool)
	sessionsRepo := sessions_repo.NewRepo(pool)

	var googleVerifier auth_service.GoogleVerifier
	if cfg.GoogleClientID != "" {
		googleVerifier = auth_service.NewGoogleVerifier(cfg.GoogleClientID)
	} else {
		logger.Warn("GOOGLE_CLIENT_ID not set: Google sign-in disabled")
	}

	appsService := applications_service.NewService(pool, appsRepo, eventsRepo)
	vacanciesService := vacancies_service.NewVacanciesService(pool, vacanciesRepo, companiesRepo)
	authService := auth_service.NewService(pool, usersRepo, sessionsRepo, googleVerifier, cfg.SessionTTL)

	mux := http.NewServeMux()

	auth_api.NewHandler(authService, apihttp.CookieConfig{
		Secure: cfg.CookieSecure,
		Domain: cfg.CookieDomain,
	}).Register(mux)
	applications_api.NewHandler(appsService).Register(mux)
	vacancies_api.NewHandler(vacanciesService).Register(mux)

	readiness := apihttp.NewReadiness()
	mux.HandleFunc("GET /healthz", apihttp.LivenessHandler())
	mux.HandleFunc("GET /readyz", apihttp.ReadinessHandler(readiness, pool, 2*time.Second))

	srv := &http.Server{
		Addr: ":" + cfg.HTTPPort,
		Handler: apihttp.Logging(logger,
			apihttp.CORS(cfg.CORSAllowedOrigins,
				apihttp.Authenticate(authService,
					apihttp.RequireAuth(publicPaths, mux)))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Buffered: if shutdown completes before this goroutine ever sends, an
	// unbuffered channel would leak the goroutine forever.
	serverErr := make(chan error, 1)

	go func() {
		logger.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		// Failed before any signal arrived — a bound port, usually.
		return err

	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	// Step 1. Fail readiness. Kubernetes notices on its next probe and starts
	// removing this pod from Service endpoints.
	readiness.Unready()
	logger.Info("readiness disabled", "wait", cfg.PreShutdownWait.String())

	// Step 2. Wait for that removal to propagate. Endpoint updates are
	// asynchronous: the endpoint controller updates the EndpointSlice, then
	// kube-proxy rewrites iptables on every node. Draining immediately means
	// traffic still arrives at a server that has stopped accepting it, and the
	// client sees a connection error instead of a response.
	//
	// This sleep looks removable. It is not.
	time.Sleep(cfg.PreShutdownWait)

	// Step 3. Drain. context.Background(), NOT ctx — ctx is already cancelled,
	// so Shutdown would return instantly having drained nothing, and the code
	// would still look correct.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownWait)
	defer cancel()

	logger.Info("draining connections", "timeout", cfg.ShutdownWait.String())

	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Requests outlived the grace period. In Kubernetes, SIGKILL follows
		// shortly after terminationGracePeriodSeconds regardless.
		logger.Warn("graceful shutdown timed out, forcing close", "err", err)
		if closeErr := srv.Close(); closeErr != nil {
			logger.Error("force close failed", "err", closeErr)
		}
	}

	// Step 4. Release the pool. After Shutdown, so in-flight requests still
	// had a working database.
	pool.Close()

	logger.Info("stopped cleanly")
	return nil
}

func parseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}
