-- Scheduling S11 permission catalog: owner/staff booking management. Split from
-- the schema migration for the same reason 000013 is split from 000012 and
-- 000011 from 000010 — table creation and catalog seeding have different
-- prerequisites, and a fixture can build the tables it needs without the RBAC
-- catalog.
INSERT INTO permissions (id, code, description) VALUES
('660e8400-e29b-41d4-a716-446655440022', 'booking.read', 'Read tenant bookings'),
('660e8400-e29b-41d4-a716-446655440023', 'booking.update', 'Cancel and manage tenant bookings');

-- Every grant is listed explicitly. Migration 000006 granted SUPER_ADMIN the
-- whole catalog via INSERT ... SELECT id FROM permissions, which was a one-time
-- snapshot: permissions added afterwards are not picked up by it, so a wildcard
-- here would both re-grant everything and hide that drift.
--
-- Granting SUPER_ADMIN these permissions confers no tenant access: it is a
-- PLATFORM-scoped role, and PermissionResolutionService.ResolveTenant reads only
-- TENANT-scoped roles, so a SUPER_ADMIN still resolves to zero permissions
-- inside any tenant. The grants exist for catalog consistency.
--
-- BUSINESS_OWNER gets both. STAFF gets booking.read only: a technician needs to
-- see the day's appointments to do their job, but cancelling a customer's
-- booking is an owner decision — the same read-only shape STAFF already has for
-- staff.* and service.*.
INSERT INTO role_permissions (role_id, permission_id)
SELECT role_data.role_id, permissions.id
FROM (VALUES
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'booking.read'),
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'booking.update'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'booking.read'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'booking.update'),
	('650e8400-e29b-41d4-a716-446655440003'::uuid, 'booking.read')
) AS role_data(role_id, permission_code)
JOIN permissions ON permissions.code = role_data.permission_code;
