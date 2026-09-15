package app

import (
	"context"
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
	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
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

// fakeRouteAvailability is a minimal AvailabilityService for this file's
// route tests. This file's own purpose (per the doc comment above) is
// proving HTTP/permission/tenant-isolation wiring, not scheduling
// correctness — the real S7 engine's own rules (working hours, capability,
// DST, the self-conflict exclusion) are proven by
// booking_management_service_test.go's unit tests and the real-database S12-BE
// integration test, so a controllable fake is the right level of
// abstraction here, exactly like fakeBookingService already is for the S10
// public-booking route tests in a different file.
type fakeRouteAvailability struct {
	result        *schedulingservice.AvailabilityResult
	err           error
	lastExcludeID string
	excludeCalls  int
}

func (f *fakeRouteAvailability) GetAvailability(context.Context, string, string, string, string) (*schedulingservice.AvailabilityResult, error) {
	return f.result, f.err
}

func (f *fakeRouteAvailability) GetAvailabilityExcludingBooking(_ context.Context, _ string, _ string, _ string, _ string, excludeBookingID string) (*schedulingservice.AvailabilityResult, error) {
	f.excludeCalls++
	f.lastExcludeID = excludeBookingID
	return f.result, f.err
}

func buildBookingManagementRoutes(t *testing.T, scenario *staffScenarioState, tenantPermissions []string, bookings ...*schedulingmodel.Booking) (http.Handler, *identityservice.TokenManager, *statefulBookingRepository, *fakeRouteAvailability) {
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
	availability := &fakeRouteAvailability{}
	services := &statefulServiceRepository{services: scenario.services}
	svc := schedulingservice.NewBookingManagementService(
		store, store, store, store,
		availability, services,
		tenants, frozenClock{now: bmRouteNow},
	)
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
	mux.Handle("POST /api/v1/tenants/{tenantID}/bookings/{bookingID}/reschedule", wrap("booking.update", func(w http.ResponseWriter, r *http.Request) {
		handler.Reschedule(w, r, r.PathValue("tenantID"), r.PathValue("bookingID"))
	}))
	mux.Handle("POST /api/v1/tenants/{tenantID}/bookings/{bookingID}/complete", wrap("booking.update", func(w http.ResponseWriter, r *http.Request) {
		handler.Complete(w, r, r.PathValue("tenantID"), r.PathValue("bookingID"))
	}))
	mux.Handle("POST /api/v1/tenants/{tenantID}/bookings/{bookingID}/no-show", wrap("booking.update", func(w http.ResponseWriter, r *http.Request) {
		handler.NoShow(w, r, r.PathValue("tenantID"), r.PathValue("bookingID"))
	}))
	return mux, tokens, store, availability
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
	handler, _, _, _ := buildBookingManagementRoutes(t, bookingScenario(t), bookingManagePermissions)
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
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, []string{"tenant.read"}) // no booking.*
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
	handler, tokens, store, _ := buildBookingManagementRoutes(t, scenario, bookingReadPermissions, booking)

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
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, upcoming, otherTenant)

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

// S13-BE: PAST must include COMPLETED and NO_SHOW bookings alongside past
// CONFIRMED ones, while CANCELLED — regardless of how far in the past it was
// scheduled for — stays out of PAST and only appears in its own view.
func TestBookingListPastViewIncludesCompletedAndNoShowButNotCancelled(t *testing.T) {
	scenario := bookingScenario(t)
	const bmRouteBookingC = "550e8400-e29b-41d4-a716-4466554f0003"
	const bmRouteBookingD = "550e8400-e29b-41d4-a716-4466554f0004"
	pastConfirmed := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(-48*time.Hour))
	completed := bmRouteBooking(bmRouteBookingB, staffRouteTenantA, schedulingmodel.BookingCompleted, bmRouteNow.Add(-24*time.Hour))
	noShow := bmRouteBooking(bmRouteBookingC, staffRouteTenantA, schedulingmodel.BookingNoShow, bmRouteNow.Add(-12*time.Hour))
	cancelled := bmRouteBooking(bmRouteBookingD, staffRouteTenantA, schedulingmodel.BookingCancelled, bmRouteNow.Add(-6*time.Hour))
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, pastConfirmed, completed, noShow, cancelled)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"?view=past", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range rows {
		ids[r["id"].(string)] = true
	}
	if len(rows) != 3 || !ids[bmRouteBookingA] || !ids[bmRouteBookingB] || !ids[bmRouteBookingC] || ids[bmRouteBookingD] {
		t.Fatalf("past view = %v, want CONFIRMED/COMPLETED/NO_SHOW present and CANCELLED excluded", rows)
	}
}

