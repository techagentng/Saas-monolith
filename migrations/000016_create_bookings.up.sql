-- Scheduling S10: persisted public appointment bookings for the
-- NAIL_TECHNICIAN vertical — the point where an S9 availability snapshot
-- becomes a real commitment on a technician's calendar.
--
-- Deliberately NOT: payment, cancellation/reschedule workflow, customer
-- accounts, reminders, or any non-appointment vertical. Those are later
-- features; this table holds only what S10 needs.

-- btree_gist lets a GiST index carry plain-equality columns (uuid) alongside
-- the range column, which is what the no-overlap exclusion constraint below
-- needs. It ships as a contrib module with the postgres image; IF NOT EXISTS
-- so a re-run is a no-op. The down migration does NOT drop it — an extension
-- another migration may come to rely on is not safe to remove here.
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE bookings (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    service_id UUID NOT NULL,
    staff_id UUID NOT NULL,

    -- Anonymous public booking identity: the smallest useful set. name is
    -- required; phone and email are optional (the product collects a way to
    -- reach the customer, not a full profile). No customer_id — a customer is
    -- not a platform user in this system, and S10 introduces no auth for them.
    customer_name TEXT NOT NULL,
    customer_phone TEXT,
    customer_email TEXT,

    -- Absolute instants, never wall-clock. The service layer converts the
    -- tenant-local (date + start) through the tenant's authoritative IANA
    -- timezone into these; end_at is start_at + the service's authoritative
    -- duration, never a client value. The tenant timezone remains the source
    -- for customer-facing display, recomputed from these instants.
    start_at TIMESTAMPTZ NOT NULL,
    end_at TIMESTAMPTZ NOT NULL,

    -- Small lifecycle by design. CONFIRMED is the only value S10 writes;
    -- CANCELLED is defined now (not later) purely so the exclusion constraint
    -- can be partial from the start — retrofitting a WHERE predicate onto an
    -- existing exclusion constraint is a lock-heavy rebuild. S11+ owns the
    -- transition to CANCELLED and any further states.
    status VARCHAR(20) NOT NULL DEFAULT 'CONFIRMED',

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT bookings_status_valid CHECK (status IN ('CONFIRMED', 'CANCELLED')),
    -- Equal is rejected: a zero-length appointment is nonsensical, and the
    -- service layer enforces the same rule before this is reached.
    CONSTRAINT bookings_time_order_valid CHECK (start_at < end_at),
    CONSTRAINT bookings_customer_name_present CHECK (length(btrim(customer_name)) > 0),

    -- The tenant-safety mechanism staff_services and staff_working_hours
    -- already use: with UNIQUE (id, tenant_id) on both parents, PostgreSQL
    -- refuses to attach a booking to a service or staff profile in a
    -- DIFFERENT tenant than the one named on this row — a guarantee that holds
    -- against a manual psql session, not only the Go service layer. No
    -- ON DELETE action: catalog and roster rows are archived, never deleted.
    CONSTRAINT bookings_service_tenant_fkey FOREIGN KEY (service_id, tenant_id)
        REFERENCES services(id, tenant_id),
    CONSTRAINT bookings_staff_tenant_fkey FOREIGN KEY (staff_id, tenant_id)
        REFERENCES staff_profiles(id, tenant_id),

    -- THE correctness backstop for concurrency. Two CONFIRMED bookings for the
    -- same staff member may never hold overlapping time. A half-open range
    -- '[)' means a booking ending exactly when another starts is NOT an
    -- overlap — the identical touching-boundary rule S5 working hours and the
    -- S7 engine already use (09:00-09:30 does not block 09:30-10:00).
    --
    -- Concurrent conflicting inserts: the second INSERT blocks on the GiST
    -- index until the first commits, then fails with SQLSTATE 23P01, which the
    -- repository maps to BOOKING_SLOT_UNAVAILABLE. Application pre-checks are
    -- UX; this constraint is correctness.
    CONSTRAINT bookings_no_overlap EXCLUDE USING gist (
        tenant_id WITH =,
        staff_id WITH =,
        tstzrange(start_at, end_at, '[)') WITH &&
    ) WHERE (status = 'CONFIRMED')
);

-- Occupancy lookups are always "this tenant's this staff member around this
-- time" — the S7 OccupancyReader window query. Ordered so a range scan on
-- start_at is index-only-ish.
CREATE INDEX bookings_tenant_staff_start_idx ON bookings (tenant_id, staff_id, start_at);

-- S11 will list a tenant's bookings by day/range across all staff; this serves
-- that without S11 needing a schema change.
CREATE INDEX bookings_tenant_start_idx ON bookings (tenant_id, start_at);
