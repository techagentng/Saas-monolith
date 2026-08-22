# Multi-Tenant Booking System
# Epic 1 — Feature 5: Roles & Permissions

**Document Type:** Feature Implementation Specification / Context Restoration Document  
**Project:** Multi-Tenant Booking System  
**Epic:** Epic 1 — Identity & Access  
**Feature:** Feature 5 — Roles & Permissions  
**Backend:** Go  
**Database:** PostgreSQL  
**Architecture:** Modular Monolith  
**Request Flow:** API → Handler → Service → Repository → Database  
**Development Method:** Pure TDD / Test-First Development  

---

# 1. Purpose

This document defines the authoritative feature-level requirements for:

**Epic 1 — Feature 5: Roles & Permissions**

Feature 5 builds on:

1. Master Project Specification.
2. Epic 1 — Identity & Access specification.
3. Feature 1 — Application Error Infrastructure.
4. Feature 2 — User Identity.
5. Feature 3 — Authentication & Session Foundation.
6. Feature 4 — Tenant Membership & Context.

Feature 5 introduces the role and permission model required for later authorization.

Its responsibility is to answer:

> What roles are assigned to this user within this scope?

and:

> Which permissions are granted by those roles?

It does NOT yet make endpoint/resource authorization decisions.

That enforcement belongs to Feature 6.

---

# 2. Architecture Context

The established backend architecture remains:

```text
API → Handler → Service → Repository → Database
```

A module is an organizational boundary, not another architectural layer.

Roles and permissions belong inside Identity & Access while preserving the established separation.

Conceptually:

```text
HTTP/API
    ↓
Role / Permission Handler
    ↓
Role / Permission Service
    ↓
Role / Permission Repository
    ↓
PostgreSQL
```

Feature 5 may also provide internal resolution capabilities that Feature 6 consumes.

---

# 3. Feature Goal

Implement the RBAC data model and assignment foundation.

Feature 5 must provide:

```text
Role
    ↓
Permissions
    ↓
Role Assignment
    ↓
Effective Permission Resolution
```

The feature should support:

- Role persistence.
- Permission persistence.
- Role-permission relationships.
- User-role relationships.
- Tenant-scoped role assignment where applicable.
- Platform-scoped role assignment where applicable.
- Role creation.
- Role lookup.
- Permission lookup.
- Assigning permissions to roles.
- Assigning roles to users.
- Removing role assignments.
- Resolving a user's effective permissions for a given scope.
- Database constraints preventing invalid duplicates.
- TDD and security tests.

---

# 4. Feature Dependencies

## Feature 1

Reuse centralized application errors.

Potentially relevant existing codes include:

```text
INVALID_REQUEST
VALIDATION_FAILED
USER_NOT_FOUND
TENANT_NOT_FOUND
TENANT_ACCESS_DENIED
RESOURCE_NOT_FOUND
SERVICE_UNAVAILABLE
INTERNAL_ERROR
```

Feature 5 may require additional stable codes, but only when justified by actual expected business failures.

Likely candidates include:

```text
ROLE_NOT_FOUND
ROLE_ALREADY_EXISTS
PERMISSION_NOT_FOUND
ROLE_ASSIGNMENT_ALREADY_EXISTS
```

Do not add codes speculatively.

Any new code must be added to the shared centralized error contract.

---

## Feature 2

Reuse the existing User identity.

Do not create a second user model.

---

## Feature 3

Reuse authenticated `Principal`.

Do not add role/permission claims into Feature 3 authentication tokens as the source of truth.

---

## Feature 4

Reuse trusted `TenantContext`.

Tenant-scoped role assignments must use trusted tenant context or validated tenant IDs.

Do not trust raw client-supplied tenant IDs without Feature 4 tenant/membership verification where tenant access matters.

---

# 5. Authentication vs Tenant Context vs Roles vs Authorization

These concepts must remain separate:

```text
Authentication
    ↓
Who is the user?

Tenant Context
    ↓
Which tenant is the request operating within?

Roles & Permissions
    ↓
What capabilities are assigned to the user?

Authorization
    ↓
May the user perform this specific action?
```

Feature 5 implements only the third layer.

Feature 6 performs authorization decisions.

---

# 6. Core Domain Concepts

Feature 5 introduces:

```text
Role
Permission
RolePermission
UserRole
```

Conceptually:

```text
User
 ↓
UserRole
 ↓
Role
 ↓
RolePermission
 ↓
Permission
```

For tenant-scoped role assignments:

```text
User
 ↓
Tenant Membership
 ↓
UserRole (Tenant Scope)
 ↓
Role
 ↓
Permissions
```

---

# 7. Role Model

A minimal Role should contain:

```text
Role
├── ID
├── Name
├── Description
├── Scope
├── CreatedAt
└── UpdatedAt
```

Potential scope values:

```text
PLATFORM
TENANT
```

Exact values must be approved during planning.

Do not add:

```text
TenantID directly on Role
```

unless the approved design requires tenant-custom roles.

