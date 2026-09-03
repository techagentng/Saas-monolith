package app

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization"
	authzservice "github.com/techagentng/saas-monolith/internal/authorization/service"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These exercise the S11 booking-management routes through the exact
// production middleware chain app.New wires:
//
//	GET  /api/v1/tenants/{tenantID}/bookings                     booking.read
//	GET  /api/v1/tenants/{tenantID}/bookings/{bookingID}         booking.read
//	POST /api/v1/tenants/{tenantID}/bookings/{bookingID}/cancel  booking.update
//
// all: Authentication -> Tenant Context -> Authorization -> Handler, with the
// REAL TenantContextService, Authorizer and BookingManagementService.

var (
	bookingReadPermissions   = []string{"tenant.read", "booking.read"}
	bookingManagePermissions = []string{"tenant.read", "booking.read", "booking.update"}
)

const (
	bmRouteBookingA = "550e8400-e29b-41d4-a716-4466554f0001"
	bmRouteBookingB = "550e8400-e29b-41d4-a716-4466554f0002"
)

func bmRouteBooking(id, tenantID string, status schedulingmodel.BookingStatus, start time.Time) *schedulingmodel.Booking {
	phone := "+2348001112222"
	return &schedulingmodel.Booking{
		ID: id, TenantID: tenantID, ServiceID: staffRouteServiceA, StaffID: staffRouteStaffA,
		Customer: schedulingmodel.Customer{Name: "Jane Doe", Phone: &phone},
		StartAt:  start.UTC(), EndAt: start.Add(30 * time.Minute).UTC(), Status: status,
		CreatedAt: start.Add(-48 * time.Hour).UTC(), UpdatedAt: start.Add(-48 * time.Hour).UTC(),
	}
}

func buildBookingManagementRoutes(t *testing.T, scenario *staffScenarioState, tenantPermissions []string, bookings ...*schedulingmodel.Booking) (http.Handler, *identityservice.TokenManager, *statefulBookingRepository) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: &fakeSessionRepository{}}

	tenants := &statefulCatalogTenantRepository{tenant: scenario.tenant, otherTenant: scenario.otherTenant}
	memberships := &staffMembershipRepository{scenario: scenario}
	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	tenantMiddleware := tenant.Middleware{Resolver: contextService}
	authorizer := authzservice.NewAuthorizer(&fakeResolutionService{tenantPermissions: tenantPermissions})

	store := &statefulBookingRepository{
		bookings: bookings,
		services: scenario.services,
		profiles: scenario.profiles,
	}
	svc := schedulingservice.NewBookingManagementService(store, store, tenants, frozenClock{now: bmRouteNow})
	handler := schedulinghandler.NewBookingManagementHandler(svc)

	wrap := func(permission string, next http.HandlerFunc) http.Handler {
		return authMiddleware.Wrap(tenantMiddleware.Wrap(
			authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: permission}.Wrap(next),
		))
	}

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/tenants/{tenantID}/bookings", wrap("booking.read", func(w http.ResponseWriter, r *http.Request) {
		handler.List(w, r, r.PathValue("tenantID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/bookings/{bookingID}", wrap("booking.read", func(w http.ResponseWriter, r *http.Request) {
		handler.Get(w, r, r.PathValue("tenantID"), r.PathValue("bookingID"))
	}))
	mux.Handle("POST /api/v1/tenants/{tenantID}/bookings/{bookingID}/cancel", wrap("booking.update", func(w http.ResponseWriter, r *http.Request) {
		handler.Cancel(w, r, r.PathValue("tenantID"), r.PathValue("bookingID"))
	}))
	return mux, tokens, store
}

var bmRouteNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func bookingScenario(t *testing.T) *staffScenarioState {
	t.Helper()
	scenario := staffScenario()
	scenario.tenant.Timezone = strPtrApp("Africa/Lagos")
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	scenario.services[staffRouteServiceA] = &schedulingmodel.Service{
		ID: staffRouteServiceA, TenantID: staffRouteTenantA, Name: "Gel Manicure",
		DurationMinutes: 30, Status: schedulingmodel.StatusActive,
	}
	return scenario
}

func bookingsPath(tenantID string) string { return "/api/v1/tenants/" + tenantID + "/bookings" }

// --- auth + permission -------------------------------------------------

func TestBookingRoutesRequireAuthentication(t *testing.T) {
	handler, _, _ := buildBookingManagementRoutes(t, bookingScenario(t), bookingManagePermissions)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, bookingsPath(staffRouteTenantA)},
		{http.MethodGet, bookingsPath(staffRouteTenantA) + "/" + bmRouteBookingA},
		{http.MethodPost, bookingsPath(staffRouteTenantA) + "/" + bmRouteBookingA + "/cancel"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestBookingListRequiresBookingReadPermission(t *testing.T) {
	scenario := bookingScenario(t)
	handler, tokens, _ := buildBookingManagementRoutes(t, scenario, []string{"tenant.read"}) // no booking.*
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA), ""))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", rec.Code, rec.Body.String())
	}
	assertBodyCode(t, rec, "PERMISSION_DENIED")
}

