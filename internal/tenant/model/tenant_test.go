package model

import "testing"

func TestTenantStatuses(t *testing.T) {
	if StatusActive != "ACTIVE" || StatusDisabled != "DISABLED" {
		t.Fatalf("tenant statuses = %q, %q", StatusActive, StatusDisabled)
	}
}
