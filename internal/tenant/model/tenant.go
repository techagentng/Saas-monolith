package model

import "time"

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusDisabled Status = "DISABLED"
)

type Tenant struct {
	ID        string
	Name      string
	Slug      string
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}
