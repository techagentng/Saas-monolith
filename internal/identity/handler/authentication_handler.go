package handler

import (
	"encoding/json"
	"net/http"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

type AuthenticationHandler struct {
	service service.AuthenticationService
	cookie  RefreshCookieConfig
}

func NewAuthenticationHandler(authenticationService service.AuthenticationService, cookie RefreshCookieConfig) *AuthenticationHandler {
	return &AuthenticationHandler{service: authenticationService, cookie: cookie}
}

func (h *AuthenticationHandler) Login(writer http.ResponseWriter, request *http.Request) {
	var input service.LoginInput
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		_ = apperrors.Map(apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)).WriteJSON(writer)
		return
	}
	result, err := h.service.Login(request.Context(), input)
	if err != nil {
		_ = apperrors.Map(err).WriteJSON(writer)
		return
	}
	h.writeAuthenticationResult(writer, result)
}

// Refresh takes the refresh credential exclusively from the HttpOnly cookie.
// There is deliberately no request-body fallback: a body-supplied refresh
// token would be readable by (and therefore stealable from) frontend
// JavaScript, which is the exact exposure the cookie exists to remove.
func (h *AuthenticationHandler) Refresh(writer http.ResponseWriter, request *http.Request) {
	token := readRefreshCookie(request)
	if token == "" {
		// Same code/message as a bad credential: a caller cannot use this to
		// distinguish "no session cookie" from "session no longer valid".
		_ = apperrors.Map(apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil)).WriteJSON(writer)
		return
	}
	result, err := h.service.Refresh(request.Context(), token)
	if err != nil {
		// The presented credential did not rotate successfully (expired,
		// revoked, already-rotated, or unknown). Clear the cookie so the
		// browser stops replaying a credential that will never work again.
		h.cookie.clear(writer)
		_ = apperrors.Map(err).WriteJSON(writer)
		return
	}
	h.writeAuthenticationResult(writer, result)
}

func (h *AuthenticationHandler) Logout(writer http.ResponseWriter, request *http.Request) {
	principal, ok := auth.FromContext(request.Context())
	if !ok {
		_ = apperrors.Map(apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil)).WriteJSON(writer)
		return
	}
	if err := h.service.Logout(request.Context(), principal.SessionID); err != nil {
		_ = apperrors.Map(err).WriteJSON(writer)
		return
	}
	// Server-side revocation is the real logout; clearing the cookie stops
	// the browser from presenting a credential that is already dead.
	h.cookie.clear(writer)
	writer.WriteHeader(http.StatusNoContent)
}

// writeAuthenticationResult sends the refresh credential as an HttpOnly
// cookie and returns only the short-lived access token in the body. The
// refresh token is intentionally absent from the JSON: nothing in the
// browser should be able to read it, and the frontend never needs to.
func (h *AuthenticationHandler) writeAuthenticationResult(writer http.ResponseWriter, result *service.AuthenticationResult) {
	if result.RefreshToken != "" {
		h.cookie.set(writer, result.RefreshToken)
	}
	response := struct {
		User        interface{} `json:"user,omitempty"`
		AccessToken string      `json:"access_token"`
		ExpiresIn   int64       `json:"expires_in"`
	}{AccessToken: result.AccessToken, ExpiresIn: result.ExpiresIn}
	if result.User != nil {
		response.User = result.User.Public()
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(response)
}