// STAFF can read bookings but never cancel them.
func TestStaffRoleCanReadBookingsButNotCancel(t *testing.T) {
	scenario := bookingScenario(t)
	booking := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, store := buildBookingManagementRoutes(t, scenario, bookingReadPermissions, booking)

	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA), ""))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d for booking.read", listRec.Code)
	}

	cancelRec := httptest.NewRecorder()
	handler.ServeHTTP(cancelRec, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/cancel", ""))
	if cancelRec.Code != http.StatusForbidden {
		t.Fatalf("cancel status = %d, want 403 without booking.update", cancelRec.Code)
	}
	assertBodyCode(t, cancelRec, "PERMISSION_DENIED")
	if store.bookings[0].Status != schedulingmodel.BookingConfirmed {
		t.Fatal("a denied cancel still mutated the booking")
	}
}

// --- list ----------------------------------------------------------

func TestBookingListReturnsOnlyThisTenantsBookingsForTheView(t *testing.T) {
	scenario := bookingScenario(t)
	scenario.otherTenant = &tenantmodel.Tenant{ID: staffRouteTenantB, Name: "Rival", Slug: "rival", Status: tenantmodel.StatusActive}
	upcoming := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	otherTenant := bmRouteBooking(bmRouteBookingB, staffRouteTenantB, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, upcoming, otherTenant)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"?view=upcoming", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != bmRouteBookingA {
		t.Fatalf("rows = %v, want only this tenant's upcoming booking", rows)
	}
	if rows[0]["service"].(map[string]any)["name"] != "Gel Manicure" || rows[0]["staff"].(map[string]any)["name"] != "Ada" {
		t.Fatalf("row relations wrong: %v", rows[0])
	}
}

func TestBookingListCancelledView(t *testing.T) {
	scenario := bookingScenario(t)
	confirmed := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	cancelled := bmRouteBooking(bmRouteBookingB, staffRouteTenantA, schedulingmodel.BookingCancelled, bmRouteNow.Add(-24*time.Hour))
	handler, tokens, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, confirmed, cancelled)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"?view=cancelled", ""))
	var rows []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0]["id"] != bmRouteBookingB || rows[0]["status"] != "CANCELLED" {
		t.Fatalf("cancelled view = %v", rows)
	}
}

func TestBookingListRejectsInvalidView(t *testing.T) {
	handler, tokens, _ := buildBookingManagementRoutes(t, bookingScenario(t), bookingManagePermissions)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"?view=sideways", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertBodyCode(t, rec, "VALIDATION_FAILED")
}

// --- detail + isolation ------------------------------------------

func TestBookingDetailReturnsBooking(t *testing.T) {
	scenario := bookingScenario(t)
	booking := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, booking)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["timezone"] != "Africa/Lagos" || body["customer_phone"] != "+2348001112222" {
		t.Fatalf("detail = %v", body)
	}
}

func TestBookingDetailCrossTenantIsIndistinguishableFromMissing(t *testing.T) {
	scenario := bookingScenario(t)
	// booking exists, but under tenant B.
	elsewhere := bmRouteBooking(bmRouteBookingA, staffRouteTenantB, schedulingmodel.BookingConfirmed, bmRouteNow)
	handler, tokens, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, elsewhere)

	existsElsewhere := httptest.NewRecorder()
	handler.ServeHTTP(existsElsewhere, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA, ""))

	nonexistent := httptest.NewRecorder()
	handler.ServeHTTP(nonexistent, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"/550e8400-e29b-41d4-a716-4466554f9999", ""))

	if existsElsewhere.Code != http.StatusNotFound || nonexistent.Code != http.StatusNotFound {
		t.Fatalf("codes = %d / %d, want both 404", existsElsewhere.Code, nonexistent.Code)
	}
	if existsElsewhere.Body.String() != nonexistent.Body.String() {
		t.Fatalf("responses differ, disclosing cross-tenant existence:\n  %s\n  %s", existsElsewhere.Body.String(), nonexistent.Body.String())
	}
}

// --- cancel ------------------------------------------------------

func TestBookingCancelTransitionsAndKeepsTheRow(t *testing.T) {
	scenario := bookingScenario(t)
	booking := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, store := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, booking)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/cancel", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"CANCELLED"`) {
		t.Fatalf("cancel body = %s", rec.Body.String())
	}
	if len(store.bookings) != 1 || store.bookings[0].Status != schedulingmodel.BookingCancelled {
		t.Fatalf("row not preserved-and-cancelled: %+v", store.bookings)
	}

	// idempotent second cancel
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/cancel", ""))
	if rec2.Code != http.StatusOK {
		t.Fatalf("idempotent cancel status = %d", rec2.Code)
	}
}

func TestBookingCancelCrossTenantIsNotFound(t *testing.T) {
	scenario := bookingScenario(t)
	elsewhere := bmRouteBooking(bmRouteBookingA, staffRouteTenantB, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, store := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, elsewhere)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/cancel", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	assertBodyCode(t, rec, "BOOKING_NOT_FOUND")
	if store.bookings[0].Status != schedulingmodel.BookingConfirmed {
		t.Fatal("a cross-tenant cancel mutated another tenant's booking")
	}
}