The distinction between globally defined roles and tenant-specific role definitions must be made explicitly during planning.

---

# 8. Permission Model

A minimal Permission should contain:

```text
Permission
├── ID
├── Code
├── Description
└── CreatedAt
```

Permission codes must be stable machine-readable identifiers.

Examples:

```text
user.read
user.create
user.update
user.disable

role.read
role.create
role.update
role.delete

permission.read
permission.assign

tenant.read
tenant.update

booking.read
booking.create
booking.update
booking.cancel
```

Feature 5 may define only the permissions actually required by the current project stage.

Do not create a huge speculative permission catalog.

---

# 9. Permission Code Rules

Permission codes should:

- Be stable.
- Be descriptive.
- Use a consistent naming convention.
- Avoid user-specific or tenant-specific values.
- Avoid high-cardinality dynamic data.

Recommended format:

```text
resource.action
```

Examples:

```text
role.read
role.assign
tenant.read
booking.create
```

Do not use:

```text
ROLE_123_ACCESS
TENANT_X_BOOKING
```

Permission codes are contracts and should remain stable once published.

---

# 10. Role Scope

Feature 5 must explicitly distinguish:

```text
PLATFORM scope
TENANT scope
```

Potential meaning:

## PLATFORM

Role applies to platform-wide administration.

Example:

```text
SUPER_ADMIN
```

## TENANT

Role applies within a specific tenant.

Examples:

```text
BUSINESS_OWNER
STAFF
```

The exact role/scope model must be approved before implementation.

Do not allow a tenant role assignment to accidentally grant platform authority.

---

# 11. Initial Role Strategy

Feature 5 must determine whether roles are:

1. System-defined only.
2. Tenant-customizable.
3. A hybrid of system-defined and tenant-custom roles.

Do not silently implement tenant-custom roles without approval.

A safe initial approach is:

```text
System-defined roles
+
Tenant-scoped assignments
```

For example:

```text
SUPER_ADMIN      → PLATFORM
BUSINESS_OWNER   → TENANT
STAFF            → TENANT
CUSTOMER         → TENANT or separate application identity behavior
```

Whether CUSTOMER requires an RBAC role assignment at all must be explicitly decided.

---

# 12. Role-Permission Relationship

A Role may contain many Permissions.

A Permission may belong to many Roles.

Therefore:

```text
Role N:M Permission
```

Database concept:

```text
role_permissions
├── role_id
└── permission_id
```

Required invariant:

```text
UNIQUE(role_id, permission_id)
```

Duplicate permission assignment to the same role must not create duplicate relationships.

---

# 13. User-Role Relationship

A user may hold one or more roles.

A tenant-scoped assignment should conceptually include:

```text
UserRole
├── ID
├── UserID
├── RoleID
├── TenantID (when tenant-scoped)
├── CreatedAt
└── UpdatedAt if required
```

The exact schema depends on the approved scope design.

A platform role must not require a tenant ID.

A tenant role should be tied to a tenant.

The implementation must enforce valid scope combinations.

---

# 14. Tenant Membership Requirement

For a tenant-scoped role assignment:

```text
User must already have ACTIVE membership in the tenant
```

Feature 5 must not allow:

```text
User A
+
Tenant B role
```

if User A is not an active member of Tenant B.

This validation must use Feature 4 membership infrastructure.

Do not recreate tenant membership logic.

---

# 15. Role Assignment Rules

Tenant role assignment must validate:

1. User exists.
2. Role exists.
3. Role is TENANT-scoped.
4. Tenant exists.
5. User has active membership in that tenant.
6. Duplicate assignment does not already exist.

Platform role assignment must validate:

1. User exists.
2. Role exists.
3. Role is PLATFORM-scoped.
4. Tenant ID is not incorrectly supplied where forbidden.

The exact mutation authorization remains outside Feature 5 unless explicitly approved.

---

# 16. Effective Permission Resolution

Feature 5 must provide an internal capability to resolve:

```text
EffectivePermissions(userID, tenantID/scope)
```

Conceptually:

```text
User
 ↓
UserRole assignments
 ↓
Role
 ↓
RolePermission
 ↓
Permission Code Set
```

Expected result:

```text
Set of permission codes
```

For example:

```text
booking.read
booking.create
staff.read
```

Feature 5 must compute or retrieve the permission set.

Feature 6 later uses that set to make authorization decisions.

---

# 17. Permission Resolution Scope

Tenant permission resolution must be tenant-specific.

Example:

```text
User A
    BUSINESS_OWNER in Tenant A
    STAFF in Tenant B
```

Then:

```text
EffectivePermissions(User A, Tenant A)
```

must not accidentally include Tenant B privileges.

Cross-tenant role leakage is a critical security failure.

---

# 18. Platform Permission Resolution

Platform-scoped roles must remain distinct.

Example:

```text
SUPER_ADMIN
```

must not be accidentally inferred from:

```text
BUSINESS_OWNER in Tenant A
```