func TestBookingListCancelledView(t *testing.T) {
	scenario := bookingScenario(t)
	confirmed := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	cancelled := bmRouteBooking(bmRouteBookingB, staffRouteTenantA, schedulingmodel.BookingCancelled, bmRouteNow.Add(-24*time.Hour))
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, confirmed, cancelled)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"?view=cancelled", ""))
	var rows []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0]["id"] != bmRouteBookingB || rows[0]["status"] != "CANCELLED" {
		t.Fatalf("cancelled view = %v", rows)
	}
}

// The tenant here is Africa/Lagos (UTC+1) — deliberately so a date filter
// bug that used UTC calendar days instead of the tenant's would be visible:
// a booking at 2026-09-06 23:30 UTC is 2026-09-07 00:30 in Lagos, and must
// be INCLUDED by date=2026-09-07 despite falling on the previous UTC day.
func TestBookingListDateFilterUsesTheTenantsCalendarDayNotUTC(t *testing.T) {
	scenario := bookingScenario(t)
	sameLagosDay := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed,
		time.Date(2026, 9, 6, 23, 30, 0, 0, time.UTC)) // 2026-09-07 00:30 Africa/Lagos
	differentDay := bmRouteBooking(bmRouteBookingB, staffRouteTenantA, schedulingmodel.BookingConfirmed,
		time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)) // 2026-09-08 11:00 Africa/Lagos
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, sameLagosDay, differentDay)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"?view=all&date=2026-09-07", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != bmRouteBookingA {
		t.Fatalf("rows = %v, want only the booking that falls on 2026-09-07 Africa/Lagos", rows)
	}
}

func TestBookingListRejectsAnInvalidDateFilter(t *testing.T) {
	handler, tokens, _, _ := buildBookingManagementRoutes(t, bookingScenario(t), bookingManagePermissions)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"?view=all&date=07-09-2026", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
	assertBodyCode(t, rec, "VALIDATION_FAILED")
}

func TestBookingListFiltersByStaffAndService(t *testing.T) {
	scenario := bookingScenario(t)
	const otherStaff = "550e8400-e29b-41d4-a716-4466554f0099"
	scenario.profiles[otherStaff] = &schedulingmodel.StaffProfile{ID: otherStaff, TenantID: staffRouteTenantA, DisplayName: "Bola", IsBookable: true, Status: schedulingmodel.StatusActive}
	forA := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	forOther := bmRouteBooking(bmRouteBookingB, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(25*time.Hour))
	forOther.StaffID = otherStaff
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, forA, forOther)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"?view=all&staff_id="+staffRouteStaffA, ""))
	var rows []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0]["id"] != bmRouteBookingA {
		t.Fatalf("staff_id filter = %v, want only bookingA", rows)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, staffRequest(t, tokens, http.MethodGet, bookingsPath(staffRouteTenantA)+"?view=all&service_id="+staffRouteServiceA, ""))
	var rows2 []map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &rows2)
	if len(rows2) != 2 {
		t.Fatalf("service_id filter = %v, want both bookings (same service)", rows2)
	}
}

func TestBookingListRejectsInvalidView(t *testing.T) {
	handler, tokens, _, _ := buildBookingManagementRoutes(t, bookingScenario(t), bookingManagePermissions)
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
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, booking)

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
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, elsewhere)

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
	handler, tokens, store, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, booking)

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
	handler, tokens, store, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, elsewhere)

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

// --- reschedule ----------------------------------------------------

func rescheduleBody(date, start string) string {
	b, _ := json.Marshal(map[string]string{"date": date, "start": start})
	return string(b)
}

