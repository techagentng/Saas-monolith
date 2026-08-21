package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
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

func (h *UserHandler) GetByID(writer http.ResponseWriter, request *http.Request, id string) {
	if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
		writeError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", nil))
		return
	}
	user, err := h.service.FindByID(request.Context(), id)
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