Platform and tenant scope resolution must remain explicit.

---

# 19. Database Tables

Feature 5 will likely require:

```text
roles
permissions
role_permissions
user_roles
```

Do not create unrelated Feature 6 policy tables.

---

# 20. Roles Table

Likely schema:

```text
roles
├── id UUID PRIMARY KEY
├── name TEXT/VARCHAR NOT NULL
├── description TEXT
├── scope TEXT NOT NULL
├── created_at TIMESTAMPTZ NOT NULL
└── updated_at TIMESTAMPTZ NOT NULL
```

Required constraints:

- Primary key.
- Non-null role name.
- Valid scope values.
- Appropriate uniqueness.

The exact uniqueness strategy must be approved.

For system-defined roles, likely:

```text
UNIQUE(name, scope)
```

---

# 21. Permissions Table

Likely schema:

```text
permissions
├── id UUID PRIMARY KEY
├── code TEXT/VARCHAR NOT NULL UNIQUE
├── description TEXT
└── created_at TIMESTAMPTZ NOT NULL
```

Permission `code` must be unique.

---

# 22. Role Permissions Table

Likely schema:

```text
role_permissions
├── role_id UUID NOT NULL REFERENCES roles(id)
├── permission_id UUID NOT NULL REFERENCES permissions(id)
└── created_at TIMESTAMPTZ if required
```

Required constraint:

```text
UNIQUE(role_id, permission_id)
```

---

# 23. User Roles Table

Likely schema:

```text
user_roles
├── id UUID PRIMARY KEY
├── user_id UUID NOT NULL REFERENCES users(id)
├── role_id UUID NOT NULL REFERENCES roles(id)
├── tenant_id UUID NULL REFERENCES tenants(id)
├── created_at TIMESTAMPTZ NOT NULL
└── updated_at TIMESTAMPTZ if required
```

The schema must enforce valid platform vs tenant assignments.

Examples:

```text
PLATFORM role
→ tenant_id must be NULL
```

```text
TENANT role
→ tenant_id must be NOT NULL
```

The exact enforcement mechanism must be planned carefully.

Do not rely solely on application logic where database constraints can protect invariants.

---

# 24. Duplicate Assignment Invariants

Database constraints must prevent duplicate:

```text
role ↔ permission
```

and:

```text
user ↔ role ↔ scope
```

relationships.

For tenant assignments:

```text
UNIQUE(user_id, role_id, tenant_id)
```

must be considered.

Platform assignments require equivalent duplicate protection where `tenant_id` is NULL.

Because SQL NULL uniqueness behavior can be subtle, the implementation plan must explicitly define the correct PostgreSQL constraint/index strategy.

Do not assume a normal UNIQUE constraint handles all NULL cases correctly.

---

# 25. Seed Data

Feature 5 must decide whether initial roles/permissions are created through:

- SQL migrations.
- Seed migrations.
- Application startup logic.
- Administrative API.

Preferred approach for system-defined static roles/permissions:

```text
Version-controlled database seed/migration
```

Do not silently create critical RBAC definitions at application startup.

The exact seed strategy must be approved.

---

# 26. Initial Roles

Likely project roles include:

```text
SUPER_ADMIN
BUSINESS_OWNER
STAFF
CUSTOMER
```

However, Feature 5 must explicitly decide:

- Which roles are seeded now.
- Which scope each role belongs to.
- Whether CUSTOMER participates in this RBAC model.
- Whether custom roles are allowed.

Do not assume all conceptual roles must immediately become database roles.

---

# 27. Initial Permissions

Do not generate all future project permissions.

Seed only permissions required by current administrative and future authorization requirements where sufficiently defined.

Feature 5 may establish a minimal catalog that Feature 6 can consume.

The implementation plan should list every initial permission code explicitly.

---

# 28. Role Repository Responsibilities

Role repository responsibilities may include:

- Create role if approved.
- Find role by ID.
- Find role by name/scope.
- List roles.
- Persist role definitions.
- Map duplicate role conditions.
- Parameterized SQL.
- Safe database error wrapping.

Do not perform permission authorization in the repository.

---

# 29. Permission Repository Responsibilities

Permission repository may include:

- Find permission by ID.
- Find permission by code.
- List permissions.
- Persist permissions only if dynamic creation is approved.

If permissions are static/system-managed, public permission creation may not be required.

---

# 30. Role Permission Repository Responsibilities

Responsibilities:

- Assign permission to role.
- Remove permission from role if approved.
- List role permissions.
- Enforce duplicate constraint.
- Parameterized SQL.
- Preserve typed errors.

Do not determine whether the actor is authorized to modify roles.

---

# 31. User Role Repository Responsibilities

Responsibilities:

- Assign role to user.
- Remove role assignment.
- List roles by user/scope.
- Resolve role assignments by tenant.
- Support effective permission queries.
- Enforce duplicate assignment constraints.
- Parameterized SQL.

Do not trust client identity.

---

# 32. Service Responsibilities

