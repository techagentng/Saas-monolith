package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// slugLookupRepository serves a single tenant by slug and records what it was
// asked for, so tests can prove the service passes the slug through unchanged.
type slugLookupRepository struct {
	tenantRepositoryFake

	tenant       *model.Tenant
	err          error
	requestedFor string
	calls        int
}

func (r *slugLookupRepository) FindBySlug(_ context.Context, slug string) (*model.Tenant, error) {
	r.calls++
	r.requestedFor = slug
	if r.err != nil {
		return nil, r.err
	}
	if r.tenant == nil || r.tenant.Slug != slug {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return r.tenant, nil
}

func activeSlugTenant() *model.Tenant {
	description := "Full service salon"
	timezone := "Africa/Lagos"
	email := "private@acme.test"
	phone := "+2348012345678"
	businessType := model.BusinessTypeNailTechnician
	return &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655441001", Name: "Acme Salon", Slug: "acme-salon",
		Status: model.StatusActive, Description: &description, Timezone: &timezone,
		ContactEmail: &email, ContactPhone: &phone,
		BusinessType: &businessType, OnboardingStatus: model.OnboardingStatusCompleted,
	}
}

func assertPublicCode(t *testing.T, err error, expected apperrors.ErrorCode) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != expected {
		t.Fatalf("error = %v, want %q", err, expected)
	}
}

func TestPublicTenantServiceResolvesActiveTenantBySlug(t *testing.T) {
	repo := &slugLookupRepository{tenant: activeSlugTenant()}
	service := NewPublicTenantService(repo)

	identity, err := service.GetBySlug(context.Background(), "acme-salon")
	if err != nil {
		t.Fatalf("GetBySlug() error = %v", err)
	}
	if identity.Slug != "acme-salon" || identity.Name != "Acme Salon" {
		t.Fatalf("identity = %#v", identity)
	}
	if identity.Description == nil || *identity.Description != "Full service salon" {
		t.Fatalf("Description = %v", identity.Description)
	}
	if identity.Timezone == nil || *identity.Timezone != "Africa/Lagos" {
		t.Fatalf("Timezone = %v", identity.Timezone)
	}
	if repo.requestedFor != "acme-salon" {
		t.Fatalf("repository queried for %q, want the slug unchanged", repo.requestedFor)
	}
}

// A DISABLED tenant is hidden from public callers and is indistinguishable
// from a slug that was never registered. activeSlugTenant is COMPLETED, so
// this proves DISABLED+COMPLETED specifically; see the visibility-matrix
// tests below for the other three combinations.
func TestPublicTenantServiceHidesDisabledTenant(t *testing.T) {
	tenant := activeSlugTenant()
	tenant.Status = model.StatusDisabled
	repo := &slugLookupRepository{tenant: tenant}

	_, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "acme-salon")
	assertPublicCode(t, err, apperrors.CodeTenantNotFound)
}

// --- Vertical Onboarding F3: publicly visible iff ACTIVE AND COMPLETED -----

// The full visibility matrix. Only ACTIVE+COMPLETED resolves; every other
// combination collapses to the same TENANT_NOT_FOUND as a nonexistent slug —
// F3 does not add a new lifecycle state, it adds a second required condition
// alongside Feature 5's existing ACTIVE check.
func TestPublicTenantServiceVisibilityMatrix(t *testing.T) {
	cases := []struct {
		name             string
		status           model.Status
		onboardingStatus model.OnboardingStatus
		wantVisible      bool
	}{
		{"active and completed", model.StatusActive, model.OnboardingStatusCompleted, true},
		{"active and in progress", model.StatusActive, model.OnboardingStatusInProgress, false},
		{"disabled and completed", model.StatusDisabled, model.OnboardingStatusCompleted, false},
		{"disabled and in progress", model.StatusDisabled, model.OnboardingStatusInProgress, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenant := activeSlugTenant()
			tenant.Status = tc.status
			tenant.OnboardingStatus = tc.onboardingStatus
			repo := &slugLookupRepository{tenant: tenant}

			identity, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "acme-salon")
			if tc.wantVisible {
				if err != nil {
					t.Fatalf("GetBySlug() error = %v, want visible", err)
				}
				if identity == nil {
					t.Fatal("GetBySlug() returned nil identity for a visible tenant")
				}
				return
			}
			assertPublicCode(t, err, apperrors.CodeTenantNotFound)
		})
	}
}

// An IN_PROGRESS tenant specifically, since this is the case F3 exists to
// close (a freshly created tenant was previously still ACTIVE and thus
// publicly resolvable before onboarding completed).
func TestPublicTenantServiceHidesInProgressTenant(t *testing.T) {
	tenant := activeSlugTenant()
	tenant.OnboardingStatus = model.OnboardingStatusInProgress
	repo := &slugLookupRepository{tenant: tenant}

	_, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "acme-salon")
	assertPublicCode(t, err, apperrors.CodeTenantNotFound)
}

