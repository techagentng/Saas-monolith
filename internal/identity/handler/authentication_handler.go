package handler

import (
	"encoding/json"
	"net/http"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

type AuthenticationHandler struct{ service service.AuthenticationService }

func NewAuthenticationHandler(authenticationService service.AuthenticationService) *AuthenticationHandler {
	return &AuthenticationHandler{service: authenticationService}
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
	writeAuthenticationResult(writer, result)
}

func (h *AuthenticationHandler) Refresh(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		_ = apperrors.Map(apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)).WriteJSON(writer)
		return
	}
	result, err := h.service.Refresh(request.Context(), input.RefreshToken)
	if err != nil {
		_ = apperrors.Map(err).WriteJSON(writer)
		return
	}
	writeAuthenticationResult(writer, result)
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
	writer.WriteHeader(http.StatusNoContent)
}

func writeAuthenticationResult(writer http.ResponseWriter, result *service.AuthenticationResult) {
	response := struct {
		User         interface{} `json:"user,omitempty"`
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		ExpiresIn    int64       `json:"expires_in"`
	}{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresIn: result.ExpiresIn}
	if result.User != nil {
		response.User = result.User.Public()
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(response)
}
