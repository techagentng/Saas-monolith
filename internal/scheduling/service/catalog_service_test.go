package service

import (
	"context"
	"errors"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

const (
	tenantA   = "550e8400-e29b-41d4-a716-446655441001"
	tenantB   = "550e8400-e29b-41d4-a716-446655441002"
	serviceID = "550e8400-e29b-41d4-a716-446655441003"
)

// --- fakes -------------------------------------------------------------------

// fakeServiceRepository records every call with the tenant it was scoped to,
// so tests can prove the service layer never addresses a service without
// naming its tenant.
type fakeServiceRepository struct {
	services map[string]*model.Service

	created      *model.Service
	createCalls  int
	updateCalls  int
	archiveCalls int
	lastFilter   repository.ServiceListFilter
	lastTenantID string

	createErr error
	findErr   error
	listErr   error
}

func newFakeServiceRepository() *fakeServiceRepository {
	return &fakeServiceRepository{services: map[string]*model.Service{}}
}

func (r *fakeServiceRepository) Create(_ context.Context, service *model.Service) (*model.Service, error) {
	r.createCalls++
	if r.createErr != nil {
		return nil, r.createErr
	}
	stored := *service
	if stored.Status == "" {
		stored.Status = model.StatusActive
	}
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	r.services[stored.ID] = &stored
	r.created = service
	return &stored, nil
}

func (r *fakeServiceRepository) FindByID(_ context.Context, tenantID string, id string) (*model.Service, error) {
	r.lastTenantID = tenantID
	if r.findErr != nil {
		return nil, r.findErr
	}
	stored, ok := r.services[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)
	}
	return stored, nil
}

func (r *fakeServiceRepository) ListByTenant(_ context.Context, tenantID string, filter repository.ServiceListFilter) ([]*model.Service, error) {
	r.lastTenantID = tenantID
	r.lastFilter = filter
	if r.listErr != nil {
		return nil, r.listErr
	}
	var result []*model.Service
	for _, stored := range r.services {
		if stored.TenantID != tenantID {
			continue
		}
		if filter.Status != nil && stored.Status != *filter.Status {
			continue
		}
		result = append(result, stored)
	}
	return result, nil
}

func (r *fakeServiceRepository) Update(_ context.Context, tenantID string, id string, update repository.ServiceUpdate) (*model.Service, error) {
	r.updateCalls++
	r.lastTenantID = tenantID
	stored, ok := r.services[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)
	}
	if update.Name != nil {
		stored.Name = *update.Name
	}
	if update.Description != nil {
		stored.Description = update.Description
	}
	if update.DurationMinutes != nil {
		stored.DurationMinutes = *update.DurationMinutes
	}
	if update.PriceMinor != nil {
		stored.PriceMinor = *update.PriceMinor
	}
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

func (r *fakeServiceRepository) Archive(_ context.Context, tenantID string, id string) (*model.Service, error) {
	r.archiveCalls++
	r.lastTenantID = tenantID
	stored, ok := r.services[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)
	}
	stored.Status = model.StatusArchived
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

type fakeTenantReader struct {
	tenant    *tenantmodel.Tenant
	findCalls int
	findErr   error
}

