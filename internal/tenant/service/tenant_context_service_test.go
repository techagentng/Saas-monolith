package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

func TestResolveTenantContextRequiresActiveMembership(t *testing.T) {
	tests := []struct {
		name       string
		membership *model.TenantMembership
		wantErr    apperrors.ErrorCode
	}{
		{name: "active", membership: &model.TenantMembership{TenantID: testTenantID, UserID: testUserID, Status: model.MembershipStatusActive}},
		{name: "disabled", membership: &model.TenantMembership{TenantID: testTenantID, UserID: testUserID, Status: model.MembershipStatusDisabled}, wantErr: apperrors.CodeTenantAccessDenied},
		{name: "missing", membership: nil, wantErr: apperrors.CodeTenantAccessDenied},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := NewTenantContextService(&tenantRepositoryFake{}, &membershipContextRepositoryFake{membership: test.membership})
			contextValue, err := resolver.Resolve(context.Background(), auth.Principal{UserID: testUserID, SessionID: "session"}, testTenantID)
			if test.wantErr != "" {
				var appErr *apperrors.AppError
				if !errors.As(err, &appErr) || appErr.Code != test.wantErr {
					t.Fatalf("error = %v, want %s", err, test.wantErr)
				}
				if contextValue != nil {
					t.Fatal("trusted tenant context was returned after failed verification")
				}
				return
			}
			if err != nil || contextValue == nil || contextValue.TenantID != testTenantID {
				t.Fatalf("Resolve() = %#v, %v", contextValue, err)
			}
		})
	}
}

func TestResolveTenantContextRejectsCrossTenantAccess(t *testing.T) {
	resolver := NewTenantContextService(&tenantRepositoryFake{}, &membershipContextRepositoryFake{membership: nil})
	contextValue, err := resolver.Resolve(context.Background(), auth.Principal{UserID: testUserID, SessionID: "session"}, "550e8400-e29b-41d4-a716-446655440099")
	if contextValue != nil {
		t.Fatal("cross-tenant context was returned")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantAccessDenied {
		t.Fatalf("error = %v, want TENANT_ACCESS_DENIED", err)
	}
}

func TestResolveTenantContextRejectsMalformedTenantID(t *testing.T) {
	resolver := NewTenantContextService(&tenantRepositoryFake{}, &membershipContextRepositoryFake{})
	contextValue, err := resolver.Resolve(context.Background(), auth.Principal{UserID: testUserID}, "not-a-uuid")
	if contextValue != nil {
		t.Fatal("trusted context was returned for malformed input")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeInvalidRequest {
		t.Fatalf("error = %v, want INVALID_REQUEST", err)
	}
}

type membershipContextRepositoryFake struct {
	membership *model.TenantMembership
	findErr    error
}

func (r *membershipContextRepositoryFake) Create(context.Context, model.TenantMembership) (*model.TenantMembership, error) {
	return nil, nil
}
func (r *membershipContextRepositoryFake) FindByTenantAndUser(context.Context, string, string) (*model.TenantMembership, error) {
	if r.findErr != nil {
		return nil, fmt.Errorf("find membership: %w", r.findErr)
	}
	return r.membership, nil
}
func (*membershipContextRepositoryFake) ListByUser(context.Context, string) ([]model.TenantMembership, error) {
	return nil, nil
}
func (*membershipContextRepositoryFake) Disable(context.Context, string, string, time.Time) error {
	return nil
}
