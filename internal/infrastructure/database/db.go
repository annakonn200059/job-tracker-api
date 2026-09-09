// Package database owns the PostgreSQL connection pool and the abstraction
// that lets repositories run either standalone or inside a transaction.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is the subset of pgx used by repositories. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so a repository does not need to know whether it is
// running on its own or inside a transaction opened by a service.
type DBTX interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Compile-time proof that both implementations still match the interface.
// If a pgx upgrade changes a signature, it fails here rather than in every
// repository package.
var (
	_ DBTX = (*pgxpool.Pool)(nil)
	_ DBTX = (pgx.Tx)(nil)
)

// PoolConfig carries the tunables. They come from the environment via
// config.Config so they can be adjusted per environment without a rebuild.
type PoolConfig struct {
	DSN               string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
}

func (c *PoolConfig) applyDefaults() {
	if c.MaxConns <= 0 {
		c.MaxConns = 10
	}
	if c.MinConns < 0 {
		c.MinConns = 0
	}
	if c.MaxConnLifetime == 0 {
		c.MaxConnLifetime = time.Hour
	}
	if c.MaxConnIdleTime == 0 {
		c.MaxConnIdleTime = 30 * time.Minute
	}
	if c.HealthCheckPeriod == 0 {
		c.HealthCheckPeriod = time.Minute
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
}

// NewPool opens the pool and verifies it can reach the database.
// The caller owns the returned pool and must Close it.
func NewPool(ctx context.Context, pc PoolConfig) (*pgxpool.Pool, error) {
	pc.applyDefaults()

	cfg, err := pgxpool.ParseConfig(pc.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database dsn: %w", err)
	}

	cfg.MaxConns = pc.MaxConns
	cfg.MinConns = pc.MinConns
	cfg.MaxConnLifetime = pc.MaxConnLifetime
	cfg.MaxConnIdleTime = pc.MaxConnIdleTime
	cfg.HealthCheckPeriod = pc.HealthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = pc.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	// NewWithConfig is lazy, so without this the first real failure would
	// surface as a request error instead of a startup error.
	pingCtx, cancel := context.WithTimeout(ctx, pc.ConnectTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// InTx runs fn inside a transaction, committing on success and rolling back
// on error or panic. Services use it instead of handling Begin/Commit by hand.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
