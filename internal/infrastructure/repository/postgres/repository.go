package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"yaConversationWriter/internal/config"
	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
	"yaConversationWriter/migrations"
)

var _ ports.Repository = (*Repository)(nil)

type Repository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func New(cfg config.Postgres, logger *slog.Logger) (*Repository, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	connectionURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))),
		Path:   cfg.Database,
	}
	query := connectionURL.Query()
	query.Set("sslmode", cfg.SSLMode)
	query.Set("connect_timeout", strconv.FormatInt(max(1, int64(cfg.ConnectTimeout/time.Second)), 10))
	connectionURL.RawQuery = query.Encode()

	poolConfig, err := pgxpool.ParseConfig(connectionURL.String())
	if err != nil {
		return nil, errors.New("parse PostgreSQL connection configuration")
	}
	poolConfig.MinConns = cfg.MinConnections
	poolConfig.MaxConns = cfg.MaxConnections
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, errors.New("create PostgreSQL connection pool")
	}
	return &Repository{pool: pool, logger: logger}, nil
}

func (r *Repository) Name() string { return "postgres" }

func (r *Repository) Start(ctx context.Context) error {
	if err := r.pool.Ping(ctx); err != nil {
		r.logger.Error("PostgreSQL connection failed", "error", err)
		r.pool.Close()
		return fmt.Errorf("connect to PostgreSQL: %w", domain.ErrInfrastructure)
	}
	r.logger.Info("PostgreSQL connected")
	return nil
}

func (r *Repository) Shutdown(context.Context) error {
	r.pool.Close()
	r.logger.Info("PostgreSQL connection pool closed")
	return nil
}

func (r *Repository) MigrateUp(ctx context.Context) error {
	return migrations.Up(ctx, r.pool)
}

func (r *Repository) MigrateDown(ctx context.Context) error {
	return migrations.Down(ctx, r.pool)
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *Repository) mapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, domain.ErrNotFound)
	}

	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		r.logger.Error("PostgreSQL operation failed", "operation", operation, "code", postgresError.Code, "constraint", postgresError.ConstraintName)
		switch postgresError.Code {
		case "23505":
			return fmt.Errorf("%s: %w", operation, domain.ErrConflict)
		case "23503":
			return fmt.Errorf("%s: %w", operation, domain.ErrNotFound)
		case "23502", "23514", "22001", "22P02":
			return fmt.Errorf("%s: %w", operation, domain.ErrInvalidArgument)
		}
	} else {
		r.logger.Error("PostgreSQL operation failed", "operation", operation, "error", err)
	}
	return fmt.Errorf("%s: %w", operation, domain.ErrInfrastructure)
}

func rollback(tx pgx.Tx) {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(rollbackCtx)
}
