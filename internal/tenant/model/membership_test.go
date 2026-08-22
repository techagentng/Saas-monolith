package model

import "testing"

func TestMembershipStatuses(t *testing.T) {
	if MembershipStatusActive != "ACTIVE" || MembershipStatusDisabled != "DISABLED" {
		t.Fatalf("membership statuses = %q, %q", MembershipStatusActive, MembershipStatusDisabled)
	}
}
