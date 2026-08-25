-- business_type is nullable: every tenant created before this migration has
-- no business type and none can be safely inferred, so those rows keep
-- business_type = NULL permanently. Every tenant created going forward is
-- required to supply one at the application layer (tenant/service.tenantService.Create) —
-- the column itself stays nullable to accommodate the legacy rows, not
-- because a new tenant is ever meant to be created without one.
--
-- onboarding_status defaults to COMPLETED so every pre-existing tenant row
-- (all of which predate onboarding entirely and are fully real, in-use
-- tenants) is unaffected by the public-visibility gate a later feature adds
-- on top of this column. Every tenant created through the application layer
-- from this point on is explicitly inserted as IN_PROGRESS, overriding this
-- default — see PostgresTenantRepository.Create. There is no NOT_STARTED
-- value: no tenant row exists until business type, name, and slug are all
-- supplied together, so a tenant is always born IN_PROGRESS.
ALTER TABLE tenants
    ADD COLUMN business_type VARCHAR(32),
    ADD COLUMN onboarding_status VARCHAR(16) NOT NULL DEFAULT 'COMPLETED',
    ADD COLUMN onboarding_step VARCHAR(64);

ALTER TABLE tenants
    ADD CONSTRAINT tenants_onboarding_status_valid CHECK (onboarding_status IN ('IN_PROGRESS', 'COMPLETED'));

ALTER TABLE tenants
    ADD CONSTRAINT tenants_business_type_valid CHECK (business_type IS NULL OR business_type IN ('NAIL_TECHNICIAN', 'RESTAURANT', 'HOTEL', 'TRANSPORT'));