// TestBookingRescheduleMovesBookingAndReturnsUpdatedDetail proves the full
// HTTP wire-up: request body parsing, the handler asking the availability
// engine to EXCLUDE this booking's own id (the self-conflict fix), and the
// response reusing the exact same booking-detail DTO List/Get/Cancel use.
func TestBookingRescheduleMovesBookingAndReturnsUpdatedDetail(t *testing.T) {
	scenario := bookingScenario(t)
	original := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, store, avail := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, original)
	avail.result = &schedulingservice.AvailabilityResult{
		Date: "2026-09-12", Timezone: "Africa/Lagos", ServiceID: staffRouteServiceA, StaffID: staffRouteStaffA,
		Slots: []availability.Slot{{Start: "14:00", End: "14:30"}},
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost,
		bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/reschedule", rescheduleBody("2026-09-12", "14:00")))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if avail.excludeCalls != 1 || avail.lastExcludeID != bmRouteBookingA {
		t.Fatalf("excludeCalls = %d, lastExcludeID = %q, want 1 call excluding %q", avail.excludeCalls, avail.lastExcludeID, bmRouteBookingA)
	}
	lagos, _ := time.LoadLocation("Africa/Lagos")
	wantStart := time.Date(2026, 9, 12, 14, 0, 0, 0, lagos)
	if !store.bookings[0].StartAt.Equal(wantStart) {
		t.Fatalf("persisted start = %s, want %s", store.bookings[0].StartAt, wantStart)
	}
	if !strings.Contains(rec.Body.String(), `"id":"`+bmRouteBookingA+`"`) || !strings.Contains(rec.Body.String(), `"status":"CONFIRMED"`) {
		t.Fatalf("body = %s, want the same booking-detail DTO shape List/Get/Cancel use", rec.Body.String())
	}
}

func TestBookingRescheduleRequiresBookingUpdatePermission(t *testing.T) {
	scenario := bookingScenario(t)
	original := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, store, _ := buildBookingManagementRoutes(t, scenario, bookingReadPermissions, original)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost,
		bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/reschedule", rescheduleBody("2026-09-12", "14:00")))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 without booking.update", rec.Code)
	}
	assertBodyCode(t, rec, "PERMISSION_DENIED")
	if store.bookings[0].StartAt != original.StartAt {
		t.Fatal("a denied reschedule still mutated the booking")
	}
}

func TestBookingRescheduleCrossTenantIsNotFound(t *testing.T) {
	scenario := bookingScenario(t)
	elsewhere := bmRouteBooking(bmRouteBookingA, staffRouteTenantB, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, store, avail := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, elsewhere)
	avail.result = &schedulingservice.AvailabilityResult{Timezone: "Africa/Lagos", Slots: []availability.Slot{{Start: "14:00", End: "14:30"}}}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost,
		bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/reschedule", rescheduleBody("2026-09-12", "14:00")))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	assertBodyCode(t, rec, "BOOKING_NOT_FOUND")
	if store.bookings[0].StartAt != elsewhere.StartAt {
		t.Fatal("a cross-tenant reschedule mutated another tenant's booking")
	}
}

// TestBookingRescheduleRejectsATargetThatIsNotAnAvailableSlot proves the
// handler trusts the S7 availability engine as sole authority: a target that
// engine does not offer is BOOKING_SLOT_UNAVAILABLE (409), never a silent
// acceptance of an arbitrary client-supplied time.
func TestBookingRescheduleRejectsATargetThatIsNotAnAvailableSlot(t *testing.T) {
	scenario := bookingScenario(t)
	original := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, store, avail := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, original)
	avail.result = &schedulingservice.AvailabilityResult{Timezone: "Africa/Lagos", Slots: []availability.Slot{{Start: "09:00", End: "09:30"}}}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost,
		bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/reschedule", rescheduleBody("2026-09-12", "14:00")))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", rec.Code, rec.Body.String())
	}
	assertBodyCode(t, rec, "BOOKING_SLOT_UNAVAILABLE")
	if store.bookings[0].StartAt != original.StartAt {
		t.Fatal("a rejected reschedule still mutated the booking")
	}
}

