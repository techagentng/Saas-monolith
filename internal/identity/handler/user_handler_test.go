package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

const (
	selfUserID  = "550e8400-e29b-41d4-a716-446655440000"
	otherUserID = "660e8400-e29b-41d4-a716-446655440999"
)

func requestAsPrincipal(method, path, userID string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{UserID: userID}))
}

func TestCreateUserHandlerReturns201AndSafeUser(t *testing.T) {
	userService := &fakeUserService{created: &model.User{ID: "id", Email: "user@example.com", PasswordHash: "secret-hash", Status: model.StatusActive}}
	handler := NewUserHandler(userService)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"email":"user@example.com","password":"password"}`))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if strings.Contains(recorder.Body.String(), "secret-hash") || strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("response leaked credential data: %s", recorder.Body.String())
	}
}

func TestCreateUserHandlerMapsInvalidJSON(t *testing.T) {
	handler := NewUserHandler(&fakeUserService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader("{"))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, req)

	assertResponseCode(t, recorder, apperrors.CodeInvalidRequest, http.StatusBadRequest)
}

func TestCreateUserHandlerMapsDuplicateEmail(t *testing.T) {
	handler := NewUserHandler(&fakeUserService{createErr: apperrors.New(apperrors.CodeUserAlreadyExists, "internal", nil)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"email":"user@example.com","password":"password"}`))
	recorder := httptest.NewRecorder()

	handler.Create(recorder, req)

	assertResponseCode(t, recorder, apperrors.CodeUserAlreadyExists, http.StatusConflict)
}

func TestGetUserHandlerMapsNotFound(t *testing.T) {
	handler := NewUserHandler(&fakeUserService{findErr: apperrors.New(apperrors.CodeUserNotFound, "internal", nil)})
	req := requestAsPrincipal(http.MethodGet, "/api/v1/users/"+selfUserID, selfUserID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, req, selfUserID)

	assertResponseCode(t, recorder, apperrors.CodeUserNotFound, http.StatusNotFound)
}

func TestGetUserHandlerRequiresAuthentication(t *testing.T) {
	handler := NewUserHandler(&fakeUserService{found: &model.User{ID: selfUserID, Email: "user@example.com", Status: model.StatusActive}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+selfUserID, nil) // no principal in context
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, req, selfUserID)

	assertResponseCode(t, recorder, apperrors.CodeInvalidCredentials, http.StatusUnauthorized)
}

func TestGetUserHandlerDeniesLookupOfAnotherUser(t *testing.T) {
	service := &fakeUserService{found: &model.User{ID: otherUserID, Email: "other@example.com", Status: model.StatusActive}}
	handler := NewUserHandler(service)
	req := requestAsPrincipal(http.MethodGet, "/api/v1/users/"+otherUserID, selfUserID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, req, otherUserID)

	// Denial must be byte-for-byte identical to the genuine not-found response
	// (TestGetUserHandlerMapsNotFound): an authenticated caller must not be
	// able to distinguish "that UUID belongs to a real account I can't read"
	// from "that UUID does not exist" for any other user's ID.
	assertResponseCode(t, recorder, apperrors.CodeUserNotFound, http.StatusNotFound)
	if service.findCalled {
		t.Fatal("service.FindByID must not be called for a self-only violation — no data should reach the handler to leak")
	}
}

func TestGetUserHandlerReturnsOwnProfile(t *testing.T) {
	found := &model.User{ID: selfUserID, Email: "user@example.com", PasswordHash: "secret-hash", Status: model.StatusActive}
	handler := NewUserHandler(&fakeUserService{found: found})
	req := requestAsPrincipal(http.MethodGet, "/api/v1/users/"+selfUserID, selfUserID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, req, selfUserID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 for a caller reading their own record", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "secret-hash") {
		t.Fatalf("response leaked credential data: %s", recorder.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["id"] != selfUserID {
		t.Fatalf("response id = %v, want %v", body["id"], selfUserID)
	}
}

func assertResponseCode(t *testing.T, recorder *httptest.ResponseRecorder, code apperrors.ErrorCode, status int) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, status, recorder.Body.String())
	}
	var body map[string]map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if body["error"]["code"] != string(code) {
		t.Fatalf("error code = %q, want %q", body["error"]["code"], code)
	}
}

type fakeUserService struct {
	created    *model.User
	found      *model.User
	createErr  error
	findErr    error
	findCalled bool
}

func (s *fakeUserService) Create(_ context.Context, _ service.CreateUserInput) (*model.User, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.created, nil
}

func (s *fakeUserService) FindByID(_ context.Context, _ string) (*model.User, error) {
	s.findCalled = true
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.found, nil
}

func (s *fakeUserService) FindByEmail(_ context.Context, _ string) (*model.User, error) {
	return nil, errors.New("not used")
}

func TestGetUserHandlerRejectsMalformedUUID(t *testing.T) {
	service := &fakeUserService{}
	handler := NewUserHandler(service)
	req := requestAsPrincipal(http.MethodGet, "/api/v1/users/not-a-uuid", selfUserID)
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, req, "not-a-uuid")

	assertResponseCode(t, recorder, apperrors.CodeInvalidRequest, http.StatusBadRequest)
}