func (r *fakeTenantReader) FindByID(_ context.Context, id string) (*tenantmodel.Tenant, error) {
	r.findCalls++
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.tenant == nil || r.tenant.ID != id {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return r.tenant, nil
}

func tenantWithCurrency(code string) *fakeTenantReader {
	currency := code
	return &fakeTenantReader{tenant: &tenantmodel.Tenant{ID: tenantA, Name: "Acme Nails", Slug: "acme-nails", Status: tenantmodel.StatusActive, Currency: &currency}}
}

func tenantWithoutCurrency() *fakeTenantReader {
	return &fakeTenantReader{tenant: &tenantmodel.Tenant{ID: tenantA, Name: "Acme Nails", Slug: "acme-nails", Status: tenantmodel.StatusActive}}
}

func assertCode(t *testing.T, err error, want apperrors.ErrorCode, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != want {
		t.Fatalf("%s: error = %v, want %s", context, err, want)
	}
}

func validCreateInput() CreateServiceInput {
	return CreateServiceInput{Name: "Gel Manicure", DurationMinutes: 45, PriceMinor: 1999}
}

// --- Create ------------------------------------------------------------------

func TestCreateStoresValidatedServiceAsActive(t *testing.T) {
	services := newFakeServiceRepository()
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	description := "  Long-lasting gel finish.  "
	input := validCreateInput()
	input.Description = &description

	created, err := catalog.Create(context.Background(), tenantA, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE — a new service is always active and the client never chooses", created.Status)
	}
	if created.TenantID != tenantA {
		t.Fatalf("TenantID = %q, want the trusted tenant %q", created.TenantID, tenantA)
	}
	if created.ID == "" {
		t.Fatal("Create() did not assign an ID")
	}
	if created.Name != "Gel Manicure" {
		t.Fatalf("Name = %q, want the trimmed value", created.Name)
	}
	if created.Description == nil || *created.Description != "Long-lasting gel finish." {
		t.Fatalf("Description = %v, want the trimmed value", created.Description)
	}
	// The model handed to the repository must leave Status unset so the
	// repository's own defaulting applies — the same division of responsibility
	// PostgresTenantRepository.Create already uses for tenant status.
	if services.created.Status != "" {
		t.Fatalf("service passed to repository had Status %q preset, want it left to the repository default", services.created.Status)
	}
}

func TestCreateWithoutDescriptionStoresNil(t *testing.T) {
	services := newFakeServiceRepository()
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	created, err := catalog.Create(context.Background(), tenantA, validCreateInput())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Description != nil {
		t.Fatalf("Description = %v, want nil when never supplied", *created.Description)
	}
}

func TestCreateAllowsFreeService(t *testing.T) {
	services := newFakeServiceRepository()
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	input := validCreateInput()
	input.PriceMinor = 0

	created, err := catalog.Create(context.Background(), tenantA, input)
	if err != nil {
		t.Fatalf("Create() error = %v, want a zero-priced service accepted", err)
	}
	if created.PriceMinor != 0 {
		t.Fatalf("PriceMinor = %d, want 0", created.PriceMinor)
	}
}

func TestCreateRequiresTenantCurrency(t *testing.T) {
	services := newFakeServiceRepository()
	catalog := NewCatalogService(services, tenantWithoutCurrency())

	_, err := catalog.Create(context.Background(), tenantA, validCreateInput())
	assertCode(t, err, apperrors.CodeValidationFailed, "Create() without tenant currency")
	if services.createCalls != 0 {
		t.Fatal("Create() reached the repository despite the tenant having no currency")
	}
}

func TestCreateRejectsUnsupportedStoredCurrency(t *testing.T) {
	// Defensive: the column is only ever written through CurrencyService, which
	// validates — but a value that somehow became unsupported must not silently
	// denominate a real price.
	services := newFakeServiceRepository()
	catalog := NewCatalogService(services, tenantWithCurrency("JPY"))

	_, err := catalog.Create(context.Background(), tenantA, validCreateInput())
	assertCode(t, err, apperrors.CodeValidationFailed, "Create() with an unsupported stored currency")
	if services.createCalls != 0 {
		t.Fatal("Create() reached the repository with an unsupported currency")
	}
}

func TestCreateRejectsInvalidFieldsBeforeTouchingPersistence(t *testing.T) {
	tests := []struct {
		name  string
		input CreateServiceInput
	}{
		{"empty name", CreateServiceInput{Name: "   ", DurationMinutes: 45, PriceMinor: 1999}},
		{"zero duration", CreateServiceInput{Name: "Gel Manicure", DurationMinutes: 0, PriceMinor: 1999}},
		{"negative duration", CreateServiceInput{Name: "Gel Manicure", DurationMinutes: -30, PriceMinor: 1999}},
		{"duration over limit", CreateServiceInput{Name: "Gel Manicure", DurationMinutes: model.MaxDurationMinutes + 1, PriceMinor: 1999}},
		{"negative price", CreateServiceInput{Name: "Gel Manicure", DurationMinutes: 45, PriceMinor: -1}},
		{"price over limit", CreateServiceInput{Name: "Gel Manicure", DurationMinutes: 45, PriceMinor: model.MaxPriceMinor + 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			services := newFakeServiceRepository()
			tenants := tenantWithCurrency("NGN")
			catalog := NewCatalogService(services, tenants)

			_, err := catalog.Create(context.Background(), tenantA, test.input)
			assertCode(t, err, apperrors.CodeValidationFailed, "Create("+test.name+")")
			if services.createCalls != 0 {
				t.Fatal("Create() reached the repository with invalid input")
			}
			if tenants.findCalls != 0 {
				t.Fatal("Create() loaded the tenant before field validation — invalid input must fail on its own merits")
			}
		})
	}
}

func TestCreateRejectsMalformedTenantID(t *testing.T) {
	services := newFakeServiceRepository()
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	_, err := catalog.Create(context.Background(), "not-a-uuid", validCreateInput())
	assertCode(t, err, apperrors.CodeInvalidRequest, "Create(malformed tenant id)")
	if services.createCalls != 0 {
		t.Fatal("Create() reached the repository with a malformed tenant id")
	}
}

func TestCreatePropagatesRepositoryFailure(t *testing.T) {
	services := newFakeServiceRepository()
	services.createErr = errors.New("connection reset")
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	_, err := catalog.Create(context.Background(), tenantA, validCreateInput())
	if err == nil {
		t.Fatal("Create() swallowed a repository failure")
	}
	// An infrastructure failure must not masquerade as a business outcome: it
	// carries no known code, so errors.Map turns it into INTERNAL_ERROR.
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		t.Fatalf("repository failure surfaced as a business error %q, want an opaque system failure", appErr.Code)
	}
	if mapped := apperrors.Map(err); mapped.Code != apperrors.CodeInternalError {
		t.Fatalf("mapped code = %s, want INTERNAL_ERROR", mapped.Code)
	}
}

