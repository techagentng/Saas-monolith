package model

import "time"

type Session struct {
	ID               string
	UserID           string
	RefreshTokenHash string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ExpiresAt        time.Time
	RevokedAt        *time.Time
}

func (s Session) Expired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}

func (s Session) Revoked() bool {
	return s.RevokedAt != nil
}
