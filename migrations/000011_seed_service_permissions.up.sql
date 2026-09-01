-- Scheduling S1 permission catalog. Split from 000010 (which creates the
-- schema) for the same reason 000006 is split from 000005: table creation and
-- catalog seeding have different prerequisites, and keeping them apart lets a
-- fixture build just the tables it needs without dragging in the RBAC catalog.
--
INSERT INTO permissions (id, code, description) VALUES
('660e8400-e29b-41d4-a716-446655440014', 'service.read', 'Read services'),
('660e8400-e29b-41d4-a716-446655440015', 'service.create', 'Create services'),
('660e8400-e29b-41d4-a716-446655440016', 'service.update', 'Update services'),
('660e8400-e29b-41d4-a716-446655440017', 'service.archive', 'Archive services');

-- Every grant is listed explicitly. Migration 000006 granted SUPER_ADMIN every
-- permission via INSERT ... SELECT id FROM permissions, which was a one-time
-- snapshot: permissions added afterwards are not picked up by it, so a wildcard
-- here would both re-grant the whole catalog and hide that drift. SUPER_ADMIN's
-- grants below are therefore spelled out like every other role's.
--
-- Granting SUPER_ADMIN these permissions confers no tenant access: it is a
-- PLATFORM-scoped role, and PermissionResolutionService.ResolveTenant reads
-- only TENANT-scoped roles, so a SUPER_ADMIN still resolves to zero permissions
-- inside any tenant. The grants exist for catalog consistency, not to open a
-- platform path into tenant data.
--
-- STAFF gets service.read only. A technician needs to see the menu to do their
-- job; pricing and catalog structure are owner decisions, and STAFF holds no
-- write permission anywhere in the catalog today.
INSERT INTO role_permissions (role_id, permission_id)
SELECT role_data.role_id, permissions.id
FROM (VALUES
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'service.read'),
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'service.create'),
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'service.update'),
	('650e8400-e29b-41d4-a716-446655440001'::uuid, 'service.archive'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'service.read'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'service.create'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'service.update'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'service.archive'),
	('650e8400-e29b-41d4-a716-446655440003'::uuid, 'service.read')
) AS role_data(role_id, permission_code)
JOIN permissions ON permissions.code = role_data.permission_code;
