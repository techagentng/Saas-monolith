package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/techagentng/saas-monolith/internal/authorization/model"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

type PostgresRoleRepository struct{ db *sql.DB }

func NewPostgresRoleRepository(db *sql.DB) *PostgresRoleRepository {
	return &PostgresRoleRepository{db: db}
}
func (r *PostgresRoleRepository) FindByID(ctx context.Context, id string) (*model.Role, error) {
	var role model.Role
	err := r.db.QueryRowContext(ctx, `SELECT id, name, description, scope, created_at, updated_at FROM roles WHERE id = $1`, id).Scan(&role.ID, &role.Name, &role.Description, &role.Scope, &role.CreatedAt, &role.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeRoleNotFound, "role not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding role: %w", err)
	}
	return &role, nil
}
func (r *PostgresRoleRepository) FindByNameScope(ctx context.Context, name string, scope model.Scope) (*model.Role, error) {
	var role model.Role
	err := r.db.QueryRowContext(ctx, `SELECT id, name, description, scope, created_at, updated_at FROM roles WHERE name = $1 AND scope = $2`, name, scope).Scan(&role.ID, &role.Name, &role.Description, &role.Scope, &role.CreatedAt, &role.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeRoleNotFound, "role not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding role: %w", err)
	}
	return &role, nil
}

type PostgresRolePermissionRepository struct{ db *sql.DB }

func NewPostgresRolePermissionRepository(db *sql.DB) *PostgresRolePermissionRepository {
	return &PostgresRolePermissionRepository{db: db}
}
func (r *PostgresRolePermissionRepository) Assign(ctx context.Context, relation model.RolePermission) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2)`, relation.RoleID, relation.PermissionID)
	if isUnique(err) {
		return apperrors.New(apperrors.CodeRoleAssignmentAlreadyExists, "role permission already exists", err)
	}
	if err != nil {
		return fmt.Errorf("assigning permission: %w", err)
	}
	return nil
}
func (r *PostgresRolePermissionRepository) Remove(ctx context.Context, roleID, permissionID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = $1 AND permission_id = $2`, roleID, permissionID)
	if err != nil {
		return fmt.Errorf("removing permission: %w", err)
	}
	return nil
}
func (r *PostgresRolePermissionRepository) ListByRole(ctx context.Context, roleID string) ([]model.Permission, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT p.id, p.code, p.description, p.created_at FROM permissions p JOIN role_permissions rp ON rp.permission_id = p.id WHERE rp.role_id = $1 ORDER BY p.code`, roleID)
	if err != nil {
		return nil, fmt.Errorf("listing role permissions: %w", err)
	}
	defer rows.Close()
	var result []model.Permission
	for rows.Next() {
		var permission model.Permission
		if err := rows.Scan(&permission.ID, &permission.Code, &permission.Description, &permission.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning permission: %w", err)
		}
		result = append(result, permission)
	}
	return result, rows.Err()
}

type PostgresPermissionRepository struct{ db *sql.DB }

func NewPostgresPermissionRepository(db *sql.DB) *PostgresPermissionRepository {
	return &PostgresPermissionRepository{db: db}
}
func (r *PostgresPermissionRepository) FindByID(ctx context.Context, id string) (*model.Permission, error) {
	var permission model.Permission
	err := r.db.QueryRowContext(ctx, `SELECT id, code, description, created_at FROM permissions WHERE id = $1`, id).Scan(&permission.ID, &permission.Code, &permission.Description, &permission.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodePermissionNotFound, "permission not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding permission: %w", err)
	}
	return &permission, nil
}
func (r *PostgresPermissionRepository) FindByCode(ctx context.Context, code string) (*model.Permission, error) {
	var permission model.Permission
	err := r.db.QueryRowContext(ctx, `SELECT id, code, description, created_at FROM permissions WHERE code = $1`, code).Scan(&permission.ID, &permission.Code, &permission.Description, &permission.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodePermissionNotFound, "permission not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding permission: %w", err)
	}
	return &permission, nil
}

type PostgresUserRoleRepository struct{ db *sql.DB }

func NewPostgresUserRoleRepository(db *sql.DB) *PostgresUserRoleRepository {
	return &PostgresUserRoleRepository{db: db}
}
func (r *PostgresUserRoleRepository) Assign(ctx context.Context, assignment model.UserRole) (*model.UserRole, error) {
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_roles (id, user_id, role_id, tenant_id, created_at) VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5)`, assignment.ID, assignment.UserID, assignment.RoleID, assignment.TenantID, assignment.CreatedAt)
	if isUnique(err) {
		return nil, apperrors.New(apperrors.CodeRoleAssignmentAlreadyExists, "role assignment already exists", err)
	}
	if err != nil {
		return nil, fmt.Errorf("assigning role: %w", err)
	}
	return &assignment, nil
}
func (r *PostgresUserRoleRepository) Remove(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_roles WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("removing role assignment: %w", err)
	}
	return nil
}
func (r *PostgresUserRoleRepository) ListByUserScope(ctx context.Context, userID string, scope model.Scope, tenantID string) ([]model.Role, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT r.id, r.name, r.description, r.scope, r.created_at, r.updated_at FROM roles r JOIN user_roles ur ON ur.role_id = r.id WHERE ur.user_id = $1 AND r.scope = $2 AND ((r.scope = 'PLATFORM' AND ur.tenant_id IS NULL) OR (r.scope = 'TENANT' AND ur.tenant_id = $3)) ORDER BY r.name`, userID, scope, tenantID)
	if err != nil {
		return nil, fmt.Errorf("listing user roles: %w", err)
	}
	defer rows.Close()
	var result []model.Role
	for rows.Next() {
		var role model.Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.Scope, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning role: %w", err)
		}
		result = append(result, role)
	}
	return result, rows.Err()
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
