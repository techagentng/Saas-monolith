-- SC1: tenant-scoped service categories, plus a nullable category_id on
-- services. Model is exactly Category -> Service (no subcategories). The
-- platform suggestion catalogue is static, backend-owned code and gets no
-- table.
--
-- Verified against every existing migration's own conventions (000010, 000012,
-- 000015, 000016): the composite tenant-scoping FK below matches
-- staff_services' pattern exactly, and "no ON DELETE action" matches the
-- archive-not-delete rule used everywhere else in this schema. status was
-- added after that review — every other catalog table (services,
-- staff_profiles) carries ACTIVE/ARCHIVED, and without it a category with
-- services attached could never be hidden, only deleted once empty.

CREATE TABLE service_categories (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(120) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT service_categories_name_present CHECK (length(btrim(name)) > 0),
    CONSTRAINT service_categories_status_valid CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    -- Parent side of the composite FK services.category_id uses.
    CONSTRAINT service_categories_id_tenant_unique UNIQUE (id, tenant_id)
);

-- Case-sensitive, and scoped to ACTIVE only ("Add-Ons" and "add-ons" are
-- distinct, matching service_category.go's stated rule): a full, unscoped
-- unique constraint would permanently block recreating a name the owner had
-- archived, the exact trap services.ValidateName's own comment calls out for
-- why the services table carries no name-uniqueness constraint at all.
-- Partial-unique-index shape is not invented here: staff_profiles already uses
-- it (staff_profiles_tenant_user_unique) for the same "unique among the live
-- rows only" need.
CREATE UNIQUE INDEX service_categories_tenant_name_unique ON service_categories (tenant_id, name) WHERE status = 'ACTIVE';

CREATE INDEX service_categories_tenant_sort_idx ON service_categories (tenant_id, sort_order, name);

-- Nullable; no backfill. Every existing (uncategorised) service keeps working.
ALTER TABLE services ADD COLUMN category_id UUID;

-- Tenant safety at the schema level: a service can only reference a category in
-- its own tenant. No ON DELETE action — a referenced category cannot be
-- deleted.
ALTER TABLE services
    ADD CONSTRAINT services_category_tenant_fkey
    FOREIGN KEY (category_id, tenant_id) REFERENCES service_categories(id, tenant_id);

CREATE INDEX services_tenant_category_idx ON services (tenant_id, category_id);
