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
//
// OnboardingStatus is defaulted here the same way Status already is: a
// caller that leaves it unset gets IN_PROGRESS, mirroring the
// empty-Status-becomes-ACTIVE pattern below (TenantService.Create is the
// only real caller, and it always leaves this unset — see its own comment
// for why). BusinessType is passed through as-is; this layer does not
// require it to be non-nil, TenantService.Create does.
func (r *PostgresTenantRepository) Create(ctx context.Context, tenant *model.Tenant) (*model.Tenant, error) {
	status := tenant.Status
	if status == "" {
		status = model.StatusActive
	}
	onboardingStatus := tenant.OnboardingStatus
	if onboardingStatus == "" {
		onboardingStatus = model.OnboardingStatusInProgress
	}
	created := *tenant
	created.Status = status
	created.OnboardingStatus = onboardingStatus
	const query = `INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, onboarding_step) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING status, onboarding_status, created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query, tenant.ID, tenant.Name, tenant.Slug, status, (*string)(tenant.BusinessType), onboardingStatus, tenant.OnboardingStep).
		Scan(&created.Status, &created.OnboardingStatus, &created.CreatedAt, &created.UpdatedAt)
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

// tenantColumns is the full column set every tenant-row SELECT below reads,
// kept as one constant so the three query strings and scanTenant's argument
// order can't silently drift apart from each other.
const tenantColumns = "id, name, slug, status, description, contact_email, contact_phone, timezone, business_type, onboarding_status, onboarding_step, created_at, updated_at"

// scanner is satisfied by both *sql.Row and *sql.Rows, letting FindByID,
// FindBySlug, and each row of ListAccessibleByUserID share one scan routine
// instead of repeating the same column-to-field mapping three times.
type scanner interface {
	Scan(dest ...any) error
}

// scanTenant scans one row matching tenantColumns' order. BusinessType is
// scanned through a local *string and converted afterward — model.BusinessType
// and string share an identical underlying type, so this is a plain pointer
// conversion, not a copy — rather than scanning directly into
// *model.BusinessType at every call site.
func scanTenant(row scanner) (*model.Tenant, error) {
	var tenant model.Tenant
	var businessType *string
	err := row.Scan(
		&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Status,
		&tenant.Description, &tenant.ContactEmail, &tenant.ContactPhone, &tenant.Timezone,
		&businessType, &tenant.OnboardingStatus, &tenant.OnboardingStep,
		&tenant.CreatedAt, &tenant.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	tenant.BusinessType = (*model.BusinessType)(businessType)
	return &tenant, nil
}

func (r *PostgresTenantRepository) FindByID(ctx context.Context, id string) (*model.Tenant, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+tenantColumns+" FROM tenants WHERE id = $1", id)
	tenant, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding tenant: %w", err)
	}
	return tenant, nil
}

// FindBySlug resolves a tenant by its exact canonical slug, relying on the
// unique index created with the slug column. The comparison is exact: no
// lowering, trimming, or pattern matching happens here, so lookup agrees
// byte-for-byte with what creation validated and stored.
//
// Visibility rules (for example hiding DISABLED tenants from public callers)
// belong to the service layer, not here.
func (r *PostgresTenantRepository) FindBySlug(ctx context.Context, slug string) (*model.Tenant, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+tenantColumns+" FROM tenants WHERE slug = $1", slug)
	tenant, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding tenant by slug: %w", err)
	}
	return tenant, nil
}

// ListAccessibleByUserID returns all ACTIVE tenants where the user has an ACTIVE membership.
// Results are ordered by created_at ASC, then id ASC for deterministic results.
// Uses a single JOIN query to avoid N+1 lookups.
func (r *PostgresTenantRepository) ListAccessibleByUserID(ctx context.Context, userID string) ([]*model.Tenant, error) {
	qualified := "t.id, t.name, t.slug, t.status, t.description, t.contact_email, t.contact_phone, t.timezone, t.business_type, t.onboarding_status, t.onboarding_step, t.created_at, t.updated_at"
	rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT `+qualified+`
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
		tenant, err := scanTenant(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning tenant: %w", err)
		}
		tenants = append(tenants, tenant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tenants: %w", err)
	}
	return tenants, nil
}

// UpdateProfile updates tenant profile fields: name, description, contact_email, contact_phone, timezone.
// Only non-nil fields in the update are modified. Omitted fields remain unchanged.
// business_type/onboarding_status/onboarding_step are never part of the SET
// clause — TenantProfileUpdate has no fields for them (Feature 1 decision:
// business_type is immutable through this endpoint) — but they are still
// read back accurately in the RETURNING clause so the response reflects the
// tenant's real, unchanged state rather than going blank.
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
		"UPDATE tenants SET %s WHERE id = $%d RETURNING %s",
		strings.Join(sets, ", "),
		whereIndex,
		tenantColumns,
	)

	row := r.db.QueryRowContext(ctx, query, args...)
	tenant, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("updating tenant: %w", err)
	}

	return tenant, nil
}

// UpdateOnboardingStep persists only onboarding_step (Vertical Onboarding
// F2). Its SET clause has no other column to write, which is what
// structurally guarantees it can never touch onboarding_status,
// business_type, or anything else — the same "absence is the enforcement"
// pattern UpdateProfile already relies on for business_type immutability.
func (r *PostgresTenantRepository) UpdateOnboardingStep(ctx context.Context, tenantID string, step string) (*model.Tenant, error) {
	row := r.db.QueryRowContext(ctx, "UPDATE tenants SET onboarding_step = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 RETURNING "+tenantColumns, step, tenantID)
	tenant, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("updating onboarding step: %w", err)
	}
	return tenant, nil
}

// CompleteOnboarding transitions onboarding_status to COMPLETED
// unconditionally. It has no idea whether completion prerequisites were
// checked or whether the tenant was already COMPLETED — OnboardingService.Complete
// owns both of those decisions and only calls this once they're settled.
func (r *PostgresTenantRepository) CompleteOnboarding(ctx context.Context, tenantID string) (*model.Tenant, error) {
	row := r.db.QueryRowContext(ctx, "UPDATE tenants SET onboarding_status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 RETURNING "+tenantColumns, string(model.OnboardingStatusCompleted), tenantID)
	tenant, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("completing onboarding: %w", err)
	}
	return tenant, nil
}
