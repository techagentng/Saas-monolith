package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	authzmodel "github.com/techagentng/saas-monolith/internal/authorization/model"
	authzrepository "github.com/techagentng/saas-monolith/internal/authorization/repository"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identityrepository "github.com/techagentng/saas-monolith/internal/identity/repository"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// CreateTenantInput carries the transport-validated tenant creation request.
// CreatorUserID must come from the authenticated principal, never a request
// body field — the caller of Create is responsible for that boundary.
type CreateTenantInput struct {
	Name          string
	Slug          string
	CreatorUserID string
}

// UpdateTenantProfileRequest carries profile update fields from the transport layer.
// Only non-nil fields will be updated; omitted fields remain unchanged.
type UpdateTenantProfileRequest struct {
	Name         *string
	Description  *string
	ContactEmail *string
	ContactPhone *string
	Timezone     *string
}

type TenantService interface {
	Create(ctx context.Context, input CreateTenantInput) (*model.Tenant, error)
	UpdateProfile(ctx context.Context, tenantID string, req UpdateTenantProfileRequest) (*model.Tenant, error)
}

// txBeginner is satisfied by *sql.DB. It is the only capability TenantService
// needs beyond the repository interfaces it is handed.
type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

type tenantService struct {
	db      txBeginner
	users   identityrepository.UserRepository
	tenants repository.TenantRepository
}

func NewTenantService(db txBeginner, users identityrepository.UserRepository, tenants repository.TenantRepository) TenantService {
	return &tenantService{db: db, users: users, tenants: tenants}
}

// Create persists a tenant, an ACTIVE membership for the creator, and a
// BUSINESS_OWNER assignment for the creator, all in one transaction. Any
// failure after BeginTx rolls back everything; only a successful Commit
// leaves any of it in place.
func (s *tenantService) Create(ctx context.Context, input CreateTenantInput) (*model.Tenant, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "tenant name is required", nil)
	}
	// Feature 5: the slug is the tenant's public identity, so it must already
	// be canonical. It is validated here — before BeginTx — so a malformed or
	// reserved slug never opens a transaction, and never creates a tenant,
	// membership, or role assignment. The value is passed through untouched;
	// validation deliberately does not normalize.
	slug := input.Slug
	if err := model.ValidateSlug(slug); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(input.CreatorUserID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("starting tenant creation transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	tenants := repository.NewPostgresTenantRepository(tx)
	memberships := repository.NewPostgresMembershipRepository(tx)
	roles := authzrepository.NewPostgresRoleRepository(tx)
	userRoles := authzrepository.NewPostgresUserRoleRepository(tx)

	tenant, err := tenants.Create(ctx, &model.Tenant{ID: uuid.NewString(), Name: name, Slug: slug})
	if err != nil {
		return nil, err
	}

	membershipService := NewMembershipService(s.users, tenants, memberships)
	if _, err := membershipService.Create(ctx, CreateMembershipInput{TenantID: tenant.ID, UserID: input.CreatorUserID}); err != nil {
		return nil, err
	}

	// A missing BUSINESS_OWNER catalog row is a seed/environment integrity
	// failure, not something the caller can act on — it must not surface as
	// the business-facing ROLE_NOT_FOUND code (which would misleadingly
	// imply the caller specified a bad role). Re-classify it as a safe
	// INTERNAL_ERROR via the existing AppError mechanism, preserving the
	// original error as its cause (Unwrap/errors.Is still reach it) rather
	// than discarding the error chain.
	ownerRole, err := roles.FindByNameScope(ctx, "BUSINESS_OWNER", authzmodel.ScopeTenant)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "resolving business owner role", err)
	}

	// authzservice.AssignmentService cannot be reused here: it lives in
	// internal/authorization/service, which imports internal/tenant (for
	// tenant.TenantContext) — and internal/tenant imports internal/tenant/service
	// (for TenantContextService), so importing authzservice from this package
	// would be an import cycle. The tenant and membership were both just
	// created ACTIVE in this same transaction, so AssignmentService's own
	// defensive re-checks of tenant/membership status would be redundant
	// here anyway; the tx-scoped repository call below is equivalent for
	// this specific, already-validated case.
	assignment := authzmodel.UserRole{ID: uuid.NewString(), UserID: input.CreatorUserID, RoleID: ownerRole.ID, TenantID: tenant.ID, CreatedAt: time.Now().UTC()}
	if _, err := userRoles.Assign(ctx, assignment); err != nil {
		return nil, fmt.Errorf("assigning business owner: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing tenant creation: %w", err)
	}
	committed = true
	return tenant, nil
}

// UpdateProfile updates the profile fields of a tenant.
// Validates all input fields and calls the repository to persist changes.
func (s *tenantService) UpdateProfile(ctx context.Context, tenantID string, req UpdateTenantProfileRequest) (*model.Tenant, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}

	update := &repository.TenantProfileUpdate{}

	// Validate and prepare name field if provided
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "name cannot be empty", nil)
		}
		if len(name) > 255 {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "name exceeds maximum length", nil)
		}
		update.Name = &name
	}

	// Validate and prepare description field if provided. Unlike the other
	// optional fields, an empty description is a legitimate product state
	// ("this business has no description"), so it is accepted rather than
	// rejected. This is not null-clearing: the column is set to the empty
	// string, and an omitted description still leaves the stored value alone.
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		if len(desc) > 1000 {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "description exceeds maximum length", nil)
		}
		update.Description = &desc
	}

	// Validate and prepare contact email field if provided
	if req.ContactEmail != nil {
		email := strings.TrimSpace(*req.ContactEmail)
		if email == "" {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "contact email cannot be empty if provided", nil)
		}
		if _, err := mail.ParseAddress(email); err != nil {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid email format", nil)
		}
		update.ContactEmail = &email
	}

	// Validate and prepare contact phone field if provided
	if req.ContactPhone != nil {
		phone := strings.TrimSpace(*req.ContactPhone)
		if phone == "" {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "contact phone cannot be empty if provided", nil)
		}
		if len(phone) > 20 {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "phone number exceeds maximum length", nil)
		}
		update.ContactPhone = &phone
	}

	// Validate and prepare timezone field if provided
	if req.Timezone != nil {
		tz := strings.TrimSpace(*req.Timezone)
		if tz == "" {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "timezone cannot be empty if provided", nil)
		}
		if _, err := time.LoadLocation(tz); err != nil {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid timezone identifier", nil)
		}
		update.Timezone = &tz
	}

	// Verify at least one field is being updated
	if update.IsEmpty() {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)
	}

	// Call repository to persist the update
	updated, err := s.tenants.UpdateProfile(ctx, tenantID, *update)
	if err != nil {
		return nil, err
	}

	return updated, nil
}
