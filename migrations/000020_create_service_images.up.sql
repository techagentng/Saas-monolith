-- Service Images: per-service uploaded photos. Metadata only — the bytes
-- live in whatever internal/media.MediaStorage implementation is configured
-- (local disk today; see internal/config's MediaStorageDriver), never in this
-- table. storage_key is this row's handle back to that object; public_url is
-- the fully-qualified, directly-fetchable address the public booking page
-- renders — computed once at upload time so no request-time URL-building
-- logic needs to agree with the storage layer about how a key becomes a URL.
--
-- Tenant safety mirrors service_categories (migration 000019) exactly: a
-- composite FK on (service_id, tenant_id) referencing services(id, tenant_id)
-- makes it impossible, at the schema level — not merely by service-layer
-- discipline — for an image whose tenant_id names one tenant to be attached
-- to a service belonging to a different one.
--
-- No migration/backfill of the services table itself: this is a wholly new,
-- initially-empty table, so every existing service simply has zero rows here
-- and keeps working exactly as before.
CREATE TABLE service_images (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    service_id UUID NOT NULL,
    storage_key TEXT NOT NULL,
    public_url TEXT NOT NULL,
    alt_text VARCHAR(255),
    sort_order INT NOT NULL DEFAULT 0,
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    -- Mirrors the server-side allow-list (internal/scheduling/model's image
    -- validation) at the schema level, the same belt-and-suspenders reasoning
    -- service_categories_name_present uses: a future admin tool or manual
    -- psql session cannot silently record an SVG or an arbitrary content type.
    mime_type VARCHAR(100) NOT NULL,
    file_size BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT service_images_mime_type_valid CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    CONSTRAINT service_images_file_size_valid CHECK (file_size > 0),
    -- Parent side of the FK below requires services(id, tenant_id) to carry a
    -- unique constraint of exactly that shape — already true since migration
    -- 000010 (services_id_tenant_unique), added for S3's staff_services.
    CONSTRAINT service_images_service_tenant_fkey
        FOREIGN KEY (service_id, tenant_id) REFERENCES services (id, tenant_id)
);

-- Not served by the composite FK's own index (which leads with service_id, so
-- it already covers plain service_id lookups) — this one is for a
-- tenant-wide query that never names a specific service.
CREATE INDEX service_images_tenant_id_idx ON service_images (tenant_id);
CREATE INDEX service_images_service_id_idx ON service_images (service_id);
-- The one query every read path actually runs: "this service's images, in
-- display order."
CREATE INDEX service_images_service_sort_idx ON service_images (service_id, sort_order);

-- At most one primary image per service. Scoped to service_id alone (not
-- tenant_id + service_id) because "primary" is a per-service concept, not a
-- per-tenant one — a partial index, the same shape
-- service_categories_tenant_name_unique already uses, so only PRIMARY rows
-- compete for this uniqueness; a service with zero or many non-primary
-- images is unaffected.
CREATE UNIQUE INDEX service_images_service_primary_unique ON service_images (service_id) WHERE is_primary = TRUE;
