-- Reverses ONLY what 000020 added. Dropping the table cascades its own
-- indexes and constraints; there is nothing on the services table itself to
-- reverse, since 000020 never altered it.
DROP TABLE IF EXISTS service_images;
