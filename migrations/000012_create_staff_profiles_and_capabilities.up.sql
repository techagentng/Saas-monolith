-- Scheduling S3: bookable staff, and which services each of them can perform.
--
-- A staff profile is deliberately NOT a role. STAFF (the RBAC role) answers
-- "what may this person do in the workspace"; a staff profile answers "is this
-- person a schedulable resource". A BUSINESS_OWNER who personally performs
-- services gets a profile without being demoted to STAFF, and a technician who
-- never signs in gets a profile with no user at all.
--
-- user_id is nullable because a non-login worker has no users row, and creating
-- one would mean fabricating a unique email and a password hash. A nullable
-- foreign key is a far smaller lie than a synthetic account.
CREATE TABLE staff_profiles (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID NULL REFERENCES users(id),
    display_name VARCHAR(255) NOT NULL,
    bio TEXT,
    -- is_bookable is separate from status on purpose: a receptionist is an
    -- ACTIVE profile who is not bookable, while a departed technician is
    -- ARCHIVED. Collapsing them would make "not taking appointments this month"
    -- indistinguishable from "no longer works here".
    is_bookable BOOLEAN NOT NULL DEFAULT true,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT staff_profiles_status_valid CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    -- Redundant for uniqueness (id is the primary key) and required anyway:
    -- PostgreSQL only accepts a composite foreign key whose referenced columns
    -- carry a unique constraint of exactly that shape. staff_services below is
    -- the child that needs it.
    CONSTRAINT staff_profiles_id_tenant_unique UNIQUE (id, tenant_id)
);

-- Not served by staff_profiles_id_tenant_unique, which leads with id: every
-- roster read filters on tenant_id.
CREATE INDEX staff_profiles_tenant_id_idx ON staff_profiles (tenant_id);

-- One profile per linked user per tenant, while leaving non-login profiles
-- (user_id IS NULL) unconstrained — many of them may coexist. The partial-unique
-- shape is not invented here: user_roles already uses it twice
-- (user_roles_platform_unique / user_roles_tenant_unique) to express exactly
-- "unique when this nullable column is set".
CREATE UNIQUE INDEX staff_profiles_tenant_user_unique ON staff_profiles (tenant_id, user_id) WHERE user_id IS NOT NULL;

-- staff_services is pure capability: the row's existence is the fact. There is
-- no status column (a capability is not a lifecycle), no price or duration
-- override, no proficiency, and no primary-service flag — all deferred, and all
-- additive later if they are ever justified.
--
-- The composite foreign keys are the point of this table. With UNIQUE
-- (id, tenant_id) on both parents, the DATABASE refuses to link one tenant's
-- technician to another tenant's service — the guarantee holds against a future
-- admin tool, a migration, or a manual psql session, not only against the Go
-- service layer. Two independent single-column foreign keys would permit
-- exactly that cross-tenant row, which is why they are not used here.
CREATE TABLE staff_services (
    staff_id UUID NOT NULL,
    service_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (staff_id, service_id),
    CONSTRAINT staff_services_staff_tenant_fkey FOREIGN KEY (staff_id, tenant_id) REFERENCES staff_profiles(id, tenant_id),
    CONSTRAINT staff_services_service_tenant_fkey FOREIGN KEY (service_id, tenant_id) REFERENCES services(id, tenant_id)
);

-- PostgreSQL does not index foreign keys automatically, and the primary key
-- leads with staff_id, so "which staff can perform this service" — the query
-- the availability engine will run in S7 — has no index without this one.
CREATE INDEX staff_services_service_id_idx ON staff_services (service_id);
