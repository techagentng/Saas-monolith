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
	return &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655441001", Name: "Acme Salon", Slug: "acme-salon",
		Status: model.StatusActive, Description: &description, Timezone: &timezone,
		ContactEmail: &email, ContactPhone: &phone,
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
// from a slug that was never registered.
func TestPublicTenantServiceHidesDisabledTenant(t *testing.T) {
	tenant := activeSlugTenant()
	tenant.Status = model.StatusDisabled
	repo := &slugLookupRepository{tenant: tenant}

	_, err := NewPublicTenantService(repo).GetBySlug(context.Background(), "acme-salon")
	assertPublicCode(t, err, apperrors.CodeTenantNotFound)
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
	want := map[string]bool{"Slug": true, "Name": true, "Description": true, "Timezone": true}
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
