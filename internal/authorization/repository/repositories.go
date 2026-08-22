package repository

import (
	"context"
	"github.com/techagentng/saas-monolith/internal/authorization/model"
	"time"
)

type RoleRepository interface {
	FindByID(context.Context, string) (*model.Role, error)
	FindByNameScope(context.Context, string, model.Scope) (*model.Role, error)
}
type PermissionRepository interface {
	FindByID(context.Context, string) (*model.Permission, error)
	FindByCode(context.Context, string) (*model.Permission, error)
}
type RolePermissionRepository interface {
	Assign(context.Context, model.RolePermission) error
	Remove(context.Context, string, string) error
	ListByRole(context.Context, string) ([]model.Permission, error)
}
type UserRoleRepository interface {
	Assign(context.Context, model.UserRole) (*model.UserRole, error)
	Remove(context.Context, string) error
	ListByUserScope(context.Context, string, model.Scope, string) ([]model.Role, error)
}
type Clock interface{ Now() time.Time }