Feature 5 services may include:

```text
RoleService
PermissionService
RoleAssignmentService
PermissionResolutionService
```

Exact service decomposition should follow cohesion.

Avoid creating a service for every noun automatically.

---

# 33. Role Service

Potential responsibilities:

- Create roles if dynamic role creation is approved.
- Validate name/scope.
- Find/list roles.
- Preserve shared error semantics.

If roles are system-defined only, role creation endpoint/service behavior may be deferred.

---

# 34. Permission Service

Potential responsibilities:

- Find/list permissions.
- Validate permission references.
- Assign permissions to roles where approved.

If permissions are system-defined only, dynamic permission creation should not be introduced.

---

# 35. Role Assignment Service

Responsibilities:

- Validate user.
- Validate role.
- Validate role scope.
- Validate tenant when tenant-scoped.
- Validate active tenant membership.
- Prevent duplicate assignment.
- Persist assignment.
- Remove assignment.
- Preserve error wrapping.

No Feature 6 authorization decisions should be embedded here.

---

# 36. Permission Resolution Service

Responsibilities:

- Resolve effective permission codes.
- Respect platform vs tenant scope.
- Prevent cross-tenant permission leakage.
- Return a deterministic permission set.
- Avoid relying on client claims.

Feature 6 will consume this capability.

---

# 37. Error Codes

Feature 5 should reuse existing codes wherever appropriate.

Likely new stable codes may include:

```text
ROLE_NOT_FOUND
ROLE_ALREADY_EXISTS
PERMISSION_NOT_FOUND
ROLE_ASSIGNMENT_ALREADY_EXISTS
```

Possibly:

```text
ROLE_SCOPE_INVALID
```

only if needed as a stable expected business failure.

Do not add an error code merely because it sounds useful.

New codes must:

- Be centrally defined.
- Have centralized HTTP mapping where publicly exposed.
- Be covered by Feature 1 error tests if shared contract tests require it.

---

# 38. Public API Scope

Feature 5 is primarily RBAC foundation.

Production mutation routes are security-sensitive because Feature 6 authorization does not yet exist.

Therefore, the implementation plan must distinguish:

```text
capability exists internally
```

from:

```text
route is publicly registered
```

Do not expose unrestricted RBAC mutation APIs before authorization exists.

---

# 39. Candidate Endpoints

Potential capabilities include:

```text
GET /api/v1/roles
GET /api/v1/permissions

POST /api/v1/users/{userID}/roles
DELETE /api/v1/users/{userID}/roles/{roleID}

POST /api/v1/roles/{roleID}/permissions
DELETE /api/v1/roles/{roleID}/permissions/{permissionID}
```

Tenant-scoped role assignment may require:

```text
POST /api/v1/tenants/{tenantID}/users/{userID}/roles
```

These are conceptual.

Do NOT register mutation routes publicly until their authorization policy exists.

Read-only routes also require an explicit access policy before exposure.

---

# 40. Public DTOs

Do not serialize internal persistence models blindly.

Use explicit public representations for:

- Roles.
- Permissions.
- Role assignments.

Avoid exposing:

- Internal database details.
- Hidden fields.
- Future implementation metadata.

---

# 41. Tenant Role Assignment Flow

Conceptually:

```text
Authenticated Principal
        ↓
Trusted TenantContext
        ↓
Role Assignment Request
        ↓
RoleAssignmentService
        ↓
Validate User
        ↓
Validate Role
        ↓
Validate Role Scope = TENANT
        ↓
Validate Active Membership
        ↓
Persist UserRole
```

Feature 5 validates assignment correctness.

Feature 6 later validates whether the actor is allowed to perform the assignment.

---

# 42. Platform Role Assignment Flow

Conceptually:

```text
User
 ↓
Platform Role
 ↓
UserRole with PLATFORM scope
```

No tenant context should be required.

Platform role assignment must never be inferred from tenant membership.

---

# 43. Security Requirements

Mandatory requirements:

- Cross-tenant role leakage must be impossible.
- Tenant roles require active membership.
- Platform roles remain distinct.
- Roles/permissions must not come from untrusted request headers.
- Feature 3 tokens must not become the source of role/permission truth.
- Permission resolution must use current server-side state.
- Database constraints enforce duplicates.
- SQL must be parameterized.
- Raw database errors must not leak.
- Public APIs must use explicit DTOs.
- No unauthorized mutation route should be publicly exposed before Feature 6.
- No direct object/reference shortcut should bypass tenant scope.

---

# 44. Role/Permission Caching

Do not introduce permission caching in Feature 5 unless explicitly approved.

Role and permission assignments may change.

Feature 6 should initially rely on authoritative server-side resolution.

Caching introduces invalidation complexity and stale authorization risk.

Defer caching until performance evidence requires it.

---

# 45. TDD Requirements

Feature 5 must use pure TDD.

Recommended sequence:

