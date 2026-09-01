-- Reverses ONLY what 000013 added. Order matters: role_permissions holds a
-- foreign key to permissions, so the grants must go before the permission rows
-- they reference, or the DELETE below fails.
--
-- The grants are removed by permission_id rather than by (role_id,
-- permission_id) pairs so this also cleans up any grant of these four
-- permissions to a role added after 000013 — leaving one behind would strand a
-- role_permissions row pointing at a permission that no longer exists.
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('staff.read', 'staff.create', 'staff.update', 'staff.archive')
);

DELETE FROM permissions
WHERE code IN ('staff.read', 'staff.create', 'staff.update', 'staff.archive');
