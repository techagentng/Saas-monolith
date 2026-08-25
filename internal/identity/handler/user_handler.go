package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

type UserHandler struct {
	service service.UserService
}

func NewUserHandler(userService service.UserService) *UserHandler {
	return &UserHandler{service: userService}
}

func (h *UserHandler) Create(writer http.ResponseWriter, request *http.Request) {
	var input service.CreateUserInput
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}
	user, err := h.service.Create(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeUser(writer, http.StatusCreated, user)
}

// GetByID is self-only: an authenticated caller may retrieve only their own
// user record. There is no tenant context on this route to scope a
// cross-user lookup safely, and no established Epic 01 policy grants any
// role blanket cross-tenant user reads (the seeded user.read permission is
// not wired to a concrete authorization check anywhere in this codebase), so
// looking up anyone else's ID — same tenant or not — is denied identically
// to a nonexistent ID. This route must never reveal whether an arbitrary
// UUID belongs to a real account.
func (h *UserHandler) GetByID(writer http.ResponseWriter, request *http.Request, id string) {
	principal, ok := auth.FromContext(request.Context())
	if !ok {
		writeError(writer, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil))
		return
	}
	trimmed := strings.TrimSpace(id)
	if _, err := uuid.Parse(trimmed); err != nil {
		writeError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", nil))
		return
	}
	if principal.UserID != trimmed {
		writeError(writer, apperrors.New(apperrors.CodeUserNotFound, "user not found", nil))
		return
	}
	user, err := h.service.FindByID(request.Context(), trimmed)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeUser(writer, http.StatusOK, user)
}

func writeUser(writer http.ResponseWriter, status int, user *model.User) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(user.Public())
}

func writeError(writer http.ResponseWriter, err error) {
	_ = apperrors.Map(err).WriteJSON(writer)
}
