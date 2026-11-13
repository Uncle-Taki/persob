package postgres

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/001_initial_schema.sql
var initialSchemaSQL string

type DB struct {
	Pool *pgxpool.Pool
}

func Connect(ctx context.Context, dsn string) (*DB, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pgxpool: %w", err)
	}

	// Verify connection
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	slog.Info("connected to postgres database successfully")
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

// Migrate runs embedded SQL migrations safely
func (db *DB) Migrate(ctx context.Context) error {
	slog.Info("running postgres migrations...")
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, initialSchemaSQL); err != nil {
		return fmt.Errorf("execute initial schema migration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration tx: %w", err)
	}

	slog.Info("postgres migrations completed successfully")
	return nil
}

func (db *DB) Ping(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}
