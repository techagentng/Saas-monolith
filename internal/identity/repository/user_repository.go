package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/identity/model"
)

type UserRepository interface {
	Create(ctx context.Context, user model.User) (*model.User, error)
	FindByID(ctx context.Context, id string) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
}
