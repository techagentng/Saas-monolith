-- Scheduling S1: the appointment-style service catalog, plus the tenant
-- currency a service price is denominated in.
--
-- currency is nullable and is NOT backfilled: no currency can be safely
-- inferred for a tenant created before this migration, and guessing one would
-- silently reinterpret every price later entered against it. A tenant supplies
-- its currency explicitly (PUT /api/v1/tenants/{tenantID}/currency) before its
-- first priced service can be created.
--
-- The CHECK guards the column's SHAPE only, not membership of the supported
-- set. The allow-list itself lives in internal/money (ValidateCurrency) because
-- it is product configuration expected to grow; encoding it here would make
-- adding a currency a schema migration.
ALTER TABLE tenants
    ADD COLUMN currency CHAR(3);

ALTER TABLE tenants
    ADD CONSTRAINT tenants_currency_format_valid CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$');

-- services is tenant-scoped from row one. Rows are archived, never deleted:
-- appointments will hold a real foreign key to this table from S10 onward, so a
-- DELETE would either fail or orphan booking history.
--
-- duration_minutes upper bound (480 = 8h) bounds future slot-generation cost
-- and catches the classic unit error of entering seconds. price_minor upper
-- bound (1,000,000 major units) catches the same class of error for money.
-- Both are deliberately generous; neither encodes a product rule finer than
-- "this value is implausible".
CREATE TABLE services (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    duration_minutes INT NOT NULL,
    price_minor BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT services_status_valid CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    CONSTRAINT services_duration_valid CHECK (duration_minutes > 0 AND duration_minutes <= 480),
    CONSTRAINT services_price_valid CHECK (price_minor >= 0 AND price_minor <= 100000000),
    -- Redundant for uniqueness (id is already the primary key), and required
    -- anyway: PostgreSQL will only accept a composite foreign key whose
    -- referenced columns carry a unique constraint of exactly that shape. S3's
    -- staff_services join uses FOREIGN KEY (service_id, tenant_id) to make
    -- linking one tenant's staff to another tenant's service impossible at the
    -- schema level, and this is the parent side of that guarantee.
    CONSTRAINT services_id_tenant_unique UNIQUE (id, tenant_id)
);

-- Not duplicated by services_id_tenant_unique: that index leads with id, so it
-- cannot serve a tenant-scoped list. Every catalog read filters on tenant_id.
CREATE INDEX services_tenant_id_idx ON services (tenant_id);
