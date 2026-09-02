-- Scheduling S5: recurring weekly working hours for a staff profile.
--
-- Deliberately NOT appointment slots, breaks, holidays, one-off exceptions, or
-- an availability calculation — this table records only "on Mondays this
-- person generally works 09:00-17:00", the same way a paper roster would. The
-- availability engine (S7+) reads this table; it is not written here.
--
-- One row per working interval rather than one row per staff/day, so a split
-- shift (09:00-12:00, 13:00-17:00) is representable without a second schema
-- later. A day with no rows means "unavailable that day" — there is no
-- separate flag for it, matching how an owner would describe a closed day: the
-- absence of hours, not a boolean.
CREATE TABLE staff_working_hours (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    staff_id UUID NOT NULL,
    -- A closed vocabulary, the same string-enum discipline as
    -- staff_profiles.status: mirrored by a CHECK constraint so no writer can
    -- bypass it, spelled out rather than 0-6 so a row is legible in a raw
    -- query without a lookup table.
    day_of_week VARCHAR(9) NOT NULL,
    -- Wall-clock local business time, never a timestamp: a 09:00 start means
    -- 09:00 in the tenant's own timezone (already recorded elsewhere on
    -- tenants), not a UTC instant. Converting this to actual availability
    -- windows is explicitly out of scope for this migration and this feature.
    start_time TIME NOT NULL,
    end_time TIME NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT staff_working_hours_day_valid CHECK (day_of_week IN (
        'MONDAY', 'TUESDAY', 'WEDNESDAY', 'THURSDAY', 'FRIDAY', 'SATURDAY', 'SUNDAY'
    )),
    -- Equal is rejected here too: a zero-width interval is nonsensical
    -- wall-clock hours, and the service layer enforces the identical rule
    -- before this is ever reached — this is the backstop, not the primary
    -- check, the same division used throughout this codebase.
    CONSTRAINT staff_working_hours_time_order_valid CHECK (start_time < end_time),
    -- The tenant-safety mechanism staff_services already established: with
    -- UNIQUE (id, tenant_id) on staff_profiles, PostgreSQL refuses to link a
    -- working-hours row to a staff profile in a DIFFERENT tenant than the one
    -- named on this row — a guarantee that holds against a future admin tool
    -- or a manual psql session, not only against the Go service layer. No
    -- ON DELETE action, matching staff_services: a staff profile is archived,
    -- never deleted, so cascade behavior is never exercised in practice.
    CONSTRAINT staff_working_hours_staff_tenant_fkey FOREIGN KEY (staff_id, tenant_id)
        REFERENCES staff_profiles(id, tenant_id),
    -- Backstops "duplicate identical intervals are rejected" at the database
    -- level. Partial overlap (09:00-12:00 vs 10:00-13:00) is NOT expressible
    -- as a UNIQUE constraint and is enforced by the service layer instead; a
    -- true range-exclusion constraint would need the btree_gist extension,
    -- which this migration does not introduce.
    CONSTRAINT staff_working_hours_unique_interval UNIQUE (staff_id, day_of_week, start_time, end_time)
);

-- Every read is "this staff member's schedule within this tenant" — the same
-- access shape staff_profiles_tenant_id_idx serves for the roster.
CREATE INDEX staff_working_hours_tenant_staff_idx ON staff_working_hours (tenant_id, staff_id);
