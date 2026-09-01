package repository

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
)

const (
	identityTestUserID      = "550e8400-e29b-41d4-a716-446655442000"
	identityTestOtherUserID = "550e8400-e29b-41d4-a716-446655442001"
	identityTestSubject     = "112233445566778899000"
)

// openIdentityTestDatabase applies migrations 000001 and 000014 to the
// disposable test database. It mirrors the schema of the .sql files exactly —
// the point of this suite is to prove the constraints declared there actually
// hold, so a divergence here would make the whole suite meaningless.
func openIdentityTestDatabase(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	assertDisposableIdentityDatabase(t, databaseURL)

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("database is unavailable: %v", err)
	}
	for _, statement := range []string{
		// CASCADE because the disposable database may hold a fully migrated
		// schema (sessions, tenant_memberships, ...) whose foreign keys would
		// otherwise block the drop. Safe only because
		// assertDisposableIdentityDatabase has already refused to run here
		// against the development database.
		"DROP TABLE IF EXISTS user_identities CASCADE",
		"DROP TABLE IF EXISTS users CASCADE",
		`CREATE TABLE users (
            id UUID PRIMARY KEY,
            email VARCHAR(320) NOT NULL,
            password_hash TEXT NOT NULL,
            status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
            created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT users_email_unique UNIQUE (email),
            CONSTRAINT users_status_valid CHECK (status IN ('ACTIVE', 'DISABLED'))
        )`,
		`CREATE TABLE user_identities (
            id UUID PRIMARY KEY,
            user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
            provider VARCHAR(32) NOT NULL,
            provider_subject VARCHAR(255) NOT NULL,
            provider_email VARCHAR(320) NULL,
            created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
            CONSTRAINT user_identities_provider_subject_unique UNIQUE (provider, provider_subject),
            CONSTRAINT user_identities_user_provider_unique UNIQUE (user_id, provider),
            CONSTRAINT user_identities_provider_valid CHECK (provider IN ('GOOGLE'))
        )`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("preparing identity schema: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS user_identities CASCADE")
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS users CASCADE")
		db.Close()
	})
	return db, ctx
}

// assertDisposableIdentityDatabase fails loudly rather than skipping: silently
// skipping would let a misconfigured run report "no failures" while verifying
// nothing, and these tests drop the users table.
func assertDisposableIdentityDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parsing database URL: %v", err)
	}
	if name := strings.TrimPrefix(parsed.Path, "/"); name == "booking" {
		t.Fatalf("refusing to run destructive tests against the development database %q — point TEST_DATABASE_URL at the disposable Docker database (docker-compose.test.yml)", name)
	}
}

func seedIdentityTestUser(t *testing.T, db *sql.DB, ctx context.Context, id, email string) {
	t.Helper()
	// A federated account has no password. Persisting an empty hash is the
	// behavior the service relies on, so it is exercised here rather than
	// assumed.
	if _, err := NewPostgresUserRepository(db).Create(ctx, model.User{ID: id, Email: email, PasswordHash: "", Status: model.StatusActive}); err != nil {
		t.Fatalf("seeding user: %v", err)
	}
}

