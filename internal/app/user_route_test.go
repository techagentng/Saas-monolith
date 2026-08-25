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
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identityhandler "github.com/techagentng/saas-monolith/internal/identity/handler"
	identitymodel "github.com/techagentng/saas-monolith/internal/identity/model"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
)

// These tests exercise the exact production middleware chain app.New wires
// for the user domain's two routes:
//
//	POST /api/v1/users       -> Handler (no middleware: registration must stay public)
//	GET  /api/v1/users/{id}  -> Authentication -> Handler (self-only)
//
// using real auth.Middleware and identityservice.NewUserService, backed only
// by fake repositories, dispatched through a real http.ServeMux.

const (
	userRouteSelfID    = "550e8400-e29b-41d4-a716-446655440801"
	userRouteOtherID   = "550e8400-e29b-41d4-a716-446655440802"
	userRouteSessionID = "550e8400-e29b-41d4-a716-446655440803"
)

func TestPostUsersRouteRemainsAnonymouslyReachable(t *testing.T) {
	handler, _ := buildUserRoutes(t, &userRouteScenario{})
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"email":"new@example.com","password":"password123"}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201 — registration must remain anonymous", recorder.Code, recorder.Body.String())
	}
}

func TestGetUserRouteRequiresAuthentication(t *testing.T) {
	handler, _ := buildUserRoutes(t, &userRouteScenario{self: &identitymodel.User{ID: userRouteSelfID, Email: "self@example.com", Status: identitymodel.StatusActive}})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/users/"+userRouteSelfID, nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s, want 401 for an anonymous request", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "INVALID_CREDENTIALS")
}

func TestGetUserRouteReturnsOwnProfile(t *testing.T) {
	scenario := &userRouteScenario{self: &identitymodel.User{ID: userRouteSelfID, Email: "self@example.com", Status: identitymodel.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	handler, tokens := buildUserRoutes(t, scenario)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, userRouteAuthenticatedRequest(t, tokens, userRouteSelfID, "/api/v1/users/"+userRouteSelfID))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 for a caller reading their own record", recorder.Code, recorder.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["id"] != userRouteSelfID || body["email"] != "self@example.com" {
		t.Fatalf("response = %s, want the caller's own record", recorder.Body.String())
	}
}

func TestGetUserRouteDeniesLookupOfAnotherUser(t *testing.T) {
	scenario := &userRouteScenario{
		self:  &identitymodel.User{ID: userRouteSelfID, Email: "self@example.com", Status: identitymodel.StatusActive},
		other: &identitymodel.User{ID: userRouteOtherID, Email: "unrelated-tenant-user@example.com", Status: identitymodel.StatusActive},
	}
	handler, tokens := buildUserRoutes(t, scenario)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, userRouteAuthenticatedRequest(t, tokens, userRouteSelfID, "/api/v1/users/"+userRouteOtherID))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404 USER_NOT_FOUND (enumeration-safe denial)", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "USER_NOT_FOUND")
	if strings.Contains(recorder.Body.String(), "unrelated-tenant-user") {
		t.Fatalf("response leaked another user's data: %s", recorder.Body.String())
	}
}

func TestGetUserRouteNonexistentUserIsIndistinguishableFromDenied(t *testing.T) {
	scenario := &userRouteScenario{self: &identitymodel.User{ID: userRouteSelfID, Email: "self@example.com", Status: identitymodel.StatusActive}}
	handler, tokens := buildUserRoutes(t, scenario)

	deniedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deniedRecorder, userRouteAuthenticatedRequest(t, tokens, userRouteSelfID, "/api/v1/users/"+userRouteOtherID))

	notFoundRecorder := httptest.NewRecorder()
	handler.ServeHTTP(notFoundRecorder, userRouteAuthenticatedRequest(t, tokens, userRouteSelfID, "/api/v1/users/550e8400-e29b-41d4-a716-446655440999"))

	if deniedRecorder.Code != notFoundRecorder.Code || deniedRecorder.Body.String() != notFoundRecorder.Body.String() {
		t.Fatalf("a real-but-inaccessible user (status=%d body=%s) is distinguishable from a nonexistent one (status=%d body=%s); enumeration is possible",
			deniedRecorder.Code, deniedRecorder.Body.String(), notFoundRecorder.Code, notFoundRecorder.Body.String())
	}
}

func TestGetUserRouteRejectsMalformedUUID(t *testing.T) {
	scenario := &userRouteScenario{self: &identitymodel.User{ID: userRouteSelfID, Email: "self@example.com", Status: identitymodel.StatusActive}}
	handler, tokens := buildUserRoutes(t, scenario)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, userRouteAuthenticatedRequest(t, tokens, userRouteSelfID, "/api/v1/users/not-a-uuid"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 INVALID_REQUEST", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "INVALID_REQUEST")
}

// --- test wiring -----------------------------------------------------------------

type userRouteScenario struct {
	self  *identitymodel.User
	other *identitymodel.User
}

func buildUserRoutes(t *testing.T, scenario *userRouteScenario) (http.Handler, *identityservice.TokenManager) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: &fakeSessionRepository{}}

	userService := identityservice.NewUserService(&fakeUserRouteRepository{scenario: scenario}, identityservice.NewBcryptHasher())
	userHandler := identityhandler.NewUserHandler(userService)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/users", userHandler.Create)
	mux.Handle("GET /api/v1/users/{id}", authMiddleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userHandler.GetByID(w, r, r.PathValue("id"))
	})))
	return mux, tokens
}

func userRouteAuthenticatedRequest(t *testing.T, tokens *identityservice.TokenManager, userID, path string) *http.Request {
	t.Helper()
	token, err := tokens.Issue(userID, userRouteSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

type fakeUserRouteRepository struct{ scenario *userRouteScenario }

func (f *fakeUserRouteRepository) Create(_ context.Context, user identitymodel.User) (*identitymodel.User, error) {
	return &user, nil
}

func (f *fakeUserRouteRepository) FindByID(_ context.Context, id string) (*identitymodel.User, error) {
	if f.scenario.self != nil && f.scenario.self.ID == id {
		return f.scenario.self, nil
	}
	if f.scenario.other != nil && f.scenario.other.ID == id {
		return f.scenario.other, nil
	}
	return nil, apperrors.New(apperrors.CodeUserNotFound, "user not found", nil)
}

func (f *fakeUserRouteRepository) FindByEmail(context.Context, string) (*identitymodel.User, error) {
	return nil, nil
}
