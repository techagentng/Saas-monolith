package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

type MembershipHandler struct{ service service.MembershipService }

type PublicMembership struct {
	ID        string                 `json:"id"`
	TenantID  string                 `json:"tenant_id"`
	UserID    string                 `json:"user_id"`
	Status    model.MembershipStatus `json:"status"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

func NewMembershipHandler(membershipService service.MembershipService) *MembershipHandler {
	return &MembershipHandler{service: membershipService}
}

func (h *MembershipHandler) Create(writer http.ResponseWriter, request *http.Request, tenantID string) {
	var input struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}
	membership, err := h.service.Create(request.Context(), service.CreateMembershipInput{TenantID: tenantID, UserID: input.UserID})
	if err != nil {
		writeError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(writer).Encode(PublicMembership{
		ID: membership.ID, TenantID: membership.TenantID, UserID: membership.UserID,
		Status: membership.Status, CreatedAt: membership.CreatedAt, UpdatedAt: membership.UpdatedAt,
	})
}

func (h *MembershipHandler) Revoke(writer http.ResponseWriter, request *http.Request, tenantID, userID string) {
	if _, err := uuid.Parse(tenantID); err != nil {
		writeError(writer, err)
		return
	}
	if err := h.service.Revoke(request.Context(), tenantID, userID); err != nil {
		writeError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func writeError(writer http.ResponseWriter, err error) { _ = applicationError(err).WriteJSON(writer) }
