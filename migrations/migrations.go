package migrations

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const Version = 1

//go:embed *.sql
var files embed.FS

type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

func Up(ctx context.Context, database Beginner) error {
	return apply(ctx, database, true)
}

func Down(ctx context.Context, database Beginner) error {
	return apply(ctx, database, false)
}

func apply(ctx context.Context, database Beginner, up bool) error {
	tx, err := database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(71520260801)"); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version BIGINT PRIMARY KEY,
            applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
        )
	`); err != nil {
		return fmt.Errorf("create migration tracking table: %w", err)
	}

	var current int
	err = tx.QueryRow(ctx, "SELECT version FROM schema_migrations WHERE version = $1", Version).Scan(&current)
	switch {
	case err == nil && up:
		return tx.Commit(ctx)
	case errors.Is(err, pgx.ErrNoRows) && !up:
		return tx.Commit(ctx)
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read migration version: %w", err)
	}

	fileName := "000001_initial.up.sql"
	if !up {
		fileName = "000001_initial.down.sql"
	}
	statement, err := files.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("read embedded migration %q: %w", fileName, err)
	}
	if _, err := tx.Exec(ctx, string(statement)); err != nil {
		return fmt.Errorf("execute migration %q: %w", fileName, err)
	}
	if up {
		_, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES ($1)", Version)
	} else {
		_, err = tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", Version)
	}
	if err != nil {
		return fmt.Errorf("update migration version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}
