-- Scheduling S13-BE: extends the booking lifecycle CHECK constraint
-- (bookings_status_valid, migration 000016) from CONFIRMED/CANCELLED to also
-- allow COMPLETED and NO_SHOW — the two owner-marked terminal outcomes of a
-- past appointment. This does not touch bookings_no_overlap: that constraint
-- already applies WHERE (status = 'CONFIRMED'), so it is completely
-- unaffected by two more statuses that were never CONFIRMED existing.
--
-- No existing row changes: every currently-stored status (CONFIRMED,
-- CANCELLED) remains valid under the widened constraint.
ALTER TABLE bookings DROP CONSTRAINT bookings_status_valid;
ALTER TABLE bookings ADD CONSTRAINT bookings_status_valid
    CHECK (status IN ('CONFIRMED', 'CANCELLED', 'COMPLETED', 'NO_SHOW'));
