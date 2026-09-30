package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL     string
	HTTPPort        string
	LogLevel        string
	ShutdownWait    time.Duration
	PreShutdownWait time.Duration

	DBMaxConns        int32
	DBMinConns        int32
	DBMaxConnLifetime time.Duration
	DBMaxConnIdleTime time.Duration
	DBConnectTimeout  time.Duration

	SessionTTL         time.Duration
	CookieSecure       bool
	CookieDomain       string
	CORSAllowedOrigins []string
	// GoogleClientID enables POST /auth/google when set. It is the OAuth
	// client ID the frontend's Google sign-in button uses; ID tokens issued
	// for any other client are rejected.
	GoogleClientID string
}

func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		HTTPPort:        envOr("HTTP_PORT", "8080"),
		LogLevel:        envOr("LOG_LEVEL", "info"),
		ShutdownWait:    envDuration("SHUTDOWN_WAIT", 15*time.Second),
		PreShutdownWait: envDuration("PRE_SHUTDOWN_WAIT", 5*time.Second),

		DBMaxConns:        envInt32("DB_MAX_CONNS", 10),
		DBMinConns:        envInt32("DB_MIN_CONNS", 0),
		DBMaxConnLifetime: envDuration("DB_MAX_CONN_LIFETIME", time.Hour),
		DBMaxConnIdleTime: envDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
		DBConnectTimeout:  envDuration("DB_CONNECT_TIMEOUT", 5*time.Second),

		SessionTTL:         envDuration("SESSION_TTL", 30*24*time.Hour),
		CookieSecure:       envBool("COOKIE_SECURE", true),
		CookieDomain:       os.Getenv("COOKIE_DOMAIN"),
		CORSAllowedOrigins: envList("CORS_ALLOWED_ORIGINS"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	}

	return c, c.validate()
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.DBMaxConns < 1 {
		return fmt.Errorf("DB_MAX_CONNS must be at least 1, got %d", c.DBMaxConns)
	}
	if c.DBMinConns > c.DBMaxConns {
		return fmt.Errorf("DB_MIN_CONNS (%d) exceeds DB_MAX_CONNS (%d)",
			c.DBMinConns, c.DBMaxConns)
	}
	if _, err := strconv.Atoi(c.HTTPPort); err != nil {
		return fmt.Errorf("HTTP_PORT must be numeric, got %q", c.HTTPPort)
	}
	if c.SessionTTL <= 0 {
		return fmt.Errorf("SESSION_TTL must be positive, got %s", c.SessionTTL)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt32(key string, def int32) int32 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return def
	}
	return int32(n)
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// envList parses a comma-separated list, dropping empty entries.
func envList(key string) []string {
	var out []string
	for _, s := range strings.Split(os.Getenv(key), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
