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
	catalogService := schedulingservice.NewCatalogService(schedulingrepository.NewPostgresServiceRepository(db), tenants)
	serviceHandler := schedulinghandler.NewServiceHandler(catalogService)

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
