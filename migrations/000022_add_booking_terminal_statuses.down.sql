-- Reverses ONLY what 000022 added. This will fail if any row has since been
-- set to COMPLETED or NO_SHOW — the same "the down migration assumes the
-- widened state was never actually used" caveat every additive-constraint
-- rollback in this project carries.
ALTER TABLE bookings DROP CONSTRAINT bookings_status_valid;
ALTER TABLE bookings ADD CONSTRAINT bookings_status_valid
    CHECK (status IN ('CONFIRMED', 'CANCELLED'));
