package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/config"
	"github.com/techagentng/saas-monolith/internal/database"
	identityhandler "github.com/techagentng/saas-monolith/internal/identity/handler"
	identityrepository "github.com/techagentng/saas-monolith/internal/identity/repository"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
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
	authenticationHandler := identityhandler.NewAuthenticationHandler(authenticationService)
	userHandler := identityhandler.NewUserHandler(userService)
	membershipService := tenantservice.NewMembershipService(users, tenants, memberships)
	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	_ = tenanthandler.NewMembershipHandler(membershipService)

	api := http.NewServeMux()
	api.HandleFunc("GET /health", health)
	api.HandleFunc("POST /api/v1/auth/login", authenticationHandler.Login)
	api.HandleFunc("POST /api/v1/auth/refresh", authenticationHandler.Refresh)
	api.HandleFunc("POST /api/v1/users", userHandler.Create)
	api.HandleFunc("GET /api/v1/users/{id}", func(writer http.ResponseWriter, request *http.Request) {
		userHandler.GetByID(writer, request, request.PathValue("id"))
	})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: sessions}
	api.Handle("POST /api/v1/auth/logout", authMiddleware.Wrap(http.HandlerFunc(authenticationHandler.Logout)))
	api.Handle("GET /api/v1/tenants/{tenantID}/context", authMiddleware.Wrap(tenantContextHandler(contextService)))
	return &Application{db: db, Server: &http.Server{Addr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}}, nil
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
