-- Reverses ONLY the schema 000010 added. The permission catalog it used to
-- carry now lives in 000011 and is reversed by that migration's own down file.
DROP TABLE IF EXISTS services;

ALTER TABLE tenants
    DROP CONSTRAINT IF EXISTS tenants_currency_format_valid;

ALTER TABLE tenants
    DROP COLUMN IF EXISTS currency;
