package service

import (
	"context"
	"database/sql"
	"fmt"
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

type TenantService interface {
	Create(ctx context.Context, input CreateTenantInput) (*model.Tenant, error)
}

// txBeginner is satisfied by *sql.DB. It is the only capability TenantService
// needs beyond the repository interfaces it is handed.
type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

type tenantService struct {
	db    txBeginner
	users identityrepository.UserRepository
}

func NewTenantService(db txBeginner, users identityrepository.UserRepository) TenantService {
	return &tenantService{db: db, users: users}
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
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "tenant slug is required", nil)
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