// --- Get ---------------------------------------------------------------------

func TestGetScopesLookupToTheTenant(t *testing.T) {
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", Status: model.StatusActive}
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	found, err := catalog.Get(context.Background(), tenantA, serviceID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if found.ID != serviceID {
		t.Fatalf("Get() = %q, want %q", found.ID, serviceID)
	}
	if services.lastTenantID != tenantA {
		t.Fatalf("repository was called with tenant %q, want %q — every lookup must be tenant-scoped", services.lastTenantID, tenantA)
	}
}

func TestGetTreatsAnotherTenantsServiceAsNotFound(t *testing.T) {
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", Status: model.StatusActive}
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	// Tenant B asks for a service ID that genuinely exists, under tenant A.
	_, err := catalog.Get(context.Background(), tenantB, serviceID)
	assertCode(t, err, apperrors.CodeServiceNotFound, "Get(cross-tenant)")
}

func TestGetRejectsMalformedIdentifiers(t *testing.T) {
	catalog := NewCatalogService(newFakeServiceRepository(), tenantWithCurrency("NGN"))

	if _, err := catalog.Get(context.Background(), "not-a-uuid", serviceID); err == nil {
		t.Fatal("Get() accepted a malformed tenant id")
	} else {
		assertCode(t, err, apperrors.CodeInvalidRequest, "Get(malformed tenant id)")
	}
	if _, err := catalog.Get(context.Background(), tenantA, "not-a-uuid"); err == nil {
		t.Fatal("Get() accepted a malformed service id")
	} else {
		assertCode(t, err, apperrors.CodeInvalidRequest, "Get(malformed service id)")
	}
}

// --- List --------------------------------------------------------------------

func TestListDefaultsToActiveOnly(t *testing.T) {
	services := newFakeServiceRepository()
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	if _, err := catalog.List(context.Background(), tenantA, ""); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if services.lastFilter.Status == nil || *services.lastFilter.Status != model.StatusActive {
		t.Fatalf("filter = %v, want ACTIVE by default", services.lastFilter.Status)
	}
}

func TestListStatusFilters(t *testing.T) {
	tests := []struct {
		raw       string
		wantAll   bool
		wantValue model.Status
	}{
		{"ACTIVE", false, model.StatusActive},
		{"ARCHIVED", false, model.StatusArchived},
		{"ALL", true, ""},
	}

	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			services := newFakeServiceRepository()
			catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

			if _, err := catalog.List(context.Background(), tenantA, test.raw); err != nil {
				t.Fatalf("List(%q) error = %v", test.raw, err)
			}
			if test.wantAll {
				if services.lastFilter.Status != nil {
					t.Fatalf("filter = %v, want every status for ALL", *services.lastFilter.Status)
				}
				return
			}
			if services.lastFilter.Status == nil || *services.lastFilter.Status != test.wantValue {
				t.Fatalf("filter = %v, want %q", services.lastFilter.Status, test.wantValue)
			}
		})
	}
}

