package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

func TestCreateMembershipReturnsExplicitPublicRepresentation(t *testing.T) {
	membership := &model.TenantMembership{ID: "membership", TenantID: "tenant", UserID: "user", Status: model.MembershipStatusActive}
	handler := NewMembershipHandler(&membershipServiceFake{created: membership})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant/members", strings.NewReader(`{"user_id":"user"}`))

	handler.Create(recorder, request, "tenant")

	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["tenant_id"] != "tenant" || body["user_id"] != "user" || body["status"] != "ACTIVE" {
		t.Fatalf("response = %s", recorder.Body.Bytes())
	}
	if _, ok := body["PasswordHash"]; ok {
		t.Fatal("response contains an internal field")
	}
}

type membershipServiceFake struct{ created *model.TenantMembership }

func (s *membershipServiceFake) Create(context.Context, service.CreateMembershipInput) (*model.TenantMembership, error) {
	return s.created, nil
}
func (*membershipServiceFake) Find(context.Context, string, string) (*model.TenantMembership, error) {
	return nil, nil
}
func (*membershipServiceFake) ListByUser(context.Context, string) ([]model.TenantMembership, error) {
	return nil, nil
}
func (*membershipServiceFake) Revoke(context.Context, string, string) error { return nil }