// --- complete / no-show (S13-BE) ----------------------------------------

func TestBookingCompleteTransitionsAPastConfirmedBooking(t *testing.T) {
	scenario := bookingScenario(t)
	past := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(-2*time.Hour))
	handler, tokens, store, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, past)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/complete", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"COMPLETED"`) {
		t.Fatalf("complete body = %s", rec.Body.String())
	}
	if len(store.bookings) != 1 || store.bookings[0].Status != schedulingmodel.BookingCompleted {
		t.Fatalf("row not preserved-and-completed: %+v", store.bookings)
	}

	// idempotent second complete
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/complete", ""))
	if rec2.Code != http.StatusOK {
		t.Fatalf("idempotent complete status = %d", rec2.Code)
	}
}

func TestBookingNoShowTransitionsAPastConfirmedBooking(t *testing.T) {
	scenario := bookingScenario(t)
	past := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(-2*time.Hour))
	handler, tokens, store, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, past)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/no-show", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"NO_SHOW"`) {
		t.Fatalf("no-show body = %s", rec.Body.String())
	}
	if len(store.bookings) != 1 || store.bookings[0].Status != schedulingmodel.BookingNoShow {
		t.Fatalf("row not preserved-and-marked: %+v", store.bookings)
	}
}

func TestBookingCompleteRejectsAFutureConfirmedBooking(t *testing.T) {
	scenario := bookingScenario(t)
	future := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(2*time.Hour))
	handler, tokens, store, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, future)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/complete", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", rec.Code, rec.Body.String())
	}
	assertBodyCode(t, rec, "BOOKING_INVALID_TRANSITION")
	if store.bookings[0].Status != schedulingmodel.BookingConfirmed {
		t.Fatal("a rejected complete mutated the booking")
	}
}

func TestBookingCompleteAndNoShowRejectACancelledBooking(t *testing.T) {
	scenario := bookingScenario(t)
	cancelled := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingCancelled, bmRouteNow.Add(-2*time.Hour))
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, cancelled)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/complete", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	assertBodyCode(t, rec, "BOOKING_INVALID_TRANSITION")

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/no-show", ""))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec2.Code)
	}
	assertBodyCode(t, rec2, "BOOKING_INVALID_TRANSITION")
}

func TestBookingCompleteAndNoShowRequireBookingUpdatePermission(t *testing.T) {
	scenario := bookingScenario(t)
	past := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(-2*time.Hour))
	handler, tokens, store, _ := buildBookingManagementRoutes(t, scenario, bookingReadPermissions, past)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/complete", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 without booking.update", rec.Code)
	}
	assertBodyCode(t, rec, "PERMISSION_DENIED")
	if store.bookings[0].Status != schedulingmodel.BookingConfirmed {
		t.Fatal("a denied complete still mutated the booking")
	}
}

func TestBookingCompleteAndNoShowCrossTenantIsNotFound(t *testing.T) {
	scenario := bookingScenario(t)
	elsewhere := bmRouteBooking(bmRouteBookingA, staffRouteTenantB, schedulingmodel.BookingConfirmed, bmRouteNow.Add(-2*time.Hour))
	handler, tokens, store, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, elsewhere)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost, bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/complete", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	assertBodyCode(t, rec, "BOOKING_NOT_FOUND")
	if store.bookings[0].Status != schedulingmodel.BookingConfirmed {
		t.Fatal("a cross-tenant complete mutated another tenant's booking")
	}
}

func TestBookingRescheduleRejectsAMalformedBody(t *testing.T) {
	scenario := bookingScenario(t)
	original := bmRouteBooking(bmRouteBookingA, staffRouteTenantA, schedulingmodel.BookingConfirmed, bmRouteNow.Add(24*time.Hour))
	handler, tokens, _, _ := buildBookingManagementRoutes(t, scenario, bookingManagePermissions, original)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, staffRequest(t, tokens, http.MethodPost,
		bookingsPath(staffRouteTenantA)+"/"+bmRouteBookingA+"/reschedule", "{not json"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertBodyCode(t, rec, "INVALID_REQUEST")
}
