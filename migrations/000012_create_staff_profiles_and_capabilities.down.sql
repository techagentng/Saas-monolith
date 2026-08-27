-- Reverses ONLY the schema 000012 added. The permission catalog it pairs with
-- lives in 000013 and is reversed by that migration's own down file.
--
-- staff_services first: it holds composite foreign keys into staff_profiles.
DROP TABLE IF EXISTS staff_services;

DROP TABLE IF EXISTS staff_profiles;
