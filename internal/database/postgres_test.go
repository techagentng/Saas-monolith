package database

import (
	"testing"

	"github.com/techagentng/saas-monolith/internal/config"
)

func TestDSNEscapesCredentialsAndUsesConfiguredSSLMode(t *testing.T) {
	dsn := DSN(config.Config{PostgresHost: "localhost", PostgresPort: 5432, PostgresUser: "user@example", PostgresPassword: "p@ss word", PostgresDB: "booking", PostgresSSLMode: "require"})
	if dsn != "postgres://user%40example:p%40ss%20word@localhost:5432/booking?sslmode=require" {
		t.Fatalf("DSN() = %q", dsn)
	}
}
