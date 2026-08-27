package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

type PostgresStaffRepository struct{ db dbtx }

func NewPostgresStaffRepository(db dbtx) *PostgresStaffRepository {
	return &PostgresStaffRepository{db: db}
}

// staffColumns is the full column set every SELECT below reads, kept as one
// constant so the query strings and scanStaff's argument order cannot silently
// drift apart.
const staffColumns = "id, tenant_id, user_id, display_name, bio, is_bookable, status, created_at, updated_at"

func scanStaff(row scanner) (*model.StaffProfile, error) {
	var profile model.StaffProfile
	err := row.Scan(
		&profile.ID, &profile.TenantID, &profile.UserID, &profile.DisplayName,
		&profile.Bio, &profile.IsBookable, &profile.Status,
		&profile.CreatedAt, &profile.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// staffNotFound is the single response to "no such profile in this tenant". It
// is used identically for a profile that does not exist and one that exists
// under a different tenant, which is what keeps the API from disclosing another
// tenant's roster.
func staffNotFound(cause error) error {
	return apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", cause)
}

// isLinkedUserUniqueViolation recognizes a second profile linking the same user
// inside one tenant. Like isSlugUniqueViolation in the tenant repository, it
// matches the specific constraint name rather than any 23505: a different unique
// violation added later must not be silently reported as this one.
func isLinkedUserUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "staff_profiles_tenant_user_unique"
}

// isMissingUserViolation recognizes a link to a user row that does not exist.
// The service layer checks membership first, so reaching this means the user was
// deleted between that check and the insert — a real race, reported as the same
// presentable validation failure rather than a 500.
func isMissingUserViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "staff_profiles_user_id_fkey"
}

// Create persists a staff profile. Status is defaulted here the same way
// PostgresServiceRepository.Create defaults a service: a caller that leaves it
// unset gets ACTIVE, and no code path lets a client choose.
func (r *PostgresStaffRepository) Create(ctx context.Context, profile *model.StaffProfile) (*model.StaffProfile, error) {
	status := profile.Status
	if status == "" {
		status = model.StatusActive
	}
	created := *profile
	created.Status = status
	const query = `INSERT INTO staff_profiles (id, tenant_id, user_id, display_name, bio, is_bookable, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING status, created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query,
		profile.ID, profile.TenantID, profile.UserID, profile.DisplayName,
		profile.Bio, profile.IsBookable, status,
	).Scan(&created.Status, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		if isLinkedUserUniqueViolation(err) {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "this user already has a staff profile in this tenant", err)
		}
		if isMissingUserViolation(err) {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "the linked user is not valid for this tenant", err)
		}
		return nil, fmt.Errorf("inserting staff profile: %w", err)
	}
	return &created, nil
}

func (r *PostgresStaffRepository) FindByID(ctx context.Context, tenantID string, staffID string) (*model.StaffProfile, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+staffColumns+" FROM staff_profiles WHERE id = $1 AND tenant_id = $2", staffID, tenantID)
	profile, err := scanStaff(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, staffNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding staff profile: %w", err)
	}
	return profile, nil
}

// ListByTenant returns the roster ordered by display name then id — the same
// deterministic-ordering discipline the catalog listing already applies.
func (r *PostgresStaffRepository) ListByTenant(ctx context.Context, tenantID string, filter StaffListFilter) ([]*model.StaffProfile, error) {
	query := "SELECT " + staffColumns + " FROM staff_profiles WHERE tenant_id = $1"
	args := []any{tenantID}
	if filter.Status != nil {
		query += " AND status = $2"
		args = append(args, string(*filter.Status))
	}
	query += " ORDER BY display_name ASC, id ASC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing staff profiles: %w", err)
	}
	defer rows.Close()
	var profiles []*model.StaffProfile
	for rows.Next() {
		profile, err := scanStaff(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning staff profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating staff profiles: %w", err)
	}
	return profiles, nil
}

// Update writes only the non-nil fields of update. status, tenant_id, user_id,
// id and created_at are never part of the SET clause — StaffUpdate has no fields
// for them — the same "absence is the enforcement" pattern used throughout.
func (r *PostgresStaffRepository) Update(ctx context.Context, tenantID string, staffID string, update StaffUpdate) (*model.StaffProfile, error) {
	sets := []string{}
	args := []any{}
	argIndex := 1

	if update.DisplayName != nil {
		sets = append(sets, fmt.Sprintf("display_name = $%d", argIndex))
		args = append(args, *update.DisplayName)
		argIndex++
	}
	if update.Bio != nil {
		sets = append(sets, fmt.Sprintf("bio = $%d", argIndex))
		args = append(args, *update.Bio)
		argIndex++
	}
	if update.IsBookable != nil {
		sets = append(sets, fmt.Sprintf("is_bookable = $%d", argIndex))
		args = append(args, *update.IsBookable)
		argIndex++
	}
	if len(sets) == 0 {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)
	}

	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, staffID, tenantID)

	query := fmt.Sprintf(
		"UPDATE staff_profiles SET %s WHERE id = $%d AND tenant_id = $%d RETURNING %s",
		strings.Join(sets, ", "), argIndex, argIndex+1, staffColumns,
	)

	row := r.db.QueryRowContext(ctx, query, args...)
	profile, err := scanStaff(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, staffNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("updating staff profile: %w", err)
	}
	return profile, nil
}

// Archive sets status to ARCHIVED. The row is never deleted: appointments will
// hold a foreign key to it from S10, and a customer's booking history must keep
// resolving after a technician leaves.
//
// Capability rows are deliberately left in place. They record what this person
// could do while they worked here, which stays true; and an owner who archives a
// profile by mistake should not silently lose that configuration.
func (r *PostgresStaffRepository) Archive(ctx context.Context, tenantID string, staffID string) (*model.StaffProfile, error) {
	row := r.db.QueryRowContext(ctx,
		"UPDATE staff_profiles SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND tenant_id = $3 RETURNING "+staffColumns,
		string(model.StatusArchived), staffID, tenantID,
	)
	profile, err := scanStaff(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, staffNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("archiving staff profile: %w", err)
	}
	return profile, nil
}

// --- capabilities ------------------------------------------------------------

// PostgresCapabilityRepository persists staff_services rows. It is a separate
// type from PostgresStaffRepository so a caller that only needs capability
// access is not handed profile mutation as well.
type PostgresCapabilityRepository struct{ db dbtx }

func NewPostgresCapabilityRepository(db dbtx) *PostgresCapabilityRepository {
	return &PostgresCapabilityRepository{db: db}
}

func (r *PostgresCapabilityRepository) ListServiceIDs(ctx context.Context, tenantID string, staffID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT service_id FROM staff_services WHERE staff_id = $1 AND tenant_id = $2 ORDER BY service_id ASC",
		staffID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("listing staff capabilities: %w", err)
	}
	defer rows.Close()
	serviceIDs := []string{}
	for rows.Next() {
		var serviceID string
		if err := rows.Scan(&serviceID); err != nil {
			return nil, fmt.Errorf("scanning staff capability: %w", err)
		}
		serviceIDs = append(serviceIDs, serviceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating staff capabilities: %w", err)
	}
	return serviceIDs, nil
}

func (r *PostgresCapabilityRepository) DeleteAll(ctx context.Context, tenantID string, staffID string) error {
	if _, err := r.db.ExecContext(ctx, "DELETE FROM staff_services WHERE staff_id = $1 AND tenant_id = $2", staffID, tenantID); err != nil {
		return fmt.Errorf("clearing staff capabilities: %w", err)
	}
	return nil
}

// Assign records one capability row.
//
// A composite-foreign-key violation means the staff member or the service does
// not belong to the named tenant. The service layer validates both beforehand,
// so reaching this is either a race or a caller bypassing that layer; either way
// it is reported as a presentable validation failure rather than a 500, because
// the database — not the Go code — is the authority on that pairing.
func (r *PostgresCapabilityRepository) Assign(ctx context.Context, tenantID string, staffID string, serviceID string) error {
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO staff_services (staff_id, service_id, tenant_id) VALUES ($1, $2, $3)",
		staffID, serviceID, tenantID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return apperrors.New(apperrors.CodeValidationFailed, "staff member and service must belong to the same tenant", err)
		}
		return fmt.Errorf("assigning staff capability: %w", err)
	}
	return nil
}

// compile-time guards: the Postgres implementations must keep satisfying their
// interfaces.
var (
	_ StaffRepository      = (*PostgresStaffRepository)(nil)
	_ CapabilityRepository = (*PostgresCapabilityRepository)(nil)
)
