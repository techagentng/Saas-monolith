package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/techagentng/saas-monolith/internal/config"
)

func DSN(c config.Config) string {
	return (&url.URL{Scheme: "postgres", User: url.UserPassword(c.PostgresUser, c.PostgresPassword), Host: fmt.Sprintf("%s:%d", c.PostgresHost, c.PostgresPort), Path: "/" + c.PostgresDB, RawQuery: url.Values{"sslmode": []string{c.PostgresSSLMode}}.Encode()}).String()
}

func Open(ctx context.Context, c config.Config) (*sql.DB, error) {
	db, err := sql.Open("pgx", DSN(c))
	if err != nil {
		return nil, fmt.Errorf("opening postgres: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	pingContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingContext); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return db, nil
}

func ApplyMigrations(ctx context.Context, db *sql.DB, directory string) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return fmt.Errorf("creating migration table: %w", err)
	}
	files, err := filepath.Glob(filepath.Join(directory, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("listing migrations: %w", err)
	}
	sort.Strings(files)
	for _, file := range files {
		version := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		var applied bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if applied {
			continue
		}
		script, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", version, err)
		}
		transaction, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("starting migration %s: %w", version, err)
		}
		if _, err = transaction.ExecContext(ctx, string(script)); err == nil {
			_, err = transaction.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version)
		}
		if err != nil {
			_ = transaction.Rollback()
			return fmt.Errorf("applying migration %s: %w", version, err)
		}
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", version, err)
		}
	}
	return nil
}