func TestListRejectsUnknownStatusFilter(t *testing.T) {
	// A typo must surface rather than silently return the default catalog.
	catalog := NewCatalogService(newFakeServiceRepository(), tenantWithCurrency("NGN"))

	_, err := catalog.List(context.Background(), tenantA, "active")
	assertCode(t, err, apperrors.CodeValidationFailed, "List(unknown filter)")
}

func TestListIsScopedToTheTenant(t *testing.T) {
	services := newFakeServiceRepository()
	services.services["a"] = &model.Service{ID: "a", TenantID: tenantA, Name: "A", Status: model.StatusActive}
	services.services["b"] = &model.Service{ID: "b", TenantID: tenantB, Name: "B", Status: model.StatusActive}
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	result, err := catalog.List(context.Background(), tenantA, "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result) != 1 || result[0].TenantID != tenantA {
		t.Fatalf("List() returned %d rows including another tenant's, want only tenant A's", len(result))
	}
}

// --- Update ------------------------------------------------------------------

func TestUpdateAppliesOnlySuppliedFields(t *testing.T) {
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", DurationMinutes: 45, PriceMinor: 1999, Status: model.StatusActive}
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	name := "  Gel Manicure Deluxe  "
	updated, err := catalog.Update(context.Background(), tenantA, serviceID, UpdateServiceInput{Name: &name})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "Gel Manicure Deluxe" {
		t.Fatalf("Name = %q, want the trimmed value", updated.Name)
	}
	if updated.DurationMinutes != 45 || updated.PriceMinor != 1999 {
		t.Fatalf("Update() changed a field that was not supplied: duration=%d price=%d", updated.DurationMinutes, updated.PriceMinor)
	}
	if updated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want it untouched by an update — archiving owns lifecycle", updated.Status)
	}
}

func TestUpdateRejectsAnEmptyPatch(t *testing.T) {
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", Status: model.StatusActive}
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	_, err := catalog.Update(context.Background(), tenantA, serviceID, UpdateServiceInput{})
	assertCode(t, err, apperrors.CodeValidationFailed, "Update(empty patch)")
	if services.updateCalls != 0 {
		t.Fatal("Update() reached the repository with nothing to write")
	}
}

func TestUpdateValidatesEachSuppliedField(t *testing.T) {
	emptyName := "  "
	zeroDuration := 0
	negativePrice := int64(-1)

	tests := []struct {
		name  string
		input UpdateServiceInput
	}{
		{"empty name", UpdateServiceInput{Name: &emptyName}},
		{"zero duration", UpdateServiceInput{DurationMinutes: &zeroDuration}},
		{"negative price", UpdateServiceInput{PriceMinor: &negativePrice}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			services := newFakeServiceRepository()
			services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", Status: model.StatusActive}
			catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

			_, err := catalog.Update(context.Background(), tenantA, serviceID, test.input)
			assertCode(t, err, apperrors.CodeValidationFailed, "Update("+test.name+")")
			if services.updateCalls != 0 {
				t.Fatal("Update() reached the repository with invalid input")
			}
		})
	}
}

