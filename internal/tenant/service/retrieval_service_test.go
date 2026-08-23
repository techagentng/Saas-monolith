package service

import (
	"context"
	"errors"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

func TestRetrievalServiceListAccessibleReturnsAccessibleTenants(t *testing.T) {
	userID := "550e8400-e29b-41d4-a716-446655440000"
	tenants := []*model.Tenant{
		{ID: "550e8400-e29b-41d4-a716-446655440001", Name: "A", Status: model.StatusActive, CreatedAt: time.Now()},
		{ID: "550e8400-e29b-41d4-a716-446655440002", Name: "B", Status: model.StatusActive, CreatedAt: time.Now()},
	}
	repo := &fakeTenantRepository{listAccessibleResult: tenants}
	svc := NewRetrievalService(repo)

	result, err := svc.ListAccessible(context.Background(), userID)

	if err != nil {
		t.Fatalf("ListAccessible() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("ListAccessible() returned %d tenants, want 2", len(result))
	}
	if result[0].ID != "550e8400-e29b-41d4-a716-446655440001" {
		t.Fatalf("ListAccessible() first tenant ID = %q", result[0].ID)
	}
}

func TestRetrievalServiceListAccessibleReturnsEmptyListForNoTenants(t *testing.T) {
	userID := "550e8400-e29b-41d4-a716-446655440000"
	repo := &fakeTenantRepository{listAccessibleResult: []*model.Tenant{}}
	svc := NewRetrievalService(repo)

	result, err := svc.ListAccessible(context.Background(), userID)

	if err != nil {
		t.Fatalf("ListAccessible() error = %v", err)
	}
	if result == nil {
		t.Fatalf("ListAccessible() returned nil, want empty slice")
	}
	if len(result) != 0 {
		t.Fatalf("ListAccessible() returned %d tenants, want 0", len(result))
	}
}

func TestRetrievalServiceListAccessibleRejectsInvalidUserID(t *testing.T) {
	repo := &fakeTenantRepository{}
	svc := NewRetrievalService(repo)

	_, err := svc.ListAccessible(context.Background(), "not-a-uuid")

	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeInvalidRequest {
		t.Fatalf("ListAccessible() error = %v, want INVALID_REQUEST", err)
	}
}

func TestRetrievalServiceListAccessiblePropagatesDatabaseError(t *testing.T) {
	userID := "550e8400-e29b-41d4-a716-446655440000"
	repo := &fakeTenantRepository{listAccessibleErr: errors.New("database connection failed")}
	svc := NewRetrievalService(repo)

	_, err := svc.ListAccessible(context.Background(), userID)

	if err == nil {
		t.Fatal("ListAccessible() succeeded, want error")
	}
	if !errors.Is(err, repo.listAccessibleErr) {
		t.Fatalf("ListAccessible() error = %v, want wrapped database error", err)
	}
}

func TestRetrievalServiceGetAccessibleReturnsAccessibleTenant(t *testing.T) {
	userID := "550e8400-e29b-41d4-a716-446655440000"
	tenantID := "550e8400-e29b-41d4-a716-446655440001"
	tenant := &model.Tenant{ID: tenantID, Name: "Test", Status: model.StatusActive}
	repo := &fakeTenantRepository{listAccessibleResult: []*model.Tenant{tenant}}
	svc := NewRetrievalService(repo)

	result, err := svc.GetAccessible(context.Background(), userID, tenantID)

	if err != nil {
		t.Fatalf("GetAccessible() error = %v", err)
	}
	if result.ID != tenantID {
		t.Fatalf("GetAccessible() returned tenant ID = %q, want %q", result.ID, tenantID)
	}
}

func TestRetrievalServiceGetAccessibleDeniesAccessIfNotInList(t *testing.T) {
	userID := "550e8400-e29b-41d4-a716-446655440000"
	requestedTenantID := "550e8400-e29b-41d4-a716-446655440099"
	otherTenant := &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440001", Name: "Other"}
	repo := &fakeTenantRepository{listAccessibleResult: []*model.Tenant{otherTenant}}
	svc := NewRetrievalService(repo)

	_, err := svc.GetAccessible(context.Background(), userID, requestedTenantID)

	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantAccessDenied {
		t.Fatalf("GetAccessible() error = %v, want TENANT_ACCESS_DENIED", err)
	}
}

func TestRetrievalServiceGetAccessibleRejectsInvalidUserID(t *testing.T) {
	repo := &fakeTenantRepository{}
	svc := NewRetrievalService(repo)

	_, err := svc.GetAccessible(context.Background(), "not-a-uuid", "550e8400-e29b-41d4-a716-446655440001")

	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeInvalidRequest {
		t.Fatalf("GetAccessible() error = %v, want INVALID_REQUEST", err)
	}
}

func TestRetrievalServiceGetAccessibleRejectsInvalidTenantID(t *testing.T) {
	repo := &fakeTenantRepository{}
	svc := NewRetrievalService(repo)

	_, err := svc.GetAccessible(context.Background(), "550e8400-e29b-41d4-a716-446655440000", "not-a-uuid")

	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeInvalidRequest {
		t.Fatalf("GetAccessible() error = %v, want INVALID_REQUEST", err)
	}
}

func TestRetrievalServiceGetAccessibleDeniesWhenNoMemberships(t *testing.T) {
	userID := "550e8400-e29b-41d4-a716-446655440000"
	tenantID := "550e8400-e29b-41d4-a716-446655440001"
	repo := &fakeTenantRepository{listAccessibleResult: []*model.Tenant{}}
	svc := NewRetrievalService(repo)

	_, err := svc.GetAccessible(context.Background(), userID, tenantID)

	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantAccessDenied {
		t.Fatalf("GetAccessible() error = %v, want TENANT_ACCESS_DENIED", err)
	}
}

// Fake repository for unit testing
type fakeTenantRepository struct {
	listAccessibleResult []*model.Tenant
	listAccessibleErr    error
}

func (f *fakeTenantRepository) Create(ctx context.Context, tenant *model.Tenant) (*model.Tenant, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeTenantRepository) FindByID(ctx context.Context, id string) (*model.Tenant, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeTenantRepository) ListAccessibleByUserID(ctx context.Context, userID string) ([]*model.Tenant, error) {
	if f.listAccessibleErr != nil {
		return nil, f.listAccessibleErr
	}
	return f.listAccessibleResult, nil
}
