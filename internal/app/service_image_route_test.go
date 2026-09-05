package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization"
	authzservice "github.com/techagentng/saas-monolith/internal/authorization/service"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	"github.com/techagentng/saas-monolith/internal/media"
	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the exact production middleware chain app.New wires
// for Service Images, reusing service.read/service.update — the same
// route-authorization matrix service-categories and service-suggestions
// already establish (no new permission family).
//
// failingTxBeginner stands in for the real *sql.DB: Upload, List, a
// plain alt-text Update, and a non-primary Delete never open a transaction
// (see ServiceImageService's own doc comments), so these routes are fully
// exercisable without one. Making-primary, promoting-on-delete and Reorder
// DO require a real transaction and are proven instead by
// postgres_service_image_repository_integration_test.go against real
// Postgres — the identical split StaffService.ReplaceCapabilities' own tests
// already establish.
type failingTxBeginner struct{}

func (failingTxBeginner) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	return nil, errors.New("BeginTx reached in a route test that should not need one")
}

func buildImageRoutes(t *testing.T, scenario *catalogScenario, tenantPermissions []string, images *statefulServiceImageRepository) (http.Handler, *identityservice.TokenManager) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: &fakeSessionRepository{}}

	tenants := &statefulCatalogTenantRepository{tenant: scenario.tenant, otherTenant: scenario.otherTenant}
	memberships := &catalogMembershipRepository{scenario: scenario}
	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	tenantMiddleware := tenant.Middleware{Resolver: contextService}
	authorizer := authzservice.NewAuthorizer(&fakeResolutionService{tenantPermissions: tenantPermissions})

	services := &statefulServiceRepository{services: scenario.services}
	imageService := schedulingservice.NewServiceImageService(failingTxBeginner{}, images, services, media.NewFakeStorage())
	imageHandler := schedulinghandler.NewServiceImageHandler(imageService)

	wrap := func(permission string, next http.HandlerFunc) http.Handler {
		return authMiddleware.Wrap(tenantMiddleware.Wrap(
			authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: permission}.Wrap(next),
		))
	}

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/tenants/{tenantID}/services/{serviceID}/images", wrap("service.update", func(w http.ResponseWriter, r *http.Request) {
		imageHandler.Create(w, r, r.PathValue("tenantID"), r.PathValue("serviceID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/services/{serviceID}/images", wrap("service.read", func(w http.ResponseWriter, r *http.Request) {
		imageHandler.List(w, r, r.PathValue("tenantID"), r.PathValue("serviceID"))
	}))
	mux.Handle("PATCH /api/v1/tenants/{tenantID}/services/{serviceID}/images/{imageID}", wrap("service.update", func(w http.ResponseWriter, r *http.Request) {
		imageHandler.Update(w, r, r.PathValue("tenantID"), r.PathValue("serviceID"), r.PathValue("imageID"))
	}))
	mux.Handle("DELETE /api/v1/tenants/{tenantID}/services/{serviceID}/images/{imageID}", wrap("service.update", func(w http.ResponseWriter, r *http.Request) {
		imageHandler.Delete(w, r, r.PathValue("tenantID"), r.PathValue("serviceID"), r.PathValue("imageID"))
	}))

	return mux, tokens
}

func multipartBody(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("images", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

func TestImageRoutesRequireAuthentication(t *testing.T) {
	scenario := catalogScenarioWithCurrency()
	scenario.services[catalogRouteServiceA] = activeService(catalogRouteTenantA)
	images := newStatefulServiceImageRepository()
	handler, _ := buildImageRoutes(t, scenario, businessOwnerPermissions, images)

	path := "/api/v1/tenants/" + catalogRouteTenantA + "/services/" + catalogRouteServiceA + "/images"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s, want 401", recorder.Code, recorder.Body.String())
	}
}

func TestStaffCanListImagesButNotUpload(t *testing.T) {
	scenario := catalogScenarioWithCurrency()
	scenario.services[catalogRouteServiceA] = activeService(catalogRouteTenantA)
	images := newStatefulServiceImageRepository()
	handler, tokens := buildImageRoutes(t, scenario, staffPermissions, images)

	listPath := "/api/v1/tenants/" + catalogRouteTenantA + "/services/" + catalogRouteServiceA + "/images"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, listPath, ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}

	body, contentType := multipartBody(t, "a.jpg", []byte("fake"))
	uploadRequest := catalogRequest(t, tokens, http.MethodPost, listPath, "")
	uploadRequest.Body = httptest.NewRequest(http.MethodPost, listPath, body).Body
	uploadRequest.Header.Set("Content-Type", contentType)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, uploadRequest)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("upload: status = %d, body = %s, want 403", recorder.Code, recorder.Body.String())
	}
	if images.writeCalls != 0 {
		t.Fatal("a STAFF request reached an image write")
	}
}

func TestBusinessOwnerCanUploadListAndDeleteImages(t *testing.T) {
	scenario := catalogScenarioWithCurrency()
	scenario.services[catalogRouteServiceA] = activeService(catalogRouteTenantA)
	images := newStatefulServiceImageRepository()
	handler, tokens := buildImageRoutes(t, scenario, businessOwnerPermissions, images)
	base := "/api/v1/tenants/" + catalogRouteTenantA + "/services/" + catalogRouteServiceA + "/images"

	body, contentType := multipartBody(t, "a.jpg", []byte("fake-jpeg-bytes"))
	uploadRequest := catalogRequest(t, tokens, http.MethodPost, base, "")
	uploadRequest.Body = httptest.NewRequest(http.MethodPost, base, body).Body
	uploadRequest.Header.Set("Content-Type", contentType)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, uploadRequest)
	// The fake bytes are not a real JPEG, so ServiceImageService's own MIME
	// sniff correctly refuses them — this route test's job is proving the
	// AUTHORIZATION chain (owner reaches the handler at all), not
	// re-proving upload validation, which service_image_service_test.go
	// already covers exhaustively with real encoded images.
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("upload: status = %d, body = %s, want 400 (validation reached, not 403)", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, base, ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
}

// A category/image belonging to another tenant must never be reachable
// through this tenant's own route — proven here by seeding the image under
// tenant B and requesting it via tenant A's route.
func TestImageRouteDoesNotLeakAnotherTenantsImage(t *testing.T) {
	scenario := catalogScenarioWithCurrency()
	scenario.services[catalogRouteServiceA] = activeService(catalogRouteTenantA)
	const foreignImageID = "550e8400-e29b-41d4-a716-446655449999"
	images := newStatefulServiceImageRepository()
	images.images[foreignImageID] = &schedulingmodel.ServiceImage{
		ID: foreignImageID, TenantID: catalogRouteTenantB, ServiceID: catalogRouteServiceA,
		StorageKey: "k", PublicURL: "https://cdn.test.local/media/k.jpg", MimeType: "image/jpeg", FileSize: 1,
	}
	handler, tokens := buildImageRoutes(t, scenario, businessOwnerPermissions, images)

	path := "/api/v1/tenants/" + catalogRouteTenantA + "/services/" + catalogRouteServiceA + "/images/" + foreignImageID
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodDelete, path, ""))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404 — tenant A must not be able to delete tenant B's image", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "IMAGE_NOT_FOUND")
	if _, stillThere := images.images[foreignImageID]; !stillThere {
		t.Fatal("a cross-tenant request deleted another tenant's image")
	}
}
