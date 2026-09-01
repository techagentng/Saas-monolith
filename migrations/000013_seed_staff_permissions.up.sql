-- Scheduling S3 permission catalog. Split from 000012 (which creates the
-- schema) for the same reason 000011 is split from 000010, and 000006 from
-- 000005: table creation and catalog seeding have different prerequisites, and
-- keeping them apart lets a fixture build just the tables it needs without
-- dragging in the RBAC catalog.
INSERT INTO permissions (id, code, description) VALUES
('660e8400-e29b-41d4-a716-446655440018', 'staff.read', 'Read staff profiles'),
('660e8400-e29b-41d4-a716-446655440019', 'staff.create', 'Create staff profiles'),
('660e8400-e29b-41d4-a716-446655440020', 'staff.update', 'Update staff profiles and their service capabilities'),
('660e8400-e29b-41d4-a716-446655440021', 'staff.archive', 'Archive staff profiles');

-- Every grant is listed explicitly. Migration 000006 granted SUPER_ADMIN the
-- whole catalog via INSERT ... SELECT id FROM permissions, which was a one-time
-- snapshot: permissions added afterwards are not picked up by it, so a wildcard
-- here would both re-grant everything and hide that drift.
--
-- Granting SUPER_ADMIN these permissions confers no tenant access: it is a
-- PLATFORM-scoped role, and PermissionResolutionService.ResolveTenant reads only
-- TENANT-scoped roles, so a SUPER_ADMIN still resolves to zero permissions
-- inside any tenant. The grants exist for catalog consistency, not to open a
-- platform path into tenant data.
--
-- STAFF gets staff.read only. A technician needs to see the roster to know who
-- they work with; who is employed, who is bookable, and who can perform what are
-- owner decisions. There is deliberately no staff.assign permission —
-- capability assignment is part of managing a staff profile, so it rides on
-- staff.update rather than inventing a fifth code.
INSERT INTO role_permissions (role_id, permission_id)
SELECT role_data.role_id, permissions.id
FROM (VALUES
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'staff.read'),
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'staff.create'),
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'staff.update'),
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'staff.archive'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'staff.read'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'staff.create'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'staff.update'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'staff.archive'),
	('650e8400-e29b-41d4-a716-446655440003'::uuid, 'staff.read')
) AS role_data(role_id, permission_code)
JOIN permissions ON permissions.code = role_data.permission_code;
