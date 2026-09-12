package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/receipt"
	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These exercise the Scheduling S10 public booking route as app.New registers
// it — a bare mux entry, the REAL BookingService over the REAL S7
// AvailabilityService and PublicTenantService, NO auth/tenant/permission
// middleware. The in-memory booking store also backs the S7 OccupancyReader,
// so "a persisted booking removes availability" is genuinely exercised through
// the production slot engine.

// statefulBookingRepository is an in-memory BookingRepository that enforces the
// same no-overlap rule the real bookings_no_overlap exclusion constraint does,
// serves the S7 occupancy query, and (for S11) lists / finds / cancels. It is
// safe for concurrent use. services/profiles are optional lookup tables the
// S11 route tests wire so the list rows carry real names; S10 tests leave them
// nil and never call the list/find/cancel paths.
type statefulBookingRepository struct {
	mu       sync.Mutex
	bookings []*schedulingmodel.Booking
	services map[string]*schedulingmodel.Service
	profiles map[string]*schedulingmodel.StaffProfile
}

func (r *statefulBookingRepository) Create(_ context.Context, b *schedulingmodel.Booking) (*schedulingmodel.Booking, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.bookings {
		if e.StaffID == b.StaffID && e.Status == schedulingmodel.BookingConfirmed &&
			b.StartAt.Before(e.EndAt) && b.EndAt.After(e.StartAt) {
			return nil, apperrors.New(apperrors.CodeBookingSlotUnavailable, "the requested time is no longer available", nil)
		}
	}
	stored := *b
	if stored.Status == "" {
		stored.Status = schedulingmodel.BookingConfirmed
	}
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	r.bookings = append(r.bookings, &stored)
	return &stored, nil
}

func (r *statefulBookingRepository) OccupiedIntervals(_ context.Context, tenantID, staffID string, from, to time.Time) ([]availability.OccupiedInterval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []availability.OccupiedInterval{}
	for _, e := range r.bookings {
		if e.TenantID == tenantID && e.StaffID == staffID && e.Status == schedulingmodel.BookingConfirmed &&
			e.StartAt.Before(to) && e.EndAt.After(from) {
			out = append(out, availability.OccupiedInterval{Start: e.StartAt, End: e.EndAt})
		}
	}
	return out, nil
}

func (r *statefulBookingRepository) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bookings)
}

func (r *statefulBookingRepository) withRelations(b *schedulingmodel.Booking) *schedulingrepository.BookingWithRelations {
	out := &schedulingrepository.BookingWithRelations{Booking: *b}
	if svc := r.services[b.ServiceID]; svc != nil {
		out.ServiceName, out.ServiceDurationMins = svc.Name, svc.DurationMinutes
	}
	if st := r.profiles[b.StaffID]; st != nil {
		out.StaffName = st.DisplayName
	}
	return out
}

func (r *statefulBookingRepository) ListByTenant(_ context.Context, tenantID string, filter schedulingrepository.BookingListFilter) ([]*schedulingrepository.BookingWithRelations, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*schedulingrepository.BookingWithRelations
	for _, b := range r.bookings {
		if b.TenantID != tenantID {
			continue
		}
		if filter.Status != nil && b.Status != *filter.Status {
			continue
		}
		switch filter.Window {
		case schedulingrepository.BookingWindowUpcoming:
			if b.StartAt.Before(filter.Now) {
				continue
			}
		case schedulingrepository.BookingWindowPast:
			if !b.StartAt.Before(filter.Now) {
				continue
			}
		}
		if filter.StaffID != nil && b.StaffID != *filter.StaffID {
			continue
		}
		if filter.ServiceID != nil && b.ServiceID != *filter.ServiceID {
			continue
		}
		if filter.StartAtFrom != nil && b.StartAt.Before(*filter.StartAtFrom) {
			continue
		}
		if filter.StartAtTo != nil && !b.StartAt.Before(*filter.StartAtTo) {
			continue
		}
		out = append(out, r.withRelations(b))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Booking.StartAt.Before(out[j].Booking.StartAt) })
	return out, nil
}

func (r *statefulBookingRepository) FindByTenantAndID(_ context.Context, tenantID, bookingID string) (*schedulingrepository.BookingWithRelations, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.bookings {
		if b.ID == bookingID && b.TenantID == tenantID {
			return r.withRelations(b), nil
		}
	}
	return nil, apperrors.New(apperrors.CodeBookingNotFound, "booking not found", nil)
}

