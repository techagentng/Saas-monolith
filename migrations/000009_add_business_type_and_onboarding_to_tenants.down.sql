ALTER TABLE tenants
    DROP CONSTRAINT tenants_business_type_valid,
    DROP CONSTRAINT tenants_onboarding_status_valid;

ALTER TABLE tenants
    DROP COLUMN business_type,
    DROP COLUMN onboarding_status,
    DROP COLUMN onboarding_step;