func TestUpdatingPriceRequiresTenantCurrency(t *testing.T) {
	// An edit must not be able to bypass a check a creation could not.
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", PriceMinor: 1999, Status: model.StatusActive}
	catalog := NewCatalogService(services, tenantWithoutCurrency())

	price := int64(2999)
	_, err := catalog.Update(context.Background(), tenantA, serviceID, UpdateServiceInput{PriceMinor: &price})
	assertCode(t, err, apperrors.CodeValidationFailed, "Update(price without tenant currency)")
	if services.updateCalls != 0 {
		t.Fatal("Update() wrote a price with no currency to denominate it")
	}
}

func TestUpdateWithoutPriceDoesNotRequireCurrency(t *testing.T) {
	// Renaming a service on a tenant that has not yet declared a currency is
	// legitimate — the currency prerequisite guards pricing, not editing.
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", Status: model.StatusActive}
	tenants := tenantWithoutCurrency()
	catalog := NewCatalogService(services, tenants)

	name := "Classic Manicure"
	if _, err := catalog.Update(context.Background(), tenantA, serviceID, UpdateServiceInput{Name: &name}); err != nil {
		t.Fatalf("Update() error = %v, want a non-price edit accepted without a currency", err)
	}
	if tenants.findCalls != 0 {
		t.Fatal("Update() loaded the tenant for an edit that did not touch price")
	}
}

func TestUpdateTreatsAnotherTenantsServiceAsNotFound(t *testing.T) {
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", Status: model.StatusActive}
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	name := "Hijacked"
	_, err := catalog.Update(context.Background(), tenantB, serviceID, UpdateServiceInput{Name: &name})
	assertCode(t, err, apperrors.CodeServiceNotFound, "Update(cross-tenant)")
	if services.services[serviceID].Name != "Gel Manicure" {
		t.Fatalf("a cross-tenant update mutated the service: %q", services.services[serviceID].Name)
	}
}

// --- Archive -----------------------------------------------------------------

func TestArchiveMovesActiveServiceToArchived(t *testing.T) {
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", Status: model.StatusActive}
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	archived, err := catalog.Archive(context.Background(), tenantA, serviceID)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if archived.Status != model.StatusArchived {
		t.Fatalf("Status = %q, want ARCHIVED", archived.Status)
	}
	if services.archiveCalls != 1 {
		t.Fatalf("Archive repository calls = %d, want 1", services.archiveCalls)
	}
	// The row survives: appointments will hold a foreign key to it from S10.
	if _, stillPresent := services.services[serviceID]; !stillPresent {
		t.Fatal("Archive() removed the row — archiving must never delete")
	}
}

func TestArchiveIsIdempotentAndDoesNotRewriteAnArchivedService(t *testing.T) {
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", Status: model.StatusArchived}
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	archived, err := catalog.Archive(context.Background(), tenantA, serviceID)
	if err != nil {
		t.Fatalf("Archive() error = %v, want idempotent success", err)
	}
	if archived.Status != model.StatusArchived {
		t.Fatalf("Status = %q, want ARCHIVED", archived.Status)
	}
	if services.archiveCalls != 0 {
		t.Fatal("Archive() re-persisted an already archived service — a repeated call must not disturb updated_at")
	}
}

func TestArchiveTreatsAnotherTenantsServiceAsNotFound(t *testing.T) {
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantA, Name: "Gel Manicure", Status: model.StatusActive}
	catalog := NewCatalogService(services, tenantWithCurrency("NGN"))

	_, err := catalog.Archive(context.Background(), tenantB, serviceID)
	assertCode(t, err, apperrors.CodeServiceNotFound, "Archive(cross-tenant)")
	if services.services[serviceID].Status != model.StatusActive {
		t.Fatalf("a cross-tenant archive mutated the service: %q", services.services[serviceID].Status)
	}
	if services.archiveCalls != 0 {
		t.Fatal("Archive() reached the repository write path on a cross-tenant request")
	}
}

func TestParseStatusFilterIsTheSingleFilterVocabulary(t *testing.T) {
	if _, err := ParseStatusFilter("ALL"); err != nil {
		t.Fatalf("ParseStatusFilter(ALL) error = %v", err)
	}
	if _, err := ParseStatusFilter("DISABLED"); err == nil {
		t.Fatal("ParseStatusFilter accepted DISABLED — the catalog vocabulary is ACTIVE/ARCHIVED")
	}
}
