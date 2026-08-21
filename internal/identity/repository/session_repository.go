package repository

import (
	"context"
	"time"

	"github.com/techagentng/saas-monolith/internal/identity/model"
)

type SessionRepository interface {
	Create(ctx context.Context, session model.Session) (*model.Session, error)
	Rotate(ctx context.Context, refreshTokenHash, replacementHash string, now time.Time) (*model.Session, error)
	Revoke(ctx context.Context, sessionID string) error
	FindActive(ctx context.Context, userID, sessionID string, now time.Time) (*model.Session, error)
}
