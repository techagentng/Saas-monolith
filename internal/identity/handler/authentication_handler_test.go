package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

func TestLoginHandlerReturnsCredentialsWithoutPassword(t *testing.T) {
	handler := NewAuthenticationHandler(&fakeAuthenticationService{result: &service.AuthenticationResult{User: &model.User{ID: "id", Email: "user@example.com", PasswordHash: "hash"}, AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"user@example.com","password":"password"}`))

	handler.Login(recorder, request)

	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "hash") || strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("status/body = %d/%s", recorder.Code, recorder.Body.String())
	}
}

func TestLogoutHandlerUsesAuthenticatedPrincipal(t *testing.T) {
	serviceMock := &fakeAuthenticationService{}
	handler := NewAuthenticationHandler(serviceMock)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil).WithContext(auth.WithPrincipal(context.Background(), auth.Principal{UserID: "user", SessionID: "session"}))
	recorder := httptest.NewRecorder()

	handler.Logout(recorder, request)

	if recorder.Code != http.StatusNoContent || serviceMock.loggedOut != "session" {
		t.Fatalf("status/session = %d/%q", recorder.Code, serviceMock.loggedOut)
	}
}

type fakeAuthenticationService struct {
	result    *service.AuthenticationResult
	createErr error
	loggedOut string
}

func (s *fakeAuthenticationService) Login(context.Context, service.LoginInput) (*service.AuthenticationResult, error) {
	return s.result, s.createErr
}
func (s *fakeAuthenticationService) Refresh(context.Context, string) (*service.AuthenticationResult, error) {
	return s.result, s.createErr
}
func (s *fakeAuthenticationService) Logout(_ context.Context, sessionID string) error {
	s.loggedOut = sessionID
	return s.createErr
}
