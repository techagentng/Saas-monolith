package service

import (
	"context"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

func tenantWithBusinessType(businessType tenantmodel.BusinessType) *fakeTenantReader {
	return &fakeTenantReader{tenant: &tenantmodel.Tenant{
		ID: tenantA, Name: "Acme Nails", Slug: "acme-nails", Status: tenantmodel.StatusActive, BusinessType: &businessType,
	}}
}

func TestSuggestionListReturnsTheTenantsVerticalCatalogue(t *testing.T) {
	tenants := tenantWithBusinessType(tenantmodel.BusinessTypeNailTechnician)
	svc := NewSuggestionService(tenants)

	list, err := svc.List(context.Background(), tenantA)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) == 0 {
		t.Fatal("List() returned no suggestions for a NAIL_TECHNICIAN tenant")
	}
	for _, suggestion := range list {
		if suggestion.Category == "" || suggestion.Name == "" {
			t.Fatalf("suggestion missing category or name: %+v", suggestion)
		}
	}
}

func TestSuggestionListReturnsEmptyForAnUnsupportedVertical(t *testing.T) {
	tenants := tenantWithBusinessType(tenantmodel.BusinessTypeRestaurant)
	svc := NewSuggestionService(tenants)

	list, err := svc.List(context.Background(), tenantA)
	if err != nil {
		t.Fatalf("List() error = %v, want a normal empty response, not an error", err)
	}
	if len(list) != 0 {
		t.Fatalf("List() = %#v, want empty for a vertical with no starter catalogue", list)
	}
}

func TestSuggestionListReturnsEmptyForATenantWithNoBusinessTypeYet(t *testing.T) {
	tenants := tenantWithoutCurrency() // BusinessType nil, same fixture shape
	svc := NewSuggestionService(tenants)

	list, err := svc.List(context.Background(), tenantA)
	if err != nil {
		t.Fatalf("List() error = %v, want a normal empty response", err)
	}
	if len(list) != 0 {
		t.Fatalf("List() = %#v, want empty for a tenant with no business type", list)
	}
}

func TestSuggestionListRejectsMalformedTenantID(t *testing.T) {
	svc := NewSuggestionService(tenantWithBusinessType(tenantmodel.BusinessTypeNailTechnician))

	_, err := svc.List(context.Background(), "not-a-uuid")
	assertCode(t, err, apperrors.CodeInvalidRequest, "List(malformed tenant id)")
}

func TestSuggestionListPropagatesTenantLookupFailure(t *testing.T) {
	tenants := &fakeTenantReader{findErr: apperrors.New(apperrors.CodeInternalError, "boom", nil)}
	svc := NewSuggestionService(tenants)

	_, err := svc.List(context.Background(), tenantA)
	if err == nil {
		t.Fatal("List() swallowed a tenant lookup failure")
	}
}
