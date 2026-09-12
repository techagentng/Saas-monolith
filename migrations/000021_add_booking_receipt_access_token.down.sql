ALTER TABLE bookings DROP CONSTRAINT bookings_receipt_access_token_unique;
ALTER TABLE bookings DROP COLUMN receipt_access_token;