1. Inspect Features 1–4.
2. Inspect existing migrations and data model conventions.
3. Resolve role/scope strategy.
4. Resolve initial roles.
5. Resolve initial permission catalog.
6. Resolve database seed strategy.
7. Write failing Role model tests.
8. Write failing Permission model tests.
9. Write failing repository tests.
10. Write failing tenant-role assignment tests.
11. Write failing platform-role assignment tests.
12. Write failing duplicate-assignment tests.
13. Write failing role-permission tests.
14. Write failing effective-permission resolution tests.
15. Write failing cross-tenant isolation tests.
16. Write failing handler/API tests for approved capabilities.
17. Implement minimum schema.
18. Implement minimum repositories.
19. Implement services.
20. Implement resolution.
21. Implement approved handlers.
22. Register only approved routes.
23. Run focused tests.
24. Run PostgreSQL integration tests.
25. Run:

```text
go vet ./...
go test ./...
```

26. Conduct independent security review.
27. Stop before Feature 6.

---

# 46. Model Tests

Test:

- Valid role scope.
- Invalid role scope rejected.
- Permission codes follow approved format.
- Role/public representations exclude internal fields.
- Permission/public representations exclude internal fields.
- Tenant role assignments require tenant scope.
- Platform assignments do not carry tenant scope.

---

# 47. Repository Tests

Test:

- Role persistence.
- Role lookup.
- Permission persistence/seed lookup.
- Permission code uniqueness.
- Role name/scope uniqueness.
- Role-permission insertion.
- Duplicate role-permission rejection.
- User-role insertion.
- Duplicate tenant-role assignment rejection.
- Duplicate platform-role assignment rejection.
- Tenant/user/role foreign keys.
- Status/scope constraints where applicable.
- SQL is parameterized.
- Raw PostgreSQL errors are safely wrapped.

---

# 48. Role Assignment Service Tests

Test:

```text
User exists
+
Tenant role exists
+
Active membership exists
→ assignment succeeds
```

Test:

```text
User exists
+
Tenant role exists
+
No tenant membership
→ assignment rejected
```

Test:

```text
Disabled membership
→ assignment rejected
```

Test:

```text
Tenant role + wrong tenant
→ rejected
```

Test:

```text
Platform role + tenant ID where forbidden
→ rejected
```

Test:

```text
Duplicate role assignment
→ stable duplicate error
```

Test:

```text
Missing user
→ USER_NOT_FOUND
```

Test:

```text
Missing role
→ ROLE_NOT_FOUND
```

---

# 49. Role Permission Tests

Test:

- Assign permission to role.
- Duplicate assignment rejected.
- Missing role rejected.
- Missing permission rejected.
- Removing permission behaves according to approved design.
- Platform and tenant roles may receive only approved permissions if scope restrictions are introduced.
- No actor authorization is accidentally implemented.

---

# 50. Effective Permission Resolution Tests

Mandatory cases:

```text
User A
Role OWNER in Tenant A
→ Tenant A permissions returned
```

```text
User A
Role STAFF in Tenant B
→ Tenant B permissions returned
```

```text
Resolve User A permissions for Tenant A
→ Tenant B permissions absent
```

```text
User A has no roles in Tenant C
→ empty permission set
```

```text
User A has platform role
→ platform permission resolution works independently
```

```text
Tenant role does not become platform permission
```

```text
Duplicate role paths do not duplicate permission codes
```

The result should behave like a set.

---

# 51. Cross-Tenant Security Tests

Mandatory tests:

```text
User A is OWNER in Tenant A.
User A is STAFF in Tenant B.

Resolve Tenant A permissions.
→ only Tenant A capabilities.
```

```text
User A knows Tenant B role ID.
→ role ID alone cannot assign authority outside the correct tenant.
```

```text
User A loses Tenant A membership.
→ tenant-scoped role must no longer produce usable tenant permissions.
```

Whether the role assignment row remains stored after membership disablement must be explicitly decided.

At minimum, active membership must be required for effective tenant authorization.

---

# 52. Membership Revocation Interaction

Feature 4 membership revocation must immediately affect Feature 5 permission resolution.

Expected:

```text
Authentication remains valid.
Membership becomes DISABLED.
Existing role assignment may remain stored.
Effective Tenant Permissions → none / inaccessible.
```

Do not require deletion of historical role assignments merely to remove access unless the approved design explicitly requires it.

This keeps:

```text
Membership
```

as the tenant-access boundary.

---

# 53. Database Integration Tests

PostgreSQL integration tests should verify:

- Actual migration SQL.
- Role constraints.
- Permission uniqueness.
- Role-permission uniqueness.
- Tenant user-role uniqueness.
- Platform user-role uniqueness.
- Foreign keys.
- Valid/invalid scope combinations.
- Seed data if migrations provide it.
- Cross-tenant query behavior.
- Effective permission queries where performed in SQL.

Use the project's approved integration-test database convention.

---

# 54. Scope Boundaries

Feature 5 must not implement:

