package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

// dbtx is satisfied by both *sql.DB and *sql.Tx, letting this repository run
// standalone or participate in a caller-owned transaction (Feature 2).
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type PostgresTenantRepository struct{ db dbtx }

func NewPostgresTenantRepository(db dbtx) *PostgresTenantRepository {
	return &PostgresTenantRepository{db: db}
}

// Create persists a new tenant. A duplicate slug is a normal, presentable
// business outcome (Feature 2 is its first real caller) and maps to
// TENANT_SLUG_TAKEN. A duplicate ID (PK collision) remains an unexpected
// system failure per the Feature 1 decision — any other unique constraint
// added later falls back to the same system-failure wrapping rather than
// being guessed at.
func (r *PostgresTenantRepository) Create(ctx context.Context, tenant *model.Tenant) (*model.Tenant, error) {
	status := tenant.Status
	if status == "" {
		status = model.StatusActive
	}
	created := *tenant
	created.Status = status
	const query = `INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, $4) RETURNING status, created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query, tenant.ID, tenant.Name, tenant.Slug, status).Scan(&created.Status, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		if isSlugUniqueViolation(err) {
			return nil, apperrors.New(apperrors.CodeTenantSlugTaken, "tenant slug is already taken", err)
		}
		return nil, fmt.Errorf("inserting tenant: %w", err)
	}
	return &created, nil
}

func isSlugUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "tenants_slug_key"
}

func (r *PostgresTenantRepository) FindByID(ctx context.Context, id string) (*model.Tenant, error) {
	var tenant model.Tenant
	err := r.db.QueryRowContext(ctx, `SELECT id, name, slug, status, description, contact_email, contact_phone, timezone, created_at, updated_at FROM tenants WHERE id = $1`, id).
		Scan(&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Status, &tenant.Description, &tenant.ContactEmail, &tenant.ContactPhone, &tenant.Timezone, &tenant.CreatedAt, &tenant.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding tenant: %w", err)
	}
	return &tenant, nil
}

// FindBySlug resolves a tenant by its exact canonical slug, relying on the
// unique index created with the slug column. The comparison is exact: no
// lowering, trimming, or pattern matching happens here, so lookup agrees
// byte-for-byte with what creation validated and stored.
//
// Visibility rules (for example hiding DISABLED tenants from public callers)
// belong to the service layer, not here.
func (r *PostgresTenantRepository) FindBySlug(ctx context.Context, slug string) (*model.Tenant, error) {
	var tenant model.Tenant
	err := r.db.QueryRowContext(ctx, `SELECT id, name, slug, status, description, contact_email, contact_phone, timezone, created_at, updated_at FROM tenants WHERE slug = $1`, slug).
		Scan(&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Status, &tenant.Description, &tenant.ContactEmail, &tenant.ContactPhone, &tenant.Timezone, &tenant.CreatedAt, &tenant.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding tenant by slug: %w", err)
	}
	return &tenant, nil
}

// ListAccessibleByUserID returns all ACTIVE tenants where the user has an ACTIVE membership.
// Results are ordered by created_at ASC, then id ASC for deterministic results.
// Uses a single JOIN query to avoid N+1 lookups.
func (r *PostgresTenantRepository) ListAccessibleByUserID(ctx context.Context, userID string) ([]*model.Tenant, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT t.id, t.name, t.slug, t.status, t.description, t.contact_email, t.contact_phone, t.timezone, t.created_at, t.updated_at
FROM tenants t
INNER JOIN tenant_memberships tm ON t.id = tm.tenant_id
WHERE tm.user_id = $1
  AND tm.status = 'ACTIVE'
  AND t.status = 'ACTIVE'
ORDER BY t.created_at ASC, t.id ASC
`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing accessible tenants: %w", err)
	}
	defer rows.Close()
	var tenants []*model.Tenant
	for rows.Next() {
		var tenant model.Tenant
		if err := rows.Scan(&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Status, &tenant.Description, &tenant.ContactEmail, &tenant.ContactPhone, &tenant.Timezone, &tenant.CreatedAt, &tenant.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning tenant: %w", err)
		}
		tenants = append(tenants, &tenant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tenants: %w", err)
	}
	return tenants, nil
}

// UpdateProfile updates tenant profile fields: name, description, contact_email, contact_phone, timezone.
// Only non-nil fields in the update are modified. Omitted fields remain unchanged.
// Returns the full updated tenant or an error.
func (r *PostgresTenantRepository) UpdateProfile(ctx context.Context, tenantID string, update TenantProfileUpdate) (*model.Tenant, error) {
	// Build dynamic SET clause from non-nil fields
	sets := []string{}
	args := []interface{}{}
	argIndex := 1

	if update.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *update.Name)
		argIndex++
	}
	if update.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", argIndex))
		args = append(args, *update.Description)
		argIndex++
	}
	if update.ContactEmail != nil {
		sets = append(sets, fmt.Sprintf("contact_email = $%d", argIndex))
		args = append(args, *update.ContactEmail)
		argIndex++
	}
	if update.ContactPhone != nil {
		sets = append(sets, fmt.Sprintf("contact_phone = $%d", argIndex))
		args = append(args, *update.ContactPhone)
		argIndex++
	}
	if update.Timezone != nil {
		sets = append(sets, fmt.Sprintf("timezone = $%d", argIndex))
		args = append(args, *update.Timezone)
		argIndex++
	}

	// Add WHERE clause
	sets = append(sets, fmt.Sprintf("updated_at = CURRENT_TIMESTAMP"))
	args = append(args, tenantID)
	whereIndex := argIndex

	query := fmt.Sprintf(
		"UPDATE tenants SET %s WHERE id = $%d RETURNING id, name, slug, status, description, contact_email, contact_phone, timezone, created_at, updated_at",
		strings.Join(sets, ", "),
		whereIndex,
	)

	var tenant model.Tenant
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Status,
		&tenant.Description, &tenant.ContactEmail, &tenant.ContactPhone, &tenant.Timezone,
		&tenant.CreatedAt, &tenant.UpdatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("updating tenant: %w", err)
	}

	return &tenant, nil
}
