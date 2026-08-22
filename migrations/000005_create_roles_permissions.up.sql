CREATE TABLE roles (
    id UUID PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    scope VARCHAR(16) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT roles_scope_valid CHECK (scope IN ('PLATFORM', 'TENANT')),
    CONSTRAINT roles_name_scope_unique UNIQUE (name, scope)
);

CREATE TABLE permissions (
    id UUID PRIMARY KEY,
    code VARCHAR(128) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id),
    permission_id UUID NOT NULL REFERENCES permissions(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE user_roles (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    role_id UUID NOT NULL REFERENCES roles(id),
    tenant_id UUID NULL REFERENCES tenants(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX user_roles_user_scope_idx ON user_roles (user_id, tenant_id);
CREATE INDEX role_permissions_permission_idx ON role_permissions (permission_id);
CREATE UNIQUE INDEX user_roles_platform_unique ON user_roles (user_id, role_id) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX user_roles_tenant_unique ON user_roles (user_id, role_id, tenant_id) WHERE tenant_id IS NOT NULL;