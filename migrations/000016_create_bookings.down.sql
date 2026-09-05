-- Reverses ONLY what 000016 added. The bookings table (with its indexes and
-- the bookings_no_overlap exclusion constraint) is dropped; btree_gist is
-- deliberately left in place — an extension a later migration may have come to
-- depend on must not be removed by an unrelated rollback.
DROP TABLE IF EXISTS bookings;
