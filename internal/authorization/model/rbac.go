package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Scope string

const (
	ScopePlatform Scope = "PLATFORM"
	ScopeTenant   Scope = "TENANT"
)

type Role struct {
	ID, Name, Description string
	Scope                 Scope
	CreatedAt, UpdatedAt  time.Time
}
type Permission struct {
	ID, Code, Description string
	CreatedAt             time.Time
}
type RolePermission struct {
	RoleID, PermissionID string
	CreatedAt            time.Time
}
type UserRole struct {
	ID, UserID, RoleID, TenantID string
	CreatedAt                    time.Time
	Platform                     bool
}

type PublicRole struct {
	ID, Name, Description string
	Scope                 Scope
	CreatedAt, UpdatedAt  time.Time
}

func (r Role) Public() PublicRole {
	return PublicRole{r.ID, r.Name, r.Description, r.Scope, r.CreatedAt, r.UpdatedAt}
}

var permissionCodePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)

func ValidatePermissionCode(code string) error {
	if !permissionCodePattern.MatchString(code) || strings.TrimSpace(code) != code {
		return fmt.Errorf("invalid permission code")
	}
	return nil
}
