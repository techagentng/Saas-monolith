package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization"
	authzhandler "github.com/techagentng/saas-monolith/internal/authorization/handler"
	authzrepository "github.com/techagentng/saas-monolith/internal/authorization/repository"
	authzservice "github.com/techagentng/saas-monolith/internal/authorization/service"
	"github.com/techagentng/saas-monolith/internal/config"
	"github.com/techagentng/saas-monolith/internal/database"
	identityhandler "github.com/techagentng/saas-monolith/internal/identity/handler"
	identityrepository "github.com/techagentng/saas-monolith/internal/identity/repository"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenanthandler "github.com/techagentng/saas-monolith/internal/tenant/handler"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

type Application struct {
	Server *http.Server
	db     *sql.DB
}

func New(ctx context.Context, cfg config.Config) (*Application, error) {
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	users := identityrepository.NewPostgresUserRepository(db)
	sessions := identityrepository.NewPostgresSessionRepository(db)
	tenants := tenantrepository.NewPostgresTenantRepository(db)
	memberships := tenantrepository.NewPostgresMembershipRepository(db)
	userService := identityservice.NewUserService(users, identityservice.NewBcryptHasher())
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{AccessLifetime: cfg.AccessLifetime, SessionLifetime: cfg.SessionLifetime, PrivateKey: cfg.PrivateKey, PublicKey: cfg.PublicKey})
	authenticationService := identityservice.NewAuthenticationService(users, identityservice.NewBcryptHasher(), sessions, tokens)
	// The refresh cookie lives as long as the session it points at — a
	// shorter Max-Age would sign the browser out while its session is still
	// valid server-side, which is the exact defect this replaces.
	refreshCookie := identityhandler.RefreshCookieConfig{
		Secure:   cfg.CookieSecure,
		SameSite: identityhandler.ParseSameSite(cfg.CookieSameSite),
		MaxAge:   cfg.SessionLifetime,
	}
	authenticationHandler := identityhandler.NewAuthenticationHandler(authenticationService, refreshCookie)
	userHandler := identityhandler.NewUserHandler(userService)
	membershipService := tenantservice.NewMembershipService(users, tenants, memberships)
	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	membershipHandler := tenanthandler.NewMembershipHandler(membershipService)
	tenantCreationService := tenantservice.NewTenantService(db, users, tenants)
	tenantRetrievalService := tenantservice.NewRetrievalService(tenants)
	tenantHandler := tenanthandler.NewTenantHandler(tenantCreationService, tenantRetrievalService)
	publicTenantService := tenantservice.NewPublicTenantService(tenants)
	publicTenantHandler := tenanthandler.NewPublicTenantHandler(publicTenantService)
	onboardingService := tenantservice.NewOnboardingService(tenants)
	onboardingHandler := tenanthandler.NewOnboardingHandler(onboardingService)
	currencyService := tenantservice.NewCurrencyService(tenants)
	currencyHandler := tenanthandler.NewCurrencyHandler(currencyService)

	// Scheduling S1: the appointment-style service catalog. The module reads
	// the tenant only to resolve its currency before a price is accepted, so it
	// is handed the tenant repository through its own narrow TenantReader
	// interface rather than the whole tenant service.
	serviceRepository := schedulingrepository.NewPostgresServiceRepository(db)
	catalogService := schedulingservice.NewCatalogService(serviceRepository, tenants)
	serviceHandler := schedulinghandler.NewServiceHandler(catalogService)

	// Scheduling S3: bookable staff and the services each of them performs. The
	// module validates a profile's optional user link against tenant membership,
	// so it is handed the membership repository through its own narrow
	// MembershipReader interface. It owns the transaction for atomic capability
	// replacement, hence the *sql.DB.
	staffService := schedulingservice.NewStaffService(
		db,
		schedulingrepository.NewPostgresStaffRepository(db),
		schedulingrepository.NewPostgresCapabilityRepository(db),
		serviceRepository,
		memberships,
	)
	staffHandler := schedulinghandler.NewStaffHandler(staffService)

	// Scheduling S5: recurring weekly working hours for a staff profile.
	// Deliberately not appointment slots, breaks, holidays, one-off
	// exceptions, or an availability calculation — S7's availability engine
	// will read this table; nothing here computes bookable time. It reuses
	// staff.read / staff.update rather than a new permission, the same
	// reasoning capability assignment already established: this changes a
	// staff profile's configuration, not a distinct authorization concern.
	workingHoursService := schedulingservice.NewWorkingHoursService(
		db,
		schedulingrepository.NewPostgresWorkingHoursRepository(db),
		schedulingrepository.NewPostgresStaffRepository(db),
	)
	workingHoursHandler := schedulinghandler.NewWorkingHoursHandler(workingHoursService)

	// Scheduling S7: the appointment-vertical availability engine. It reads
	// (never writes) the S1 catalog, S3 capability set and S5 working hours,
	// resolves them against the tenant's own authoritative timezone, and
	// returns the bookable slots for one service with one technician on one
	// date. The pure slot maths lives in internal/scheduling/availability.
	//
	// Booking-conflict exclusion is now live (S10): the S7 OccupancyReader seam
	// is wired to the booking repository, so every persisted CONFIRMED booking
	// removes the slots it covers from this engine's output. SystemClock is
	// injected rather than calling time.Now() inside the domain logic so
	// past-slot filtering stays deterministic.
	//
	// It reuses staff.read: from the caller's side this is reading a
	// technician's bookable time, the same authorization concern the
	// working-hours GET already carries. The anonymous public availability API
	// is S9 and is deliberately not registered here.
	bookingRepository := schedulingrepository.NewPostgresBookingRepository(db)
	availabilityService := schedulingservice.NewAvailabilityService(
		tenants,
		serviceRepository,
		schedulingrepository.NewPostgresStaffRepository(db),
		schedulingrepository.NewPostgresCapabilityRepository(db),
		schedulingrepository.NewPostgresWorkingHoursRepository(db),
		bookingRepository,
		schedulingservice.SystemClock{},
	)
	availabilityHandler := schedulinghandler.NewAvailabilityHandler(availabilityService)

	// Scheduling S8: the anonymous, customer-facing view of a NAIL_TECHNICIAN
	// tenant's service catalog — the first screen of the public booking journey
	// (/book/{slug} on the frontend). It reuses the existing public slug gate
	// (PublicTenantService.ResolvePublicTenant: reserved / canonical /
	// ACTIVE+COMPLETED) rather than re-checking visibility, enforces the
	// NAIL_TECHNICIAN vertical, and exposes only customer-safe service fields
	// (no status, no timestamps, no tenant id). Public availability is S9;
	// booking creation is S10.
	publicCatalogService := schedulingservice.NewPublicCatalogService(publicTenantService, serviceRepository)
	publicServiceHandler := schedulinghandler.NewPublicServiceHandler(publicCatalogService)

	// Scheduling S9: the anonymous public availability flow — steps 2 and 3 of
	// the /book/{slug} journey. Both reuse the identical PublicTenantService
	// visibility gate (reserved / canonical / ACTIVE+COMPLETED) and the
	// NAIL_TECHNICIAN vertical check S8 uses; neither re-implements it.
	//
	//   - PublicStaffService lists the ACTIVE, bookable technicians assigned to
	//     a service, projected to {id, name} only.
	//   - PublicAvailabilityService is a thin gate over the S7 engine: it
	//     resolves the slug to an internal tenant id and delegates to
	//     availabilityService.GetAvailability — the slot maths, capability
	//     check and timezone resolution are S7's and are not duplicated.
	//
	publicStaffService := schedulingservice.NewPublicStaffService(
		publicTenantService,
		serviceRepository,
		schedulingrepository.NewPostgresStaffRepository(db),
		schedulingrepository.NewPostgresCapabilityRepository(db),
	)
	publicStaffHandler := schedulinghandler.NewPublicStaffHandler(publicStaffService)
	publicAvailabilityService := schedulingservice.NewPublicAvailabilityService(publicTenantService, availabilityService)
	publicAvailabilityHandler := schedulinghandler.NewPublicAvailabilityHandler(publicAvailabilityService)

	// Scheduling S10: anonymous appointment booking creation — the step that
	// turns an S9 slot selection into a persisted appointment. It reuses the
	// same PublicTenantService gate and NAIL_TECHNICIAN check S8/S9 use, and it
	// re-validates the requested slot against the S7 engine (never trusting the
	// client because a slot was returned earlier). The bookings_no_overlap
	// exclusion constraint in migration 000016 is the concurrency authority:
	// exactly one of two racing customers wins, the other gets
	// BOOKING_SLOT_UNAVAILABLE (409). Service duration and the appointment end
	// are derived server-side; a client-supplied end/duration/price is ignored.
	bookingService := schedulingservice.NewBookingService(
		publicTenantService,
		availabilityService,
		serviceRepository,
		schedulingrepository.NewPostgresStaffRepository(db),
		bookingRepository,
	)
	publicBookingHandler := schedulinghandler.NewPublicBookingHandler(bookingService)

	// Scheduling S11: authenticated, tenant-scoped owner/staff booking
	// management — list, detail, and cancel for the appointments S10 persists.
	// It reads and cancels only; it never creates, and it touches no
	// scheduling logic. Cancelling flips CONFIRMED -> CANCELLED in place (the
	// row is kept), and because the S7 occupancy query already counts only
	// CONFIRMED bookings, the freed slot reappears in public S9 availability
	// with no change to the engine. booking.read / booking.update (migration
	// 000017) gate the routes; SystemClock splits Upcoming from Past on
	// absolute start_at.
	bookingManagementService := schedulingservice.NewBookingManagementService(
		bookingRepository, bookingRepository, tenants, schedulingservice.SystemClock{},
	)
	bookingManagementHandler := schedulinghandler.NewBookingManagementHandler(bookingManagementService)

	roles := authzrepository.NewPostgresRoleRepository(db)
	rolePermissions := authzrepository.NewPostgresRolePermissionRepository(db)
	userRoles := authzrepository.NewPostgresUserRoleRepository(db)
	permissionResolution := authzservice.NewPermissionResolutionService(userRoles, rolePermissions, memberships, tenants)
	authorizer := authzservice.NewAuthorizer(permissionResolution)
	assignmentService := authzservice.NewAssignmentService(users, tenants, roles, userRoles, memberships)
	roleAssignmentService := authzservice.NewTenantRoleAssignmentService(authorizer, roles, assignmentService)
	roleAssignmentHandler := authzhandler.NewRoleAssignmentHandler(roleAssignmentService)
	permissionsHandler := authzhandler.NewPermissionsHandler(permissionResolution)

	api := http.NewServeMux()
	api.HandleFunc("GET /health", health)
	api.HandleFunc("POST /api/v1/auth/login", authenticationHandler.Login)
	api.HandleFunc("POST /api/v1/auth/refresh", authenticationHandler.Refresh)
	api.HandleFunc("POST /api/v1/users", userHandler.Create)
	// Public tenant identity (Feature 5): resolve a tenant by its public slug.
	// This route is intentionally anonymous — no authentication, no tenant
	// context, and no permission middleware — because the slug IS the public
	// identity. It is registered before the auth middleware is even built so
	// it cannot be wrapped into a private chain by accident. The service hides
	// non-ACTIVE tenants and reserved slugs behind TENANT_NOT_FOUND, and the
	// response DTO carries no internal identifiers or private contact data.
	api.HandleFunc("GET /api/v1/public/tenants/{slug}", func(writer http.ResponseWriter, request *http.Request) {
		publicTenantHandler.GetBySlug(writer, request, request.PathValue("slug"))
	})
	// Public service catalog (Scheduling S8): the anonymous NAIL_TECHNICIAN
	// booking catalog. Registered here, alongside the public tenant route and
	// before the auth middleware exists, for the identical reason — the slug is
	// the public identity and this must never be wrapped into a private chain.
	// The service delegates the visibility gate to PublicTenantService, refuses
	// non-nail verticals, and returns only ACTIVE services with customer-safe
	// fields.
	api.HandleFunc("GET /api/v1/public/tenants/{slug}/services", func(writer http.ResponseWriter, request *http.Request) {
		publicServiceHandler.List(writer, request, request.PathValue("slug"))
	})
	// Public technician discovery (Scheduling S9): the customer-safe list of
	// technicians who can perform one service. Anonymous, same slug gate,
	// NAIL_TECHNICIAN only, {id, name} projection.
	api.HandleFunc("GET /api/v1/public/tenants/{slug}/services/{serviceID}/staff", func(writer http.ResponseWriter, request *http.Request) {
		publicStaffHandler.List(writer, request, request.PathValue("slug"), request.PathValue("serviceID"))
	})
	// Public availability (Scheduling S9): the S7 slot engine behind the public
	// slug gate. Anonymous. ?service_id=&staff_id=&date=YYYY-MM-DD; the date is
	// tenant-local and a caller-supplied timezone is ignored.
	api.HandleFunc("GET /api/v1/public/tenants/{slug}/availability", func(writer http.ResponseWriter, request *http.Request) {
		publicAvailabilityHandler.Get(writer, request, request.PathValue("slug"))
	})
	// Public booking creation (Scheduling S10): the anonymous POST that
	// persists an appointment. Same bare-mux, pre-auth-middleware registration
	// as every other public route — it must never be wrapped in a private
	// chain. Body carries identifiers + selected start + customer contact only;
	// the backend derives tenant, duration, end and timezone. A concurrent
	// conflict is a deterministic 409 BOOKING_SLOT_UNAVAILABLE.
	api.HandleFunc("POST /api/v1/public/tenants/{slug}/bookings", func(writer http.ResponseWriter, request *http.Request) {
		publicBookingHandler.Create(writer, request, request.PathValue("slug"))
	})

	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: sessions}
	tenantMiddleware := tenant.Middleware{Resolver: contextService}
	api.Handle("POST /api/v1/auth/logout", authMiddleware.Wrap(http.HandlerFunc(authenticationHandler.Logout)))

	// User retrieval (safety correction): self-only. GetByID requires
	// authentication and only ever returns the caller's own record — see the
	// comment on UserHandler.GetByID for why this is self-only rather than
	// tenant-scoped or platform-admin-controlled. Registered separately from
	// POST /api/v1/users, which must stay anonymous for registration.
	api.Handle("GET /api/v1/users/{id}", authMiddleware.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		userHandler.GetByID(writer, request, request.PathValue("id"))
	})))
	api.Handle("GET /api/v1/tenants/{tenantID}/context", authMiddleware.Wrap(tenantContextHandler(contextService)))

	// Tenant creation (Feature 2): any authenticated user may create a
	// tenant and becomes its BUSINESS_OWNER. No tenant context or tenant
	// permission middleware applies here — there is no tenant yet against
	// which either could be evaluated.
	api.Handle("POST /api/v1/tenants", authMiddleware.Wrap(http.HandlerFunc(tenantHandler.Create)))

	// Tenant retrieval (Feature 3): List accessible tenants
	// Ordering: Authentication -> Handler (membership-based access control in service)
	api.Handle("GET /api/v1/tenants", authMiddleware.Wrap(http.HandlerFunc(tenantHandler.List)))

	// Tenant retrieval (Feature 3): Get accessible tenant by ID
	// Ordering: Authentication -> Tenant Context -> Authorization (tenant.read) -> Handler
	api.Handle("GET /api/v1/tenants/{tenantID}", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				tenantHandler.GetByID(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))

	// Tenant profile update (Feature 4): Update tenant profile fields
	// Ordering: Authentication -> Tenant Context -> Authorization (tenant.update) -> Handler
	api.Handle("PATCH /api/v1/tenants/{tenantID}", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.update"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				tenantHandler.UpdateProfile(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))

	// Onboarding progress (Vertical Onboarding F2): flexible-order save of the
	// caller's current onboarding step. No sequential enforcement, but tenant
	// access and tenant.update are required — the same chain Feature 4's
	// profile PATCH already uses, reused unchanged.
	// Ordering: Authentication -> Tenant Context -> Authorization (tenant.update) -> Handler
	api.Handle("PATCH /api/v1/tenants/{tenantID}/onboarding", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.update"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				onboardingHandler.SaveProgress(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))

	// Onboarding completion (Vertical Onboarding F2): a validated, one-way
	// transition to COMPLETED — see OnboardingService.Complete and
	// validateOnboardingCompletionPrerequisites. Same authorization chain as
	// the save-progress route above; this is deliberately not a lesser check.
	api.Handle("POST /api/v1/tenants/{tenantID}/onboarding/complete", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.update"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				onboardingHandler.Complete(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))

	// Tenant currency (Scheduling S1): a write-once declaration of the currency
	// every price in this tenant is denominated in. Deliberately its own
	// endpoint rather than a field on PATCH /tenants/{tenantID} — see
	// CurrencyService for why a write-once value does not belong in a
	// partial-update request shape. It carries tenant.update, the same
	// permission that already governs changing tenant-level settings.
	// Ordering: Authentication -> Tenant Context -> Authorization (tenant.update) -> Handler
	api.Handle("PUT /api/v1/tenants/{tenantID}/currency", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.update"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				currencyHandler.Set(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))

	// Scheduling S1 service catalog (tenant-management only — the anonymous
	// public catalog is S8 and is deliberately not registered here).
	//
	// Route authorization matrix:
	//   POST   /api/v1/tenants/{tenantID}/services                     TENANT  service.create
	//   GET    /api/v1/tenants/{tenantID}/services                     TENANT  service.read
	//   GET    /api/v1/tenants/{tenantID}/services/{serviceID}         TENANT  service.read
	//   PATCH  /api/v1/tenants/{tenantID}/services/{serviceID}         TENANT  service.update
	//   POST   /api/v1/tenants/{tenantID}/services/{serviceID}/archive TENANT  service.archive
	// Ordering: Authentication -> Tenant Context -> Authorization -> Handler.
	api.Handle("POST /api/v1/tenants/{tenantID}/services", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "service.create"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				serviceHandler.Create(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))
	api.Handle("GET /api/v1/tenants/{tenantID}/services", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "service.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				serviceHandler.List(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))
	api.Handle("GET /api/v1/tenants/{tenantID}/services/{serviceID}", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "service.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				serviceHandler.Get(writer, request, request.PathValue("tenantID"), request.PathValue("serviceID"))
			}),
		),
	)))
	api.Handle("PATCH /api/v1/tenants/{tenantID}/services/{serviceID}", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "service.update"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				serviceHandler.Update(writer, request, request.PathValue("tenantID"), request.PathValue("serviceID"))
			}),
		),
	)))
	api.Handle("POST /api/v1/tenants/{tenantID}/services/{serviceID}/archive", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "service.archive"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				serviceHandler.Archive(writer, request, request.PathValue("tenantID"), request.PathValue("serviceID"))
			}),
		),
	)))

	// Scheduling S3 staff roster and capability assignment (tenant-management
	// only — a public technician list is S8 and is deliberately not registered
	// here).
	//
	// Route authorization matrix:
	//   POST   /api/v1/tenants/{tenantID}/staff                       TENANT  staff.create
	//   GET    /api/v1/tenants/{tenantID}/staff                       TENANT  staff.read
	//   GET    /api/v1/tenants/{tenantID}/staff/{staffID}             TENANT  staff.read
	//   PATCH  /api/v1/tenants/{tenantID}/staff/{staffID}             TENANT  staff.update
	//   POST   /api/v1/tenants/{tenantID}/staff/{staffID}/archive     TENANT  staff.archive
	//   GET    /api/v1/tenants/{tenantID}/staff/{staffID}/services    TENANT  staff.read
	//   PUT    /api/v1/tenants/{tenantID}/staff/{staffID}/services    TENANT  staff.update
	//
	// Capability assignment carries staff.update, not service.update: it changes
	// what a staff member can do, never the service definition itself. There is
	// deliberately no separate staff.assign permission.
	// Ordering: Authentication -> Tenant Context -> Authorization -> Handler.
	api.Handle("POST /api/v1/tenants/{tenantID}/staff", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.create"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				staffHandler.Create(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))
	api.Handle("GET /api/v1/tenants/{tenantID}/staff", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				staffHandler.List(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))
	api.Handle("GET /api/v1/tenants/{tenantID}/staff/{staffID}", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				staffHandler.Get(writer, request, request.PathValue("tenantID"), request.PathValue("staffID"))
			}),
		),
	)))
	api.Handle("PATCH /api/v1/tenants/{tenantID}/staff/{staffID}", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.update"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				staffHandler.Update(writer, request, request.PathValue("tenantID"), request.PathValue("staffID"))
			}),
		),
	)))
	api.Handle("POST /api/v1/tenants/{tenantID}/staff/{staffID}/archive", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.archive"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				staffHandler.Archive(writer, request, request.PathValue("tenantID"), request.PathValue("staffID"))
			}),
		),
	)))
	api.Handle("GET /api/v1/tenants/{tenantID}/staff/{staffID}/services", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				staffHandler.ListCapabilities(writer, request, request.PathValue("tenantID"), request.PathValue("staffID"))
			}),
		),
	)))
	api.Handle("PUT /api/v1/tenants/{tenantID}/staff/{staffID}/services", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.update"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				staffHandler.ReplaceCapabilities(writer, request, request.PathValue("tenantID"), request.PathValue("staffID"))
			}),
		),
	)))

	// Scheduling S5 working hours:
	//   GET  /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours  TENANT  staff.read
	//   PUT  /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours  TENANT  staff.update
	// Ordering: Authentication -> Tenant Context -> Authorization -> Handler.
	api.Handle("GET /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				workingHoursHandler.Get(writer, request, request.PathValue("tenantID"), request.PathValue("staffID"))
			}),
		),
	)))
	api.Handle("PUT /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.update"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				workingHoursHandler.Replace(writer, request, request.PathValue("tenantID"), request.PathValue("staffID"))
			}),
		),
	)))

	// Scheduling S7 availability (tenant-management / dashboard only — the
	// anonymous public availability API is S9 and is deliberately not
	// registered here):
	//   GET  /api/v1/tenants/{tenantID}/availability  TENANT  staff.read
	//        ?service_id=...&staff_id=...&date=YYYY-MM-DD
	// Ordering: Authentication -> Tenant Context -> Authorization -> Handler.
	api.Handle("GET /api/v1/tenants/{tenantID}/availability", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "staff.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				availabilityHandler.Get(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))

	// Scheduling S11 owner booking management (tenant-management / dashboard):
	//   GET  /api/v1/tenants/{tenantID}/bookings                       TENANT  booking.read
	//        ?view=upcoming|past|cancelled|all&staff_id=...&service_id=...
	//   GET  /api/v1/tenants/{tenantID}/bookings/{bookingID}           TENANT  booking.read
	//   POST /api/v1/tenants/{tenantID}/bookings/{bookingID}/cancel    TENANT  booking.update
	// Ordering: Authentication -> Tenant Context -> Authorization -> Handler.
	// Cancellation carries booking.update, not a bespoke code: it changes a
	// booking's state, and there is deliberately no booking.cancel permission.
	api.Handle("GET /api/v1/tenants/{tenantID}/bookings", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "booking.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				bookingManagementHandler.List(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))
	api.Handle("GET /api/v1/tenants/{tenantID}/bookings/{bookingID}", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "booking.read"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				bookingManagementHandler.Get(writer, request, request.PathValue("tenantID"), request.PathValue("bookingID"))
			}),
		),
	)))
	api.Handle("POST /api/v1/tenants/{tenantID}/bookings/{bookingID}/cancel", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "booking.update"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				bookingManagementHandler.Cancel(writer, request, request.PathValue("tenantID"), request.PathValue("bookingID"))
			}),
		),
	)))

	// Route authorization matrix (Feature 6):
	//   POST   /api/v1/tenants/{tenantID}/members                 TENANT  user.create
	//   DELETE /api/v1/tenants/{tenantID}/members/{userID}         TENANT  user.disable
	//   POST   /api/v1/tenants/{tenantID}/role-assignments          TENANT  role.assign
	// Ordering: Authentication -> Tenant Context -> Authorization -> Handler.
	api.Handle("POST /api/v1/tenants/{tenantID}/members", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.create"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				membershipHandler.Create(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))
	api.Handle("DELETE /api/v1/tenants/{tenantID}/members/{userID}", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.disable"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				membershipHandler.Revoke(writer, request, request.PathValue("tenantID"), request.PathValue("userID"))
			}),
		),
	)))
	api.Handle("POST /api/v1/tenants/{tenantID}/role-assignments", authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "role.assign"}.Wrap(
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				roleAssignmentHandler.Assign(writer, request, request.PathValue("tenantID"))
			}),
		),
	)))

	// Effective tenant permissions (frontend F11 dependency): lets the
	// authenticated caller read their own effective permission set for the
	// selected tenant, e.g. to answer can("tenant.update") client-side. This
	// is a self-capability read, not a gate on one permission, so it carries
	// no TenantPermissionMiddleware — it reuses the same PermissionResolutionService
	// that middleware relies on and reports whatever that resolves to,
	// including an empty set for a member with no granted permissions.
	// Ordering: Authentication -> Tenant Context -> Handler.
	api.Handle("GET /api/v1/tenants/{tenantID}/permissions", authMiddleware.Wrap(tenantMiddleware.Wrap(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			permissionsHandler.GetEffective(writer, request, request.PathValue("tenantID"))
		}),
	)))

	return &Application{db: db, Server: &http.Server{Addr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), Handler: corsMiddleware(cfg.AllowedOrigins, api), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}}, nil
}

func (a *Application) Close() error {
	if a.db == nil {
		return nil
	}
	return a.db.Close()
}

func health(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(map[string]string{"status": "ok"})
}

func tenantContextHandler(resolver tenantservice.TenantContextService) http.Handler {
	return tenant.Middleware{Resolver: resolver}.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		trusted, ok := tenant.FromContext(request.Context())
		if !ok {
			http.Error(writer, "", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(trusted)
	}))
}