- Authorization middleware.
- Endpoint authorization.
- Resource ownership checks.
- Booking authorization.
- Permission decorators.
- Policy engine.
- Super-admin bypass logic scattered through handlers.
- Frontend role navigation.
- Frontend permission guards.
- Tenant branding.
- Billing.
- Notifications.
- Audit infrastructure.
- Invitation flows.
- Permission caching.
- Token role/permission claims as authority.

These belong elsewhere.

---

# 55. Acceptance Criteria

Feature 5 is complete when:

- [ ] Role model exists.
- [ ] Permission model exists.
- [ ] Role scope model exists.
- [ ] Role persistence exists.
- [ ] Permission persistence/seed strategy exists.
- [ ] Role-permission relationship exists.
- [ ] User-role assignment exists.
- [ ] Tenant-scoped role assignment is supported.
- [ ] Platform-scoped role assignment is supported if approved.
- [ ] Tenant role assignment requires active tenant membership.
- [ ] Duplicate assignments are database-enforced.
- [ ] Permission codes are stable.
- [ ] Effective permission resolution exists.
- [ ] Tenant permission resolution is tenant-specific.
- [ ] Cross-tenant permission leakage is prevented.
- [ ] Membership revocation removes effective tenant authority.
- [ ] Feature 1 errors are reused.
- [ ] Features 2–4 infrastructure is reused.
- [ ] SQL is parameterized.
- [ ] Raw database errors do not leak.
- [ ] Public representations use explicit DTOs.
- [ ] No unrestricted mutation API is exposed before authorization exists.
- [ ] Tests were written before implementation.
- [ ] Model tests pass.
- [ ] Repository tests pass.
- [ ] Service tests pass.
- [ ] Effective-permission tests pass.
- [ ] Cross-tenant security tests pass.
- [ ] PostgreSQL integration tests pass.
- [ ] `go vet ./...` passes.
- [ ] `go test ./...` passes.
- [ ] No Feature 6 authorization logic was introduced.

---

# 56. Decisions Requiring Explicit Approval

Before implementation, the planning agent must resolve:

1. Are roles system-defined only, tenant-custom, or hybrid?
2. Exact role scope values.
3. Which initial roles are seeded?
4. Is SUPER_ADMIN included now?
5. Is BUSINESS_OWNER included now?
6. Is STAFF included now?
7. Is CUSTOMER represented as an RBAC role?
8. Exact initial permission catalog.
9. Are permissions system-defined only?
10. Can roles be created dynamically in Feature 5?
11. Can permissions be created dynamically?
12. Are role definitions global or tenant-owned?
13. Exact role uniqueness rules.
14. Exact user-role schema.
15. Exact platform assignment uniqueness strategy.
16. Exact tenant assignment uniqueness strategy.
17. How NULL tenant IDs are constrained for platform roles.
18. Whether a CHECK/trigger/application validation is used for scope consistency.
19. Seed/migration strategy.
20. Whether role-permission mutation handlers exist but remain unregistered.
21. Whether user-role mutation handlers exist but remain unregistered.
22. Exact read-only endpoint exposure, if any.
23. Whether role assignment rows survive tenant membership revocation.
24. How effective permission resolution treats disabled membership.
25. Whether disabled tenant context results in zero tenant permissions.
26. Whether any permission-scope compatibility rules are required now.
27. Whether permission removal is included.
28. Whether role removal/deletion is included.
29. Whether system roles are immutable.
30. Whether any new Feature 1 error codes are required.
31. Exact public DTO shape.
32. Integration-test strategy.
33. Migration naming convention.
34. Whether existing code already partially implements Feature 5.
35. Confirmation that Feature 6 authorization remains completely deferred.

Do not silently invent these decisions.

---

# 57. COPILOT / OPUS PLANNING PROMPT

Use this first.

