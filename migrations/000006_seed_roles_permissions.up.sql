INSERT INTO roles (id, name, description, scope) VALUES
('650e8400-e29b-41d4-a716-446655440001', 'SUPER_ADMIN', 'Platform administrator', 'PLATFORM'),
('650e8400-e29b-41d4-a716-446655440002', 'BUSINESS_OWNER', 'Tenant business owner', 'TENANT'),
('650e8400-e29b-41d4-a716-446655440003', 'STAFF', 'Tenant staff member', 'TENANT');

INSERT INTO permissions (id, code, description) VALUES
('660e8400-e29b-41d4-a716-446655440001', 'user.read', 'Read users'),
('660e8400-e29b-41d4-a716-446655440002', 'user.create', 'Create users'),
('660e8400-e29b-41d4-a716-446655440003', 'user.update', 'Update users'),
('660e8400-e29b-41d4-a716-446655440004', 'user.disable', 'Disable users'),
('660e8400-e29b-41d4-a716-446655440005', 'tenant.read', 'Read tenants'),
('660e8400-e29b-41d4-a716-446655440006', 'tenant.update', 'Update tenants'),
('660e8400-e29b-41d4-a716-446655440007', 'role.read', 'Read roles'),
('660e8400-e29b-41d4-a716-446655440008', 'role.create', 'Create roles'),
('660e8400-e29b-41d4-a716-446655440009', 'role.update', 'Update roles'),
('660e8400-e29b-41d4-a716-446655440010', 'role.delete', 'Delete roles'),
('660e8400-e29b-41d4-a716-446655440011', 'role.assign', 'Assign roles'),
('660e8400-e29b-41d4-a716-446655440012', 'permission.read', 'Read permissions'),
('660e8400-e29b-41d4-a716-446655440013', 'permission.assign', 'Assign permissions');

INSERT INTO role_permissions (role_id, permission_id)
SELECT '650e8400-e29b-41d4-a716-446655440001', id FROM permissions;

INSERT INTO role_permissions (role_id, permission_id)
SELECT role_data.role_id, permissions.id
FROM (VALUES
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'user.read'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'user.create'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'user.update'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'user.disable'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'tenant.read'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'tenant.update'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'role.read'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'role.assign'),
	('650e8400-e29b-41d4-a716-446655440002'::uuid, 'permission.read'),
	('650e8400-e29b-41d4-a716-446655440003'::uuid, 'tenant.read'),
	('650e8400-e29b-41d4-a716-446655440003'::uuid, 'user.read'),
	('650e8400-e29b-41d4-a716-446655440003'::uuid, 'role.read'),
	('650e8400-e29b-41d4-a716-446655440003'::uuid, 'permission.read')
) AS role_data(role_id, permission_code)
JOIN permissions ON permissions.code = role_data.permission_code;