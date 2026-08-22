package model

import "time"

type MembershipStatus string

const (
	MembershipStatusActive   MembershipStatus = "ACTIVE"
	MembershipStatusDisabled MembershipStatus = "DISABLED"
)

type TenantMembership struct {
	ID        string
	TenantID  string
	UserID    string
	Status    MembershipStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}
