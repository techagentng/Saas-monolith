package model

import "time"

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusDisabled Status = "DISABLED"
)

type Tenant struct {
	ID           string
	Name         string
	Slug         string
	Status       Status
	Description  *string
	ContactEmail *string
	ContactPhone *string
	Timezone     *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
