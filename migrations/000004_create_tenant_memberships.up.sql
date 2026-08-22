CREATE TABLE tenant_memberships (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID NOT NULL REFERENCES users(id),
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT tenant_memberships_status_valid CHECK (status IN ('ACTIVE', 'DISABLED')),
    CONSTRAINT tenant_memberships_tenant_user_unique UNIQUE (tenant_id, user_id)
);

CREATE INDEX tenant_memberships_user_id_idx ON tenant_memberships (user_id);
CREATE INDEX tenant_memberships_tenant_id_idx ON tenant_memberships (tenant_id);