func (r *statefulBookingRepository) FindByTenantAndReceiptToken(_ context.Context, tenantID, token string) (*schedulingmodel.Booking, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.bookings {
		if b.ReceiptAccessToken == token && b.TenantID == tenantID {
			return b, nil
		}
	}
	return nil, apperrors.New(apperrors.CodeBookingNotFound, "booking not found", nil)
}

func (r *statefulBookingRepository) Cancel(_ context.Context, tenantID, bookingID string) (*schedulingmodel.Booking, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.bookings {
		if b.ID == bookingID && b.TenantID == tenantID && b.Status == schedulingmodel.BookingConfirmed {
			b.Status = schedulingmodel.BookingCancelled
			b.UpdatedAt = time.Now().UTC()
			copied := *b
			return &copied, true, nil
		}
	}
	return nil, false, nil
}

var (
	_ schedulingrepository.BookingRepository = (*statefulBookingRepository)(nil)
	_ schedulingservice.BookingCreator       = (*statefulBookingRepository)(nil)
	_ schedulingservice.OccupancyReader      = (*statefulBookingRepository)(nil)
)

func buildPublicBookingRoutes(f *s9Fixture) (http.Handler, *s9TenantRepo, *statefulBookingRepository) {
	tenantRepo := &s9TenantRepo{tenant: f.tenant}
	publicTenant := tenantservice.NewPublicTenantService(tenantRepo)

	serviceStore := &statefulServiceRepository{services: f.services}
	staffStore := &statefulStaffRepository{profiles: f.profiles}
	capStore := &statefulCapabilityRepository{assignments: f.capabilities}
	hoursStore := &statefulWorkingHoursRepository{byStaff: f.hours}
	bookingStore := &statefulBookingRepository{}

	engine := schedulingservice.NewAvailabilityService(
		tenantRepo, serviceStore, staffStore, capStore, hoursStore,
		bookingStore, // the S7 OccupancyReader seam, now live
		frozenClock{now: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
	)
	bookingSvc := schedulingservice.NewBookingService(publicTenant, engine, serviceStore, staffStore, bookingStore)
	availabilitySvc := schedulingservice.NewPublicAvailabilityService(publicTenant, engine)
	receiptSvc := schedulingservice.NewBookingReceiptService(publicTenant, bookingStore, serviceStore, staffStore, &fakeRouteReceiptGenerator{})

	bookingHandler := schedulinghandler.NewPublicBookingHandler(bookingSvc)
	availabilityHandler := schedulinghandler.NewPublicAvailabilityHandler(availabilitySvc)
	receiptHandler := schedulinghandler.NewPublicBookingReceiptHandler(receiptSvc)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/public/tenants/{slug}/bookings", func(w http.ResponseWriter, r *http.Request) {
		bookingHandler.Create(w, r, r.PathValue("slug"))
	})
	mux.HandleFunc("GET /api/v1/public/tenants/{slug}/availability", func(w http.ResponseWriter, r *http.Request) {
		availabilityHandler.Get(w, r, r.PathValue("slug"))
	})
	mux.HandleFunc("GET /api/v1/public/tenants/{slug}/bookings/{reference}/receipt", func(w http.ResponseWriter, r *http.Request) {
		receiptHandler.Get(w, r, r.PathValue("slug"), r.PathValue("reference"))
	})
	return mux, tenantRepo, bookingStore
}

// fakeRouteReceiptGenerator stands in for the real receipt.Generator (which
// would otherwise pull in go-pdf/fpdf here) — these route tests assert HTTP
// wiring, status codes, and access control, never PDF byte content, which
// internal/scheduling/receipt's own tests already cover.
type fakeRouteReceiptGenerator struct{}

func (fakeRouteReceiptGenerator) Generate(context.Context, receipt.Data) ([]byte, error) {
	return []byte("%PDF-route-test"), nil
}

func bookingBody(serviceID, staffID, date, start string) string {
	return `{"service_id":"` + serviceID + `","staff_id":"` + staffID + `","date":"` + date + `","start":"` + start +
		`","customer":{"name":"Jane Doe","phone":"+2348001112222"}}`
}

func postBooking(t *testing.T, handler http.Handler, slug, body string, withGarbageToken bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/tenants/"+slug+"/bookings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if withGarbageToken {
		req.Header.Set("Authorization", "Bearer not-a-real-token")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func slotStartsForAvailability(t *testing.T, handler http.Handler, slug, serviceID, staffID, date string) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, s9AvailPath(slug, serviceID, staffID, date), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("availability status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body s9AvailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(body.Slots))
	for i, s := range body.Slots {
		out[i] = s.Start
	}
	return out
}

// --- happy path + auth independence -----------------------------------

func TestPublicBookingRouteCreatesAnAppointmentAnonymously(t *testing.T) {
	handler, _, store := buildPublicBookingRoutes(s9NailFixture())

	rec := postBooking(t, handler, "glamour-nails", bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "09:30"), true) // garbage token present

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", rec.Code, rec.Body.String())
	}
	if store.count() != 1 {
		t.Fatalf("persisted bookings = %d, want 1", store.count())
	}

	var body struct {
		Booking struct {
			ID                         string                    `json:"id"`
			Reference                  string                    `json:"reference"`
			Status                     string                    `json:"status"`
			Service                    struct{ ID, Name string } `json:"service"`
			Staff                      struct{ ID, Name string } `json:"staff"`
			Date, Start, End, Timezone string
		} `json:"booking"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, rec.Body.String())
	}
	b := body.Booking
	if b.Status != "CONFIRMED" || b.Service.Name != "Gel Manicure" || b.Staff.Name != "Ada" {
		t.Fatalf("booking = %+v", b)
	}
	if b.Date != s9MondayDate || b.Start != "09:30" || b.End != "10:00" || b.Timezone != "Africa/Lagos" {
		t.Fatalf("schedule fields = %+v", b)
	}
	if !strings.HasPrefix(b.Reference, "NB-") {
		t.Fatalf("reference = %q", b.Reference)
	}
	// No internal fields, no customer PII echoed back.
	for _, forbidden := range []string{"tenant_id", "customer", "Jane Doe", "2348001112222", "start_at", "created_at", s9TenantAID} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, rec.Body.String())
		}
	}
}

// --- the whole point: a booking removes S9 availability ---------------

func TestPublicBookingRemovesTheSlotFromAvailabilityAndLeavesTouchingSlots(t *testing.T) {
	handler, _, _ := buildPublicBookingRoutes(s9NailFixture()) // MONDAY 09:00-12:00, 30-min service

	before := slotStartsForAvailability(t, handler, "glamour-nails", s9ServiceA, s9StaffA, s9MondayDate)
	if strings.Join(before, ",") != "09:00,09:30,10:00,10:30,11:00,11:30" {
		t.Fatalf("before = %v", before)
	}

	rec := postBooking(t, handler, "glamour-nails", bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "10:00"), false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("booking status = %d, body = %s", rec.Code, rec.Body.String())
	}

	after := slotStartsForAvailability(t, handler, "glamour-nails", s9ServiceA, s9StaffA, s9MondayDate)
	// 10:00-10:30 is gone. 09:30-10:00 (ends exactly as the booking starts) and
	// 10:30-11:00 (starts exactly as the booking ends) both survive — the
	// touching-boundary rule.
	if strings.Join(after, ",") != "09:00,09:30,10:30,11:00,11:30" {
		t.Fatalf("after = %v, want the 10:00 slot gone and 09:30 / 10:30 kept", after)
	}
}

// --- sequential conflict → 409 --------------------------------------

func TestPublicBookingSequentialConflictReturns409(t *testing.T) {
	handler, _, store := buildPublicBookingRoutes(s9NailFixture())

	first := postBooking(t, handler, "glamour-nails", bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "10:00"), false)
	if first.Code != http.StatusCreated {
		t.Fatalf("first booking status = %d", first.Code)
	}

	second := postBooking(t, handler, "glamour-nails", bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "10:00"), false)
	if second.Code != http.StatusConflict {
		t.Fatalf("second booking status = %d, body = %s, want 409", second.Code, second.Body.String())
	}
	assertBodyCode(t, second, "BOOKING_SLOT_UNAVAILABLE")
	if !strings.Contains(second.Body.String(), "no longer available") {
		t.Fatalf("409 body = %s, want the friendly message", second.Body.String())
	}
	if store.count() != 1 {
		t.Fatalf("bookings = %d, want exactly 1 after a conflict", store.count())
	}
}

// --- validation / gate cases ---------------------------------------

func TestPublicBookingRouteRejects(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*s9Fixture)
		slug       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name: "hidden tenant", slug: "glamour-nails",
			mutate:     func(f *s9Fixture) { f.tenant.OnboardingStatus = tenantmodel.OnboardingStatusInProgress },
			body:       bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "09:30"),
			wantStatus: http.StatusNotFound, wantCode: "TENANT_NOT_FOUND",
		},
		{
			name: "non-nail vertical", slug: "glamour-nails",
			mutate:     func(f *s9Fixture) { bt := tenantmodel.BusinessTypeHotel; f.tenant.BusinessType = &bt },
			body:       bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "09:30"),
			wantStatus: http.StatusNotFound, wantCode: "RESOURCE_NOT_FOUND",
		},
		{
			name: "cross-tenant service", slug: "glamour-nails",
			body:       bookingBody(s9ServiceB, s9StaffA, s9MondayDate, "09:30"),
			wantStatus: http.StatusNotFound, wantCode: "SERVICE_NOT_FOUND",
		},
		{
			name: "cross-tenant staff", slug: "glamour-nails",
			body:       bookingBody(s9ServiceA, s9StaffB, s9MondayDate, "09:30"),
			wantStatus: http.StatusNotFound, wantCode: "STAFF_NOT_FOUND",
		},
		{
			name: "incapable staff", slug: "glamour-nails",
			mutate:     func(f *s9Fixture) { f.capabilities = map[string][]string{} },
			body:       bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "09:30"),
			wantStatus: http.StatusBadRequest, wantCode: "VALIDATION_FAILED",
		},
		{
			name: "archived service", slug: "glamour-nails",
			mutate:     func(f *s9Fixture) { f.services[s9ServiceA].Status = schedulingmodel.StatusArchived },
			body:       bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "09:30"),
			wantStatus: http.StatusNotFound, wantCode: "SERVICE_NOT_FOUND",
		},
		{
			name: "start outside working hours", slug: "glamour-nails",
			body:       bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "14:00"),
			wantStatus: http.StatusConflict, wantCode: "BOOKING_SLOT_UNAVAILABLE",
		},
		{
			name: "start bridges a split-shift gap", slug: "glamour-nails",
			mutate: func(f *s9Fixture) {
				f.hours[s9StaffA] = []*schedulingmodel.WorkingHourInterval{
					{TenantID: s9TenantAID, StaffID: s9StaffA, DayOfWeek: schedulingmodel.Monday, StartTime: "09:00", EndTime: "12:00"},
					{TenantID: s9TenantAID, StaffID: s9StaffA, DayOfWeek: schedulingmodel.Monday, StartTime: "13:00", EndTime: "17:00"},
				}
			},
			body:       bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "11:45"), // 11:45-12:15 crosses the 12:00 close
			wantStatus: http.StatusConflict, wantCode: "BOOKING_SLOT_UNAVAILABLE",
		},
		{
			name: "malformed date", slug: "glamour-nails",
			body:       bookingBody(s9ServiceA, s9StaffA, "2026-9-7", "09:30"),
			wantStatus: http.StatusBadRequest, wantCode: "VALIDATION_FAILED",
		},
		{
			name: "malformed start", slug: "glamour-nails",
			body:       bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "9am"),
			wantStatus: http.StatusBadRequest, wantCode: "VALIDATION_FAILED",
		},
		{
			name: "missing customer name", slug: "glamour-nails",
			body:       `{"service_id":"` + s9ServiceA + `","staff_id":"` + s9StaffA + `","date":"` + s9MondayDate + `","start":"09:30","customer":{"phone":"x"}}`,
			wantStatus: http.StatusBadRequest, wantCode: "VALIDATION_FAILED",
		},
		{
			name: "unknown slug", slug: "no-such-salon",
			body:       bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "09:30"),
			wantStatus: http.StatusNotFound, wantCode: "TENANT_NOT_FOUND",
		},
		{
			name: "reserved slug", slug: "admin",
			body:       bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "09:30"),
			wantStatus: http.StatusNotFound, wantCode: "TENANT_NOT_FOUND",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := s9NailFixture()
			if tc.mutate != nil {
				tc.mutate(f)
			}
			handler, _, store := buildPublicBookingRoutes(f)

			rec := postBooking(t, handler, tc.slug, tc.body, false)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, body = %s, want %d", rec.Code, rec.Body.String(), tc.wantStatus)
			}
			assertBodyCode(t, rec, tc.wantCode)
			if store.count() != 0 {
				t.Fatalf("a rejected booking was persisted: %d rows", store.count())
			}
		})
	}
}

// --- S12-BE: public receipt route ---------------------------------------

type createdBookingBody struct {
	Booking struct {
		Reference    string `json:"reference"`
		ReceiptToken string `json:"receipt_token"`
	} `json:"booking"`
}

func createBookingAndExtractReceiptCredentials(t *testing.T, handler http.Handler) (reference, token string) {
	t.Helper()
	rec := postBooking(t, handler, "glamour-nails", bookingBody(s9ServiceA, s9StaffA, s9MondayDate, "09:30"), false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("booking creation status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body createdBookingBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Booking.ReceiptToken == "" {
		t.Fatal("booking creation response has no receipt_token")
	}
	return body.Booking.Reference, body.Booking.ReceiptToken
}

func getReceipt(handler http.Handler, slug, reference, token string) *httptest.ResponseRecorder {
	path := "/api/v1/public/tenants/" + slug + "/bookings/" + reference + "/receipt"
	if token != "" {
		path += "?token=" + token
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestPublicBookingReceiptRouteEndToEnd(t *testing.T) {
	handler, _, _ := buildPublicBookingRoutes(s9NailFixture())
	reference, token := createBookingAndExtractReceiptCredentials(t, handler)

	rec := getReceipt(handler, "glamour-nails", reference, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="booking-`+reference+`.pdf"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("receipt body is empty")
	}
}

func TestPublicBookingReceiptRouteRejectsAWrongToken(t *testing.T) {
	handler, _, _ := buildPublicBookingRoutes(s9NailFixture())
	reference, _ := createBookingAndExtractReceiptCredentials(t, handler)

	rec := getReceipt(handler, "glamour-nails", reference, "totally-wrong-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", rec.Code, rec.Body.String())
	}
	assertBodyCode(t, rec, "BOOKING_NOT_FOUND")
}

func TestPublicBookingReceiptRouteRejectsAMissingToken(t *testing.T) {
	handler, _, _ := buildPublicBookingRoutes(s9NailFixture())
	reference, _ := createBookingAndExtractReceiptCredentials(t, handler)

	rec := getReceipt(handler, "glamour-nails", reference, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
	assertBodyCode(t, rec, "VALIDATION_FAILED")
}

func TestPublicBookingReceiptRouteRejectsAMismatchedReference(t *testing.T) {
	handler, _, _ := buildPublicBookingRoutes(s9NailFixture())
	_, token := createBookingAndExtractReceiptCredentials(t, handler)

	rec := getReceipt(handler, "glamour-nails", "NB-DEADBEEF", token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", rec.Code, rec.Body.String())
	}
	assertBodyCode(t, rec, "BOOKING_NOT_FOUND")
}

// THE TENANT-ISOLATION PROOF: a booking genuinely persisted under a
// DIFFERENT tenant than the one "glamour-nails" resolves to must never be
// retrievable through that slug, even with its own correct token — the
// receipt service scopes FindByTenantAndReceiptToken by the SLUG-resolved
// tenant id, not merely by matching the token string.
func TestPublicBookingReceiptRouteDeniesAnotherTenantsBooking(t *testing.T) {
	handler, _, store := buildPublicBookingRoutes(s9NailFixture())

	foreignToken := "foreign0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	store.mu.Lock()
	store.bookings = append(store.bookings, &schedulingmodel.Booking{
		ID:                 "660e8400-e29b-41d4-a716-446655440099",
		TenantID:           s9TenantBID, // a DIFFERENT tenant than glamour-nails (s9TenantAID)
		ServiceID:          s9ServiceB,
		StaffID:            s9StaffB,
		Customer:           schedulingmodel.Customer{Name: "Someone Else"},
		StartAt:            time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC),
		EndAt:              time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC),
		Status:             schedulingmodel.BookingConfirmed,
		ReceiptAccessToken: foreignToken,
	})
	store.mu.Unlock()
	foreignReference := "NB-" + strings.ToUpper(strings.ReplaceAll("660e8400-e29b-41d4-a716-446655440099", "-", ""))[:8]

	rec := getReceipt(handler, "glamour-nails", foreignReference, foreignToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404 — a foreign tenant's booking must be indistinguishable from a nonexistent one", rec.Code, rec.Body.String())
	}
	assertBodyCode(t, rec, "BOOKING_NOT_FOUND")
}

func TestPublicBookingRouteRejectsMalformedJSON(t *testing.T) {
	handler, _, store := buildPublicBookingRoutes(s9NailFixture())
	rec := postBooking(t, handler, "glamour-nails", "{not json", false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertBodyCode(t, rec, "INVALID_REQUEST")
	if store.count() != 0 {
		t.Fatal("malformed JSON persisted a booking")
	}
}
