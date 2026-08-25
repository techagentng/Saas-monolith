package service

import (
	"context"
	"testing"

	identityrepository "github.com/techagentng/saas-monolith/internal/identity/repository"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// F1: business_type is validated before BEGIN (tenant_service_test.go proves
// this against fakes); this proves it against a real database that a
// rejected creation with an otherwise-valid request truly leaves no tenant,
// membership, or role assignment behind — the same shape as
// TestCreateTenantWithInvalidSlugCreatesNothing in tenant_slug_integration_test.go.
func TestCreateTenantWithMissingBusinessTypeCreatesNothing(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db, "missing-business-type@example.com")
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	_, err := svc.Create(ctx, CreateTenantInput{Name: "Acme Salon", Slug: "acme-salon-no-type", CreatorUserID: userID})
	if err == nil {
		t.Fatal("Create() succeeded with missing business_type, want error")
	}

	assertCreationLeftNothing(t, db, ctx, userID)
}

func TestCreateTenantWithInvalidBusinessTypeCreatesNothing(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db, "invalid-business-type@example.com")
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	for _, businessType := range []string{"HOTEL ", "barbershop", "SUPER_ADMIN"} {
		_, err := svc.Create(ctx, CreateTenantInput{Name: "Acme Salon", Slug: "acme-salon-bad-type", BusinessType: businessType, CreatorUserID: userID})
		if err == nil {
			t.Fatalf("Create(business_type=%q) succeeded, want error", businessType)
		}
	}

	assertCreationLeftNothing(t, db, ctx, userID)
}
