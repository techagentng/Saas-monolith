package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization/model"
	authzservice "github.com/techagentng/saas-monolith/internal/authorization/service"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

func TestAssignRoleReturnsPublicRepresentationOnSuccess(t *testing.T) {
	assignment := &model.UserRole{ID: "assignment", UserID: "user", RoleID: "role", TenantID: "tenant"}
	handler := NewRoleAssignmentHandler(&tenantRoleAssignmentServiceFake{assignment: assignment})
	recorder := httptest.NewRecorder()
	request := requestWithActorContext(http.MethodPost, "/api/v1/tenants/tenant/role-assignments", `{"user_id":"user","role_id":"role"}`)

	handler.Assign(recorder, request, "tenant")

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", recorder.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["user_id"] != "user" || body["role_id"] != "role" || body["tenant_id"] != "tenant" {
		t.Fatalf("response = %s", recorder.Body.Bytes())
	}
}

func TestAssignRolePropagatesPolicyDenial(t *testing.T) {
	handler := NewRoleAssignmentHandler(&tenantRoleAssignmentServiceFake{err: apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)})
	recorder := httptest.NewRecorder()
	request := requestWithActorContext(http.MethodPost, "/api/v1/tenants/tenant/role-assignments", `{"user_id":"user","role_id":"role"}`)

	handler.Assign(recorder, request, "tenant")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestAssignRoleRejectsInvalidBody(t *testing.T) {
	handler := NewRoleAssignmentHandler(&tenantRoleAssignmentServiceFake{})
	recorder := httptest.NewRecorder()
	request := requestWithActorContext(http.MethodPost, "/api/v1/tenants/tenant/role-assignments", `not-json`)

	handler.Assign(recorder, request, "tenant")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func requestWithActorContext(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := auth.WithPrincipal(request.Context(), auth.Principal{UserID: "actor"})
	ctx = tenant.WithContext(ctx, tenant.TenantContext{TenantID: "tenant"})
	return request.WithContext(ctx)
}

type tenantRoleAssignmentServiceFake struct {
	assignment *model.UserRole
	err        error
}

func (s *tenantRoleAssignmentServiceFake) AssignTenantRole(_ context.Context, _ auth.Principal, _ tenant.TenantContext, _, _ string) (*model.UserRole, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.assignment, nil
}

var _ authzservice.TenantRoleAssignmentService = (*tenantRoleAssignmentServiceFake)(nil)