func TestPostgresIdentityRepositoryPersistsAndResolvesByProviderSubject(t *testing.T) {
	db, ctx := openIdentityTestDatabase(t)
	seedIdentityTestUser(t, db, ctx, identityTestUserID, "person@example.com")
	identities := NewPostgresIdentityRepository(db)

	created, err := identities.Create(ctx, model.UserIdentity{
		ID:              "550e8400-e29b-41d4-a716-446655442100",
		UserID:          identityTestUserID,
		Provider:        model.ProviderGoogle,
		ProviderSubject: identityTestSubject,
		ProviderEmail:   "person@example.com",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("created identity = %#v, want database timestamps", created)
	}

	found, err := identities.FindByProviderSubject(ctx, model.ProviderGoogle, identityTestSubject)
	if err != nil {
		t.Fatalf("FindByProviderSubject() error = %v", err)
	}
	if found.UserID != identityTestUserID || found.ProviderEmail != "person@example.com" {
		t.Fatalf("found identity = %#v", found)
	}
}

func TestPostgresIdentityRepositoryReportsUnknownSubjectAsNotFound(t *testing.T) {
	db, ctx := openIdentityTestDatabase(t)

	_, err := NewPostgresIdentityRepository(db).FindByProviderSubject(ctx, model.ProviderGoogle, "never-seen")

	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeResourceNotFound {
		t.Fatalf("error = %v, want RESOURCE_NOT_FOUND", err)
	}
}

// This uniqueness is what makes repeated Google sign-ins idempotent even under
// a race between two concurrent callbacks: the second insert cannot succeed,
// so a duplicate user can never be created behind it.
func TestPostgresIdentityRepositoryRefusesADuplicateProviderSubject(t *testing.T) {
	db, ctx := openIdentityTestDatabase(t)
	seedIdentityTestUser(t, db, ctx, identityTestUserID, "person@example.com")
	seedIdentityTestUser(t, db, ctx, identityTestOtherUserID, "other@example.com")
	identities := NewPostgresIdentityRepository(db)

	if _, err := identities.Create(ctx, model.UserIdentity{ID: "550e8400-e29b-41d4-a716-446655442100", UserID: identityTestUserID, Provider: model.ProviderGoogle, ProviderSubject: identityTestSubject}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := identities.Create(ctx, model.UserIdentity{ID: "550e8400-e29b-41d4-a716-446655442101", UserID: identityTestOtherUserID, Provider: model.ProviderGoogle, ProviderSubject: identityTestSubject})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeExternalIdentityConflict {
		t.Fatalf("error = %v, want EXTERNAL_IDENTITY_CONFLICT", err)
	}
}

func TestPostgresIdentityRepositoryRefusesAnIdentityForAnUnknownUser(t *testing.T) {
	db, ctx := openIdentityTestDatabase(t)

	_, err := NewPostgresIdentityRepository(db).Create(ctx, model.UserIdentity{
		ID:              "550e8400-e29b-41d4-a716-446655442100",
		UserID:          "550e8400-e29b-41d4-a716-4466554429ff",
		Provider:        model.ProviderGoogle,
		ProviderSubject: identityTestSubject,
	})
	if err == nil {
		t.Fatal("Create() attached an identity to a user that does not exist")
	}
}

func TestPostgresIdentityRepositoryUpdatesOnlyTheProviderEmail(t *testing.T) {
	db, ctx := openIdentityTestDatabase(t)
	seedIdentityTestUser(t, db, ctx, identityTestUserID, "person@example.com")
	identities := NewPostgresIdentityRepository(db)
	created, err := identities.Create(ctx, model.UserIdentity{ID: "550e8400-e29b-41d4-a716-446655442100", UserID: identityTestUserID, Provider: model.ProviderGoogle, ProviderSubject: identityTestSubject, ProviderEmail: "old@example.com"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := identities.UpdateProviderEmail(ctx, created.ID, "new@example.com"); err != nil {
		t.Fatalf("UpdateProviderEmail() error = %v", err)
	}

	found, err := identities.FindByProviderSubject(ctx, model.ProviderGoogle, identityTestSubject)
	if err != nil {
		t.Fatalf("FindByProviderSubject() error = %v", err)
	}
	if found.ProviderEmail != "new@example.com" {
		t.Fatalf("provider email = %q", found.ProviderEmail)
	}
	// The account's own address is the user's, not Google's.
	user, err := NewPostgresUserRepository(db).FindByID(ctx, identityTestUserID)
	if err != nil || user.Email != "person@example.com" {
		t.Fatalf("user = %#v, %v, want the account email unchanged", user, err)
	}
}

// The whole reason the service owns a transaction: a user row with no identity
// is an orphan nobody could ever sign into again.
func TestIdentityTransactorRollsBackTheUserWhenTheIdentityInsertFails(t *testing.T) {
	db, ctx := openIdentityTestDatabase(t)
	transactor := NewPostgresIdentityTransactor(db)

	err := transactor.WithinTransaction(ctx, func(users UserRepository, identities IdentityRepository) error {
		if _, err := users.Create(ctx, model.User{ID: identityTestUserID, Email: "person@example.com", PasswordHash: "", Status: model.StatusActive}); err != nil {
			return err
		}
		// A subject longer than the column allows: a genuine database failure
		// rather than a simulated one.
		_, err := identities.Create(ctx, model.UserIdentity{
			ID:              "550e8400-e29b-41d4-a716-446655442100",
			UserID:          identityTestUserID,
			Provider:        "NOT_A_PROVIDER",
			ProviderSubject: identityTestSubject,
		})
		return err
	})
	if err == nil {
		t.Fatal("WithinTransaction() committed work that failed")
	}

	_, findErr := NewPostgresUserRepository(db).FindByID(ctx, identityTestUserID)
	var appErr *apperrors.AppError
	if !errors.As(findErr, &appErr) || appErr.Code != apperrors.CodeUserNotFound {
		t.Fatalf("user lookup after rollback = %v, want USER_NOT_FOUND", findErr)
	}
}

func TestIdentityTransactorCommitsBothRowsOnSuccess(t *testing.T) {
	db, ctx := openIdentityTestDatabase(t)

	err := NewPostgresIdentityTransactor(db).WithinTransaction(ctx, func(users UserRepository, identities IdentityRepository) error {
		if _, err := users.Create(ctx, model.User{ID: identityTestUserID, Email: "person@example.com", PasswordHash: "", Status: model.StatusActive}); err != nil {
			return err
		}
		_, err := identities.Create(ctx, model.UserIdentity{
			ID:              "550e8400-e29b-41d4-a716-446655442100",
			UserID:          identityTestUserID,
			Provider:        model.ProviderGoogle,
			ProviderSubject: identityTestSubject,
			ProviderEmail:   "person@example.com",
		})
		return err
	})
	if err != nil {
		t.Fatalf("WithinTransaction() error = %v", err)
	}

	if _, err := NewPostgresUserRepository(db).FindByID(ctx, identityTestUserID); err != nil {
		t.Fatalf("user was not committed: %v", err)
	}
	if _, err := NewPostgresIdentityRepository(db).FindByProviderSubject(ctx, model.ProviderGoogle, identityTestSubject); err != nil {
		t.Fatalf("identity was not committed: %v", err)
	}
}
