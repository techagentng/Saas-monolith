package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identitymodel "github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

const (
	testTenantID = "550e8400-e29b-41d4-a716-446655440000"
	testUserID   = "550e8400-e29b-41d4-a716-446655440001"
)

func TestCreateMembership(t *testing.T) {
	repository := &membershipRepositoryFake{}
	service := NewMembershipService(&userRepositoryFake{}, &tenantRepositoryFake{}, repository)

	created, err := service.Create(context.Background(), CreateMembershipInput{TenantID: testTenantID, UserID: testUserID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != model.MembershipStatusActive || repository.created.TenantID != testTenantID {
		t.Fatalf("created membership = %#v", created)
	}
}

func TestCreateMembershipPreservesDuplicateError(t *testing.T) {
	repository := &membershipRepositoryFake{createErr: apperrors.New(apperrors.CodeTenantMembershipAlreadyExists, "duplicate", nil)}
	service := NewMembershipService(&userRepositoryFake{}, &tenantRepositoryFake{}, repository)

	_, err := service.Create(context.Background(), CreateMembershipInput{TenantID: testTenantID, UserID: testUserID})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantMembershipAlreadyExists {
		t.Fatalf("error = %v, want wrapped duplicate membership error", err)
	}
}

func TestRevokeMembershipIsIdempotent(t *testing.T) {
	repository := &membershipRepositoryFake{}
	service := NewMembershipService(&userRepositoryFake{}, &tenantRepositoryFake{}, repository)

	if err := service.Revoke(context.Background(), testTenantID, testUserID); err != nil {
		t.Fatalf("first Revoke() error = %v", err)
	}
	if err := service.Revoke(context.Background(), testTenantID, testUserID); err != nil {
		t.Fatalf("second Revoke() error = %v", err)
	}
	if repository.disableCalls != 2 {
		t.Fatalf("disable calls = %d, want 2 idempotent repository operations", repository.disableCalls)
	}
}

type userRepositoryFake struct{}

func (*userRepositoryFake) Create(_ context.Context, user identitymodel.User) (*identitymodel.User, error) {
	return &user, nil
}
func (*userRepositoryFake) FindByID(context.Context, string) (*identitymodel.User, error) {
	return &identitymodel.User{ID: testUserID, Status: identitymodel.StatusActive}, nil
}
func (*userRepositoryFake) FindByEmail(context.Context, string) (*identitymodel.User, error) {
	return nil, nil
}

type tenantRepositoryFake struct {
	tenant  *model.Tenant
	findErr error
}

func (r *tenantRepositoryFake) Create(_ context.Context, tenant *model.Tenant) (*model.Tenant, error) {
	return tenant, nil
}

func (r *tenantRepositoryFake) FindByID(context.Context, string) (*model.Tenant, error) {
	if r.findErr != nil {
		return nil, fmt.Errorf("find tenant: %w", r.findErr)
	}
	if r.tenant != nil {
		return r.tenant, nil
	}
	return &model.Tenant{ID: testTenantID, Status: model.StatusActive}, nil
}

func (r *tenantRepositoryFake) ListAccessibleByUserID(context.Context, string) ([]*model.Tenant, error) {
	return []*model.Tenant{}, nil
}

func (r *tenantRepositoryFake) UpdateProfile(context.Context, string, repository.TenantProfileUpdate) (*model.Tenant, error) {
	return nil, nil
}

type membershipRepositoryFake struct {
	created      *model.TenantMembership
	createErr    error
	disableErr   error
	disableCalls int
}

func (r *membershipRepositoryFake) Create(_ context.Context, membership model.TenantMembership) (*model.TenantMembership, error) {
	if r.createErr != nil {
		return nil, fmt.Errorf("create membership: %w", r.createErr)
	}
	r.created = &membership
	return &membership, nil
}
func (*membershipRepositoryFake) FindByTenantAndUser(context.Context, string, string) (*model.TenantMembership, error) {
	return &model.TenantMembership{TenantID: testTenantID, UserID: testUserID, Status: model.MembershipStatusActive}, nil
}
func (*membershipRepositoryFake) ListByUser(context.Context, string) ([]model.TenantMembership, error) {
	return nil, nil
}
func (r *membershipRepositoryFake) Disable(context.Context, string, string, time.Time) error {
	r.disableCalls++
	return r.disableErr
}
