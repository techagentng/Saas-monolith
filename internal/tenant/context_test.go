package tenant

import (
	"encoding/json"
	"testing"
)

func TestTenantContextUsesPublicTenantIDField(t *testing.T) {
	encoded, err := json.Marshal(TenantContext{TenantID: "550e8400-e29b-41d4-a716-446655440000"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(encoded) != `{"tenant_id":"550e8400-e29b-41d4-a716-446655440000"}` {
		t.Fatalf("JSON = %s", encoded)
	}
}
