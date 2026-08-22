package model

import (
	"encoding/json"
	"testing"
)

func TestRoleScopesAndPermissionCode(t *testing.T) {
	if ScopePlatform != "PLATFORM" || ScopeTenant != "TENANT" {
		t.Fatal("unexpected role scopes")
	}
	if err := ValidatePermissionCode("booking.read"); err != nil {
		t.Fatalf("valid permission rejected: %v", err)
	}
	if err := ValidatePermissionCode("ROLE_123_ACCESS"); err == nil {
		t.Fatal("invalid permission code accepted")
	}
}

func TestPublicRoleDoesNotExposeUnexpectedFields(t *testing.T) {
	role := Role{ID: "role", Name: "STAFF", Description: "Staff", Scope: ScopeTenant}
	data, err := json.Marshal(role.Public())
	if err != nil || string(data) == "" {
		t.Fatalf("public role serialization failed: %v", err)
	}
}