```text
We are now implementing Epic 1, Feature 5 — Roles & Permissions.

Read the repository before proposing changes.

Authoritative documents:

1. Master Project Specification.
2. Epic 1 — Identity & Access specification.
3. Feature 1 — Application Error Infrastructure.
4. Feature 2 — User Identity.
5. Feature 3 — Authentication & Session Foundation.
6. Feature 4 — Tenant Membership & Context.
7. Feature 5 — Roles & Permissions specification.

Features 1–4 are complete.

IMPORTANT:

DO NOT IMPLEMENT CODE YET.

First inspect the current repository and produce a detailed implementation
plan for Feature 5 only.

Preserve:

API → Handler → Service → Repository → Database

A module is an organizational boundary, not another architectural layer.

Feature 5 answers:

"What roles does this user have?"
"What permissions do those roles provide?"

Feature 5 does NOT answer:

"May this request perform this action?"

That is Feature 6.

Inspect:

- Existing Feature 1 shared error codes.
- User identity.
- Authentication Principal.
- TenantContext.
- Tenant memberships.
- Database conventions.
- UUID conventions.
- Migration conventions.
- PostgreSQL integration-test conventions.
- Any existing role/permission code.

Your plan must cover:

1. Role model.
2. Permission model.
3. Role scope.
4. Role-permission relationship.
5. User-role relationship.
6. Platform vs tenant assignments.
7. Membership validation.
8. Effective permission resolution.
9. Cross-tenant isolation.
10. Database schema.
11. Database constraints.
12. PostgreSQL NULL/uniqueness handling for platform assignments.
13. Seed strategy.
14. Initial role catalog.
15. Initial permission catalog.
16. Repository interfaces.
17. Service responsibilities.
18. Public DTOs.
19. Candidate handlers.
20. Production route-registration boundary.
21. Feature 1 error integration.
22. TDD sequence.
23. Model tests.
24. Repository tests.
25. Service tests.
26. Effective-permission tests.
27. Cross-tenant security tests.
28. PostgreSQL integration tests.
29. Files to create.
30. Files to modify.
31. Explicit Feature 6 exclusions.

SECURITY REQUIREMENTS:

- Prevent cross-tenant role leakage.
- Tenant roles require active Feature 4 membership.
- Platform authority remains separate from tenant authority.
- Do not trust client-provided role/permission claims.
- Feature 3 access tokens remain role/permission neutral.
- Resolve permissions from server-side state.
- Do not cache permissions in Feature 5.
- Use parameterized SQL.
- Enforce duplicate relationships in PostgreSQL.
- Never expose raw database errors.
- Do not publicly expose unrestricted RBAC mutations before Feature 6
  authorization exists.

PURE TDD IS REQUIRED.

Show:

RED
→ GREEN
→ REFACTOR

for each major implementation stage.

At the end provide:

DECISIONS REQUIRING APPROVAL

For every unresolved decision provide:

- Decision.
- Recommended option.
- Alternatives.
- Security implications.
- Complexity implications.
- Why you recommend it.

Do not implement production code until the decisions are approved.
```

---

# 58. IMPLEMENTATION PROMPT

Use only after the plan and decisions are approved.

```text
The Feature 5 implementation plan and explicit decisions are approved.

Implement only:

Epic 1 — Feature 5: Roles & Permissions

Use the existing Features 1–4 as the baseline.

DO NOT recreate or duplicate their behavior.

Preserve:

API → Handler → Service → Repository → Database

Use pure TDD:

Requirement
→ RED failing test
→ verify failure
→ minimum production implementation
→ GREEN
→ refactor
→ edge/security tests

Mandatory requirements:

- Reuse User identity.
- Reuse auth.Principal.
- Reuse TenantContext.
- Reuse Tenant Membership infrastructure.
- Tenant role assignment requires ACTIVE tenant membership.
- Platform and tenant roles remain separate.
- Prevent cross-tenant role leakage.
- Resolve permissions server-side.
- Do not put roles/permissions in Feature 3 access tokens as authority.
- Do not introduce permission caching.
- Enforce duplicate assignments in PostgreSQL.
- Use parameterized SQL.
- Never expose raw database errors.
- Use Feature 1 typed errors and wrapping conventions.
- Use explicit public DTOs.

Do not expose unrestricted RBAC mutation routes before Feature 6
authorization exists.

Handlers may exist and be tested where required, but route registration
must follow the approved plan.

DO NOT implement:

- Authorization middleware.
- Permission enforcement.
- Resource authorization.
- Booking authorization.
- Policy engines.
- Super-admin bypass logic in handlers.
- Frontend RBAC.
- Audit infrastructure.
- Tenant branding.
- Billing.
- Notifications.
- Permission caching.
- Authentication redesign.

If a blocking defect is discovered in Features 1–4:

STOP.

Report the defect before modifying completed features.

After implementation run:

go vet ./...
go test ./...

and all configured PostgreSQL integration tests.

Report:

1. Files created.
2. Files modified.
3. Migration(s) created.
4. Seed data created.
5. Tests added.
6. Cross-tenant tests added.
7. Database constraints implemented.
8. Focused test results.
9. Integration-test results.
10. go vet result.
11. go test result.
12. Any deviation from the approved plan.
13. Confirmation that Feature 6 authorization was not introduced.
```

---

# 59. INDEPENDENT REVIEW PROMPT

After implementation, use this for review.

