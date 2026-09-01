package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// CreateStaffInput carries a transport-validated staff creation request.
//
// TenantID is deliberately absent: it comes from the trusted tenant context
// resolved by the middleware chain. Status is absent because a new profile is
// always ACTIVE and archiving is its own operation; the timestamps are absent
// because the database owns them.
type CreateStaffInput struct {
	DisplayName string
	Bio         *string
	// UserID nil creates a non-login worker. When supplied it must name an
	// existing user with an ACTIVE membership in this tenant.
	UserID *string
	// IsBookable nil defaults to true — the common case is that a person added
	// to the roster can take appointments.
	IsBookable *bool
}

// UpdateStaffInput carries a partial update. Only non-nil fields change.
//
// UserID is absent by design: re-pointing a profile at a different person is a
// re-identification of historical work, not an edit, and S3's approved contract
// covers display name, bio and bookability only.
type UpdateStaffInput struct {
	DisplayName *string
	Bio         *string
	IsBookable  *bool
}

// MembershipReader is the narrow slice of tenant persistence this module needs
// to validate a staff profile's optional user link. Declared here, in the
// consumer, rather than imported wholesale — the same interface-segregation
// reasoning behind TenantReader.
type MembershipReader interface {
	FindByTenantAndUser(ctx context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error)
}

// txBeginner is satisfied by *sql.DB. It is the only capability StaffService
// needs beyond the repository interfaces it is handed, mirroring how
// TenantService already obtains its transaction.
type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// StaffService owns the staff rules: validation, the user-link check, archive
// idempotency, and atomic capability replacement.
//
// Tenant access and the staff.* permission are verified by the production
// middleware chain before any method here is reached. This service does not
// re-derive authorization; it does scope every repository call by tenantID, so a
// defect in that chain cannot become a cross-tenant read or write.
type StaffService interface {
	Create(ctx context.Context, tenantID string, input CreateStaffInput) (*model.StaffProfile, error)
	Get(ctx context.Context, tenantID string, staffID string) (*model.StaffProfile, error)
	// List returns the roster. statusFilter accepts "ACTIVE", "ARCHIVED" or
	// "ALL"; an empty string means the default, ACTIVE.
	List(ctx context.Context, tenantID string, statusFilter string) ([]*model.StaffProfile, error)
	Update(ctx context.Context, tenantID string, staffID string, input UpdateStaffInput) (*model.StaffProfile, error)
	// Archive moves a profile ACTIVE -> ARCHIVED. Calling it on an already
	// archived profile is idempotent: it returns the profile unchanged without
	// writing, so a repeated call never disturbs updated_at.
	Archive(ctx context.Context, tenantID string, staffID string) (*model.StaffProfile, error)
	// ListCapabilities returns the service IDs this staff member can perform.
	ListCapabilities(ctx context.Context, tenantID string, staffID string) ([]string, error)
	// ReplaceCapabilities sets the complete capability set atomically. Either
	// every requested assignment persists or none does — a set containing one
	// unknown or foreign service leaves the previous set entirely intact.
	ReplaceCapabilities(ctx context.Context, tenantID string, staffID string, serviceIDs []string) ([]string, error)
}

type staffService struct {
	db           txBeginner
	staff        repository.StaffRepository
	capabilities repository.CapabilityRepository
	services     repository.ServiceRepository
	memberships  MembershipReader
}

func NewStaffService(
	db txBeginner,
	staff repository.StaffRepository,
	capabilities repository.CapabilityRepository,
	services repository.ServiceRepository,
	memberships MembershipReader,
) StaffService {
	return &staffService{db: db, staff: staff, capabilities: capabilities, services: services, memberships: memberships}
}

func (s *staffService) Create(ctx context.Context, tenantID string, input CreateStaffInput) (*model.StaffProfile, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}

	displayName, err := model.ValidateDisplayName(input.DisplayName)
	if err != nil {
		return nil, err
	}
	bio, err := model.ValidateBio(input.Bio)
	if err != nil {
		return nil, err
	}

	userID, err := s.validateUserLink(ctx, tenantID, input.UserID)
	if err != nil {
		return nil, err
	}

	// A roster entry is bookable unless the caller says otherwise.
	isBookable := true
	if input.IsBookable != nil {
		isBookable = *input.IsBookable
	}

	return s.staff.Create(ctx, &model.StaffProfile{
		ID:          uuid.NewString(),
		TenantID:    tenantID,
		UserID:      userID,
		DisplayName: displayName,
		Bio:         bio,
		IsBookable:  isBookable,
		// Status left unset so the repository's own defaulting applies,
		// producing ACTIVE.
	})
}

// validateUserLink enforces what an optional user link must satisfy: the user
// must exist AND already hold an ACTIVE membership in this tenant.
//
// Requiring membership stops a tenant from claiming an arbitrary platform user
// as its staff, and matches what the link is eventually for — letting that
// person see their own calendar, which requires workspace access anyway.
//
// It is checked at write time only. A membership revoked later does NOT alter,
// archive or unlink the profile: those are separate lifecycles by design, and
// silently mutating scheduling data from a membership change would be exactly
// the hidden synchronization the approved plan warns against.
//
// A nonexistent user and a non-member user return the identical error, so this
// endpoint cannot be used to probe which user IDs exist on the platform.
func (s *staffService) validateUserLink(ctx context.Context, tenantID string, candidate *string) (*string, error) {
	if candidate == nil {
		// A non-login worker. No user row is created for them, and none is
		// required.
		return nil, nil
	}
	userID := *candidate
	if _, err := uuid.Parse(userID); err != nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid linked user", nil)
	}
	membership, err := s.memberships.FindByTenantAndUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if membership == nil || membership.Status != tenantmodel.MembershipStatusActive {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "the linked user is not an active member of this tenant", nil)
	}
	return &userID, nil
}

func (s *staffService) Get(ctx context.Context, tenantID string, staffID string) (*model.StaffProfile, error) {
	if err := validateStaffIdentifiers(tenantID, staffID); err != nil {
		return nil, err
	}
	return s.staff.FindByID(ctx, tenantID, staffID)
}

func (s *staffService) List(ctx context.Context, tenantID string, statusFilter string) ([]*model.StaffProfile, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	filter, err := ParseStaffStatusFilter(statusFilter)
	if err != nil {
		return nil, err
	}
	return s.staff.ListByTenant(ctx, tenantID, filter)
}

// ParseStaffStatusFilter converts the roster endpoint's status query parameter
// into a repository filter, mirroring the catalog's own filter vocabulary so the
// two listings behave identically. An unrecognized value is rejected rather than
// silently falling back to the default.
func ParseStaffStatusFilter(raw string) (repository.StaffListFilter, error) {
	switch raw {
	case "", string(model.StatusActive):
		status := model.StatusActive
		return repository.StaffListFilter{Status: &status}, nil
	case string(model.StatusArchived):
		status := model.StatusArchived
		return repository.StaffListFilter{Status: &status}, nil
	case "ALL":
		return repository.StaffListFilter{}, nil
	default:
		return repository.StaffListFilter{}, apperrors.New(apperrors.CodeValidationFailed, "invalid status filter", nil)
	}
}

func (s *staffService) Update(ctx context.Context, tenantID string, staffID string, input UpdateStaffInput) (*model.StaffProfile, error) {
	if err := validateStaffIdentifiers(tenantID, staffID); err != nil {
		return nil, err
	}

	update := repository.StaffUpdate{}

	if input.DisplayName != nil {
		displayName, err := model.ValidateDisplayName(*input.DisplayName)
		if err != nil {
			return nil, err
		}
		update.DisplayName = &displayName
	}
	if input.Bio != nil {
		bio, err := model.ValidateBio(input.Bio)
		if err != nil {
			return nil, err
		}
		update.Bio = bio
	}
	if input.IsBookable != nil {
		update.IsBookable = input.IsBookable
	}

	if update.IsEmpty() {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)
	}

	return s.staff.Update(ctx, tenantID, staffID, update)
}

func (s *staffService) Archive(ctx context.Context, tenantID string, staffID string) (*model.StaffProfile, error) {
	if err := validateStaffIdentifiers(tenantID, staffID); err != nil {
		return nil, err
	}

	current, err := s.staff.FindByID(ctx, tenantID, staffID)
	if err != nil {
		return nil, err
	}
	// Idempotent: an already archived profile is returned as-is, without a
	// write, mirroring CatalogService.Archive.
	if current.Status == model.StatusArchived {
		return current, nil
	}
	return s.staff.Archive(ctx, tenantID, staffID)
}

func (s *staffService) ListCapabilities(ctx context.Context, tenantID string, staffID string) ([]string, error) {
	if err := validateStaffIdentifiers(tenantID, staffID); err != nil {
		return nil, err
	}
	// Resolved through the staff repository first so an unknown or foreign
	// staff ID yields STAFF_NOT_FOUND rather than an empty capability list,
	// which would wrongly imply the profile exists with nothing assigned.
	if _, err := s.staff.FindByID(ctx, tenantID, staffID); err != nil {
		return nil, err
	}
	return s.capabilities.ListServiceIDs(ctx, tenantID, staffID)
}

// ReplaceCapabilities sets the complete set of services a staff member can
// perform, atomically.
//
// Every service is validated against the tenant BEFORE the transaction opens, so
// a set naming an unknown or another tenant's service never clears the existing
// assignments. Inside the transaction the old rows are deleted and the new ones
// inserted; any failure rolls the whole thing back, leaving the previous set
// exactly as it was. The composite foreign keys on staff_services are the
// backstop beneath all of it.
func (s *staffService) ReplaceCapabilities(ctx context.Context, tenantID string, staffID string, serviceIDs []string) ([]string, error) {
	if err := validateStaffIdentifiers(tenantID, staffID); err != nil {
		return nil, err
	}

	// The staff member must exist in this tenant. Checked before anything is
	// written, and reported as STAFF_NOT_FOUND identically for a missing profile
	// and one belonging to another tenant.
	if _, err := s.staff.FindByID(ctx, tenantID, staffID); err != nil {
		return nil, err
	}

	// De-duplicate while preserving the caller's intent: sending the same
	// service twice is a sloppy request, not an error, but it must not attempt
	// two inserts of the same primary key.
	unique := make([]string, 0, len(serviceIDs))
	seen := make(map[string]struct{}, len(serviceIDs))
	for _, serviceID := range serviceIDs {
		if _, err := uuid.Parse(serviceID); err != nil {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid service id in capability set", err)
		}
		if _, duplicate := seen[serviceID]; duplicate {
			continue
		}
		seen[serviceID] = struct{}{}
		unique = append(unique, serviceID)
	}

	// Every service must exist in THIS tenant. FindByID is tenant-scoped, so a
	// service belonging to another tenant is indistinguishable from a
	// nonexistent one — the caller learns their set is invalid, not whether that
	// ID exists elsewhere on the platform.
	for _, serviceID := range unique {
		if _, err := s.services.FindByID(ctx, tenantID, serviceID); err != nil {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "capability set names a service that does not belong to this tenant", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("starting capability replacement transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	capabilities := repository.NewPostgresCapabilityRepository(tx)
	if err := capabilities.DeleteAll(ctx, tenantID, staffID); err != nil {
		return nil, err
	}
	for _, serviceID := range unique {
		if err := capabilities.Assign(ctx, tenantID, staffID, serviceID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing capability replacement: %w", err)
	}
	committed = true

	// Read back through the non-transactional repository so the response
	// reflects committed state rather than the caller's request echoed back.
	return s.capabilities.ListServiceIDs(ctx, tenantID, staffID)
}

// validateStaffIdentifiers refuses a malformed tenant or staff id before any
// query runs. Both are INVALID_REQUEST rather than STAFF_NOT_FOUND: a
// syntactically impossible id is a broken request, not a missing resource.
func validateStaffIdentifiers(tenantID string, staffID string) error {
	if _, err := uuid.Parse(tenantID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	if _, err := uuid.Parse(staffID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid staff id", err)
	}
	return nil
}

// compile-time guard: the implementation must keep satisfying its interface.
var _ StaffService = (*staffService)(nil)
