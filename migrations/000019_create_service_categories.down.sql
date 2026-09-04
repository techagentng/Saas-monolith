-- Reverses ONLY what 000019 added. Drop the FK and column on services before
-- the parent table.
DROP INDEX IF EXISTS services_tenant_category_idx;
ALTER TABLE services DROP CONSTRAINT IF EXISTS services_category_tenant_fkey;
ALTER TABLE services DROP COLUMN IF EXISTS category_id;
DROP TABLE IF EXISTS service_categories;