```text
REVIEW ONLY.

Do not modify files.

Review:

Epic 1 — Feature 5: Roles & Permissions

Authoritative sources:

1. Master Project Specification.
2. Epic 1 specification.
3. Feature 1 specification.
4. Feature 2 specification.
5. Feature 3 specification.
6. Feature 4 specification.
7. Feature 5 specification.
8. Approved Feature 5 implementation plan.
9. Approved Feature 5 decision addendum.

Inspect the actual implementation and tests.

Verify:

ARCHITECTURE

- Handler → Service → Repository separation.
- No SQL in handlers/services.
- No Feature 6 authorization logic exists.

ROLES

- Role model matches approved scope.
- Role uniqueness is enforced.
- Platform and tenant roles remain distinct.
- System role rules match approved decisions.

PERMISSIONS

- Permission codes are stable.
- Permission uniqueness exists.
- Initial permission catalog matches approved plan.
- No speculative future permission explosion occurred.

ROLE-PERMISSION

- Relationship is database-enforced.
- Duplicate assignments are rejected.
- Missing roles/permissions fail safely.

USER-ROLE

- Tenant assignment requires active membership.
- Platform assignment does not accidentally acquire tenant scope.
- Duplicate tenant assignment is prevented.
- Duplicate platform assignment is prevented.
- PostgreSQL NULL uniqueness behavior is handled correctly.

CROSS-TENANT SECURITY

- Tenant A roles cannot grant Tenant B permissions.
- Role IDs alone cannot bypass tenant scope.
- Disabled membership removes effective tenant permissions.
- Membership revocation immediately affects permission resolution.
- Tenant roles never become platform roles.

EFFECTIVE PERMISSIONS

- Resolution returns correct permission set.
- Duplicate paths are deduplicated.
- Tenant scope is respected.
- Platform scope is respected.
- Permission resolution uses server-side state.
- No stale token role claims are trusted.
- No permission caching was introduced.

ERRORS

- Feature 1 errors are reused.
- Any new error code is centrally defined.
- %w/errors.As semantics are preserved.
- Raw DB errors do not leak.

DATABASE

- Migrations execute.
- Foreign keys are correct.
- Role scope constraints are correct.
- Permission uniqueness is correct.
- Role-permission uniqueness is correct.
- Tenant user-role uniqueness is correct.
- Platform user-role uniqueness is correct.
- SQL is parameterized.

API BOUNDARY

- Explicit DTOs are used.
- No unrestricted RBAC mutation API is publicly exposed before Feature 6.
- Route registration matches approved decisions.

TESTING

Verify tests cover:

- Role model.
- Permission model.
- Role scope.
- Tenant role assignment.
- Platform role assignment.
- Missing membership.
- Disabled membership.
- Duplicate role assignment.
- Role-permission assignment.
- Duplicate permission assignment.
- Effective permission resolution.
- Cross-tenant isolation.
- Membership revocation interaction.
- PostgreSQL constraints.
- Error sanitization.

Run or inspect:

go vet ./...
go test ./...

and configured PostgreSQL integration tests.

Classify findings as:

CRITICAL
IMPORTANT
MINOR

Do not confuse stylistic preferences with specification violations.

End with:

FEATURE 5 VERDICT

Choose exactly one:

APPROVED
APPROVED AFTER TARGETED FIXES
NOT APPROVED

Then list only required fixes.

Do not modify files.
```

---

# 60. Context Restoration Summary

```text
PROJECT:
Multi-Tenant Booking System

EPIC:
Epic 1 — Identity & Access

COMPLETED:
F1 Application Error Infrastructure
F2 User Identity
F3 Authentication & Sessions
F4 Tenant Membership & Context

CURRENT:
F5 Roles & Permissions

NEXT:
F6 Authorization & Access Enforcement

ARCHITECTURE:
API → Handler → Service → Repository → Database

FEATURE 5 PURPOSE:

Define:

- roles
- permissions
- role-permission relationships
- user-role relationships
- tenant/platform scope
- effective permission resolution

FEATURE 5 DOES NOT:

Authorize endpoints or resources.

That belongs to Feature 6.

CRITICAL SECURITY BOUNDARY:

Authentication
≠ Tenant Context
≠ Roles/Permissions
≠ Authorization

TENANT ROLES:

Require ACTIVE Feature 4 membership.

PLATFORM ROLES:

Remain separate from tenant roles.

ACCESS TOKENS:

Remain role/permission neutral as authorization authority.

SERVER-SIDE STATE:

Is authoritative for role and permission resolution.

DO NOT CACHE:

Permissions in Feature 5.

DATABASE:

Likely tables:

roles
permissions
role_permissions
user_roles

Important constraints:

- permission code unique
- role uniqueness
- role_permissions unique
- tenant user-role duplicate prevention
- platform user-role duplicate prevention
- valid tenant/platform scope combinations

IMPORTANT:

PostgreSQL NULL behavior for tenant_id on platform assignments must be
handled deliberately.

TDD:

RED
→ GREEN
→ REFACTOR
→ security/edge tests

DO NOT IMPLEMENT FEATURE 6:

- authorization middleware
- permission enforcement
- resource authorization
- policy engine
- super-admin bypass behavior
```

---

# 61. Workflow

```text
Feature 5 Specification
        ↓
Give planning prompt to Opus/Copilot
        ↓
Planning only
        ↓
DECISIONS REQUIRING APPROVAL
        ↓
Bring the plan back for review
        ↓
Lock decisions
        ↓
Copilot implementation with TDD
        ↓
go vet ./...
go test ./...
PostgreSQL integration tests
        ↓
Independent review
        ↓
Targeted fixes
        ↓
Feature 5 COMPLETE
        ↓
Feature 6 — Authorization & Access Enforcement
```