func TestPublicTenantIdentityIncludesBusinessType(t *testing.T) {
	repo := &slugLookupRepository{tenant: activeSlugTenant()}
	identity, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "acme-salon")
	if err != nil {
		t.Fatal(err)
	}
	if identity.BusinessType == nil || *identity.BusinessType != model.BusinessTypeNailTechnician {
		t.Fatalf("BusinessType = %v, want NAIL_TECHNICIAN", identity.BusinessType)
	}
}

// A legacy tenant (pre-Feature-1: business_type NULL, onboarding_status
// COMPLETED via the migration default) must remain publicly resolvable —
// F3 must not force it through onboarding, and must not invent a business
// type for it. null is the approved safe representation.
func TestPublicTenantIdentityForLegacyTenantHasNilBusinessType(t *testing.T) {
	tenant := &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655441002", Name: "Legacy Salon", Slug: "legacy-salon",
		Status: model.StatusActive, OnboardingStatus: model.OnboardingStatusCompleted,
	}
	repo := &slugLookupRepository{tenant: tenant}

	identity, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "legacy-salon")
	if err != nil {
		t.Fatalf("GetBySlug() error = %v, want a legacy COMPLETED tenant to remain visible", err)
	}
	if identity.BusinessType != nil {
		t.Fatalf("BusinessType = %v, want nil for a legacy tenant, not an invented value", identity.BusinessType)
	}
}

func TestPublicTenantServiceReturnsNotFoundForUnknownSlug(t *testing.T) {
	repo := &slugLookupRepository{tenant: activeSlugTenant()}

	_, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "does-not-exist")
	assertPublicCode(t, err, apperrors.CodeTenantNotFound)
}

// A syntactically invalid slug can never match a stored tenant, so it is
// refused before any query is issued.
func TestPublicTenantServiceRejectsInvalidSlugWithoutQuerying(t *testing.T) {
	for _, slug := range []string{"Acme", "acme salon", "acme_salon", "-acme", "ac", ""} {
		repo := &slugLookupRepository{tenant: activeSlugTenant()}
		_, err := NewPublicTenantService(repo).GetBySlug(context.Background(), slug)
		assertPublicCode(t, err, apperrors.CodeTenantSlugInvalid)
		if repo.calls != 0 {
			t.Fatalf("slug %q reached the repository despite being invalid", slug)
		}
	}
}

// A reserved slug is never a tenant, and must not leak that it is reserved
// rather than simply absent.
func TestPublicTenantServiceTreatsReservedSlugAsNotFound(t *testing.T) {
	repo := &slugLookupRepository{tenant: activeSlugTenant()}
	_, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "admin")
	assertPublicCode(t, err, apperrors.CodeTenantNotFound)
	if repo.calls != 0 {
		t.Fatalf("reserved slug reached the repository")
	}
}

// The public identity must not carry private contact details or the internal
// tenant UUID, even though the underlying record holds them.
func TestPublicTenantIdentityExcludesPrivateFields(t *testing.T) {
	repo := &slugLookupRepository{tenant: activeSlugTenant()}
	identity, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "acme-salon")
	if err != nil {
		t.Fatal(err)
	}

	// PublicTenantIdentity is the whole public contract. Enumerating its fields
	// makes any future addition — an ID, a contact detail — fail here loudly
	// rather than silently widening what unauthenticated callers can read.
	// BusinessType is the one deliberate F3 addition; onboarding_status and
	// onboarding_step must never appear here (see the dedicated test below).
	want := map[string]bool{"Slug": true, "Name": true, "Description": true, "Timezone": true, "BusinessType": true}
	structType := reflect.TypeOf(identity).Elem()
	if structType.NumField() != len(want) {
		t.Fatalf("PublicTenantIdentity has %d fields, want %d", structType.NumField(), len(want))
	}
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i).Name
		if !want[field] {
			t.Fatalf("PublicTenantIdentity exposes unexpected field %q", field)
		}
	}
}

func TestPublicTenantServicePropagatesRepositoryFailure(t *testing.T) {
	repo := &slugLookupRepository{err: errors.New("connection reset")}
	_, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "acme-salon")
	if err == nil {
		t.Fatal("GetBySlug() returned no error on repository failure")
	}
	// A raw driver failure must not be presented as a business outcome.
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.Code == apperrors.CodeTenantNotFound {
		t.Fatalf("repository failure surfaced as TENANT_NOT_FOUND: %v", err)
	}
}

var _ repository.TenantRepository = (*slugLookupRepository)(nil)
