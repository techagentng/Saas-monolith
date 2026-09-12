-- S12-BE: a secure public access token for retrieving one booking's receipt.
--
-- Migration 000016's own doc comment on Booking explains why there is no
-- price snapshot; the same "do not invent a contract nothing consumes"
-- reasoning does NOT apply here — a receipt genuinely cannot be retrieved
-- publicly without SOME secret, and the existing "reference" (booking_service.go's
-- bookingReference, "NB-XXXXXXXX") is display-only: it is derived from the
-- booking id's own first 8 hex characters (32 bits), is not stored, and
-- "nothing looks a booking up by reference" by the service's own doc comment.
-- 32 bits and a deterministic derivation from an id already handed to the
-- client (see the existing public booking creation response, which already
-- echoes the raw booking id) is not a safe public lookup secret on its own.
--
-- receipt_access_token is a separate, independently random 256-bit value
-- (32 bytes, hex-encoded by the Go layer going forward), generated once at
-- booking creation and never regenerated or rotated. It is the SOLE secret a
-- public receipt request must present; the reference in the URL is a
-- readability nicety, re-validated against it but never authoritative alone.
--
-- Added nullable-then-backfilled-then-NOT-NULL so this is safe to run against
-- a production table that may already hold real bookings: two concatenated
-- gen_random_uuid() values give the same 256 bits of entropy the Go layer
-- will use for every booking created after this migration, with no pgcrypto
-- extension dependency (gen_random_uuid() is core Postgres since v13).
ALTER TABLE bookings ADD COLUMN receipt_access_token TEXT;

UPDATE bookings
SET receipt_access_token = replace(gen_random_uuid()::text || gen_random_uuid()::text, '-', '')
WHERE receipt_access_token IS NULL;

ALTER TABLE bookings ALTER COLUMN receipt_access_token SET NOT NULL;

-- Uniqueness is the access-control invariant: two bookings must never share a
-- token. Postgres creates the supporting index automatically, which is also
-- what makes FindByTenantAndReceiptToken's lookup an index scan rather than a
-- sequential one.
ALTER TABLE bookings ADD CONSTRAINT bookings_receipt_access_token_unique UNIQUE (receipt_access_token);
