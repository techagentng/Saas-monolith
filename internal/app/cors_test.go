package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCorsAllowsConfiguredOrigin(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusOK) })
	handler := corsMiddleware([]string{"http://localhost:3000"}, next)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if !called {
		t.Fatal("request did not reach the wrapped handler")
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the matched origin", got)
	}
}

func TestCorsOmitsHeaderForUnlistedOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := corsMiddleware([]string{"http://localhost:3000"}, next)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Origin", "https://evil.example.com")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for an unlisted origin", got)
	}
}

func TestCorsHandlesPreflightWithoutReachingHandler(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := corsMiddleware([]string{"http://localhost:3000"}, next)

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Access-Control-Request-Method", "POST")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if called {
		t.Fatal("preflight request reached the wrapped handler")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for a handled preflight", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("Access-Control-Allow-Methods missing on preflight response")
	}
	if got := recorder.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("Access-Control-Allow-Headers missing on preflight response")
	}
}

// A browser preflight for the write-once endpoints (tenant currency, staff
// capabilities, working hours) carries Access-Control-Request-Method: PUT, and
// the browser blocks the real request unless PUT is in Allow-Methods.
func TestCorsPreflightAllowsPutForWriteOnceEndpoints(t *testing.T) {
	handler := corsMiddleware([]string{"http://localhost:3000"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("preflight reached the wrapped handler")
	}))

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/tenants/00000000-0000-0000-0000-000000000000/currency", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Access-Control-Request-Method", "PUT")
	request.Header.Set("Access-Control-Request-Headers", "content-type,authorization")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want true", got)
	}
	methods := recorder.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(methods, "PUT") {
		t.Fatalf("Access-Control-Allow-Methods = %q, must contain PUT", methods)
	}
	// Every method the router registers must be preflight-allowed.
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		if !strings.Contains(methods, m) {
			t.Fatalf("Access-Control-Allow-Methods = %q, missing %s", methods, m)
		}
	}
	headers := recorder.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(headers, "Content-Type") || !strings.Contains(headers, "Authorization") {
		t.Fatalf("Access-Control-Allow-Headers = %q, want Content-Type + Authorization", headers)
	}
}

// The wildcard origin is illegal with credentials; the middleware must never
// emit it.
func TestCorsNeverUsesWildcardWithCredentials(t *testing.T) {
	handler := corsMiddleware([]string{"http://localhost:3000"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got == "*" {
		t.Fatal("Access-Control-Allow-Origin is '*' alongside credentials")
	}
	if recorder.Header().Get("Access-Control-Allow-Credentials") == "true" && recorder.Header().Get("Access-Control-Allow-Origin") == "*" {
		t.Fatal("wildcard origin with credentials")
	}
}
