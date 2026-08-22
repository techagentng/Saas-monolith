# Multi-Tenant Booking System
# Epic 1 — Feature 6: Authorization & Access Enforcement

**Document Type:** Feature Implementation Specification / Context Restoration Document  
**Project:** Multi-Tenant Booking System  
**Epic:** Epic 1 — Identity & Access  
**Feature:** Feature 6 — Authorization & Access Enforcement  
**Backend:** Go  
**Database:** PostgreSQL  
**Architecture:** Modular Monolith  
**Request Flow:** API → Handler → Service → Repository → Database  
**Development Method:** Pure TDD / Test-First Development  

---

# 1. Purpose

This document defines the authoritative requirements for:

**Epic 1 — Feature 6: Authorization & Access Enforcement**

Feature 6 is the final feature in Epic 1.

It builds on:

1. Master Project Specification.
2. Epic 1 — Identity & Access specification.
3. Feature 1 — Application Error Infrastructure.
4. Feature 2 — User Identity.
5. Feature 3 — Authentication & Session Foundation.
6. Feature 4 — Tenant Membership & Context.
7. Feature 5 — Roles & Permissions.

Features 1–5 establish:

```text
User Identity
        ↓
Authentication
        ↓
Tenant Context
        ↓
Roles
        ↓
Permissions
```

Feature 6 introduces the actual authorization decision:

> Is this authenticated user allowed to perform this operation in this scope?

---

# 2. Core Security Model

The system must preserve these distinct concerns:

```text
Authentication
    ↓
Who is the user?

Tenant Context
    ↓
Which tenant is the request operating within?

Roles & Permissions
    ↓
What capabilities are assigned to this user?

Authorization
    ↓
May this user perform this specific action?
```

These concepts must not collapse into one another.

For example:

```text
Authenticated
≠
Tenant Member
≠
Has Permission
≠
Authorized For Resource
```

Feature 6 must enforce all required boundaries explicitly.

---

# 3. Feature Goal

Feature 6 must provide centralized, reusable authorization capabilities that:

- Require authenticated identity.
- Respect trusted tenant context.
- Resolve current server-side effective permissions.
- Enforce required permission codes.
- Preserve platform vs tenant scope.
- Prevent cross-tenant access.
- Support resource-level tenant validation where required.
- Default deny when authorization context is incomplete.
- Return standardized Feature 1 errors.
- Avoid authorization logic scattered across handlers.
- Provide tests proving tenant isolation and permission enforcement.

---

# 4. Feature Dependencies

## Feature 1

Reuse centralized error infrastructure.

Relevant error codes include:

```text
INVALID_REQUEST
PERMISSION_DENIED
TENANT_ACCESS_DENIED
RESOURCE_NOT_FOUND
SERVICE_UNAVAILABLE
INTERNAL_ERROR
```

Do not create ad hoc authorization error strings.

---

## Feature 2

Reuse user identity.

Do not create a second user model.

---

## Feature 3

Reuse authenticated:

```text
auth.Principal
├── UserID
└── SessionID
```

Feature 6 must not trust:

- `X-User-ID`
- arbitrary client identity headers
- user IDs supplied by the frontend as authenticated authority

The Feature 3 principal is the authenticated identity source.

---

## Feature 4

Reuse:

```text
TenantContext
└── TenantID
```

Tenant context must already have been validated through:

- authenticated principal
- active tenant
- active tenant membership

Feature 6 must not re-create tenant-context parsing logic.

---

## Feature 5

Reuse server-side effective permission resolution.

Feature 6 must not:

- duplicate roles.
- duplicate permissions.
- read role/permission claims from access tokens as authority.
- implement a second permission resolver.

Feature 5 remains the source of effective permission data.

---

# 5. Authorization Principle

Authorization should be centralized.

Avoid patterns such as:

```go
if user.Role == "BUSINESS_OWNER" {
    // allow
}
```

scattered through handlers and services.

Prefer a centralized capability conceptually similar to:

```text
AuthorizationService
    ↓
RequirePermission(...)
```

or:

```text
Can(...)
```

The exact API should be determined during planning.

The central goal is:

```text
Subject
+
Scope
+
Required Permission
+
Optional Resource Context
→
Decision
```

---

# 6. Default-Deny Rule

Authorization must be **default deny**.

If the system cannot establish all required security facts, access must be denied.

Examples:

```text
Missing Principal
→ deny

Missing TenantContext for tenant operation
→ deny

Permission resolver failure
→ deny safely

Required permission absent
→ deny

Tenant mismatch
→ deny

Resource tenant cannot be validated
→ deny
```

Authorization must never default to allow because information is missing.

---

# 7. Authorization Inputs

For a tenant-scoped operation, authorization may require:

```text
Authenticated Principal.UserID
Trusted TenantContext.TenantID
Required Permission Code
```

For resource-scoped operations it may additionally require:

```text
Resource TenantID
```

For platform-scoped operations it may require:

```text
Principal.UserID
Required Platform Permission
```

Tenant context must not be required for genuine platform-scoped authorization.

---

# 8. Tenant-Scoped Authorization

Conceptually:

```text
HTTP Request
        ↓
Authentication Middleware
        ↓
Principal
        ↓
Tenant Context Middleware
        ↓
Trusted TenantContext
        ↓
Authorization
        ↓
Resolve Effective Permissions
        ↓
Check Required Permission
        ↓
Handler / Service
```

The required permission must be evaluated within the trusted tenant scope.

Example:

```text
Principal.UserID = User A
TenantContext.TenantID = Tenant A
Required Permission = user.read
```

Feature 6 asks Feature 5:

```text
ResolvePermissions(User A, Tenant A)
```

and checks whether:

```text
user.read
```

is present.

---

# 9. Platform-Scoped Authorization

Platform operations must remain independent from tenant roles.

Conceptually:

```text
Principal
    ↓
Platform Permission Resolution
    ↓
Required Platform Permission
```

A tenant role such as:

```text
BUSINESS_OWNER
```

must never implicitly produce:

```text
SUPER_ADMIN
```

or platform-level authority.

Tenant membership must not create platform authority.

---

# 10. Role Names Must Not Be the Authorization Contract

Feature 6 should authorize using permission codes rather than role names wherever possible.

Prefer:

```text
RequirePermission("user.read")
```

not:

```text
RequireRole("BUSINESS_OWNER")
```

Roles are collections of permissions.

Permission codes are the actual capability contract.

Role-name checks may only be used where a future explicit business rule genuinely depends on role identity itself.

Do not implement role-name bypass logic as the normal authorization model.

---

# 11. Super Admin Behavior

`SUPER_ADMIN` exists as a platform role from Feature 5.

Feature 6 must determine how its authority works.

Preferred design:

```text
SUPER_ADMIN
→ receives platform permissions through normal permission resolution
```

Do not scatter:

```go
if role == "SUPER_ADMIN" {
    return true
}
```

through handlers.

If SUPER_ADMIN needs broad authority, that authority should result from:

```text
Role
→ Permissions
→ AuthorizationService
```

not hardcoded bypass checks.

---

# 12. Permission Enforcement

The authorization service should support explicit permission requirements.

Conceptually:

```text
RequireTenantPermission(
    ctx,
    principal,
    tenantContext,
    "user.read",
)
```

and:

```text
RequirePlatformPermission(
    ctx,
    principal,
    "permission.assign",
)
```

Exact names may vary.

Responsibilities:

- Validate required context.
- Resolve effective permissions.
- Check required code.
- Return success or typed authorization error.
- Preserve dependency errors safely.

---

# 13. Missing Permission

If the user lacks the required permission:

```text
PERMISSION_DENIED
```

should normally be returned.

Public message must remain generic.

Example:

```json
{
  "error": {
    "code": "PERMISSION_DENIED",
    "message": "You do not have permission to perform this action."
  }
}
```

Do not expose:

```text
You are missing role X
You need BUSINESS_OWNER
Your role only has Y permissions
```

unless future product requirements explicitly need that detail.

---

# 14. Tenant Access vs Permission Denial

Maintain the distinction:

```text
TENANT_ACCESS_DENIED
```

means the user cannot establish valid tenant access.

```text
PERMISSION_DENIED
```

means the user has valid tenant access but lacks the required capability.

Conceptually:

```text
Authenticated User
+
No Tenant Membership
→ TENANT_ACCESS_DENIED
```

versus:

```text
Authenticated User
+
Active Tenant Membership
+
No required permission
→ PERMISSION_DENIED
```

Do not blur these semantics.

---

# 15. Resource-Level Authorization

Permission checks alone are not sufficient for tenant-owned resources.

Example:

```text
User A
has booking.read in Tenant A
```

must not mean:

```text
User A can read Booking B belonging to Tenant B
```

For tenant-owned resources, the complete authorization model is:

```text
Authenticated User
+
Trusted TenantContext
+
Required Permission
+
Resource belongs to TenantContext
```

---

# 16. Resource Tenant Enforcement

Tenant-owned repositories/services must eventually enforce tenant scope in data access.

Prefer:

```sql
SELECT ...
FROM resource
WHERE id = $1
AND tenant_id = $2
```

rather than:

```sql
SELECT ...
FROM resource
WHERE id = $1
```

followed only by a later application comparison.

Feature 6 must define this as the required pattern for future tenant-owned modules.

Do not implement booking repositories merely to demonstrate it.

The feature should provide authorization infrastructure and tests using suitable existing/test resources.

---

# 17. IDOR / BOLA Protection

Feature 6 must explicitly protect against insecure direct object reference / broken object-level authorization.

Knowing:

```text
resource UUID
```

must never grant access.

The system must verify:

```text
resource belongs to trusted tenant
+
subject has required permission
```

before allowing access.

Cross-tenant IDs must fail safely.

---

# 18. Information Disclosure

Authorization errors must avoid unnecessary disclosure.

For tenant-owned resources, where revealing existence is sensitive, the application may collapse:

```text
resource exists but belongs to another tenant
```

into:

```text
RESOURCE_NOT_FOUND
```

or another approved non-disclosing response.

The exact resource-disclosure policy must be explicitly determined.

Do not automatically reveal:

```text
"This booking exists but belongs to another tenant."
```

---

# 19. Authorization Middleware vs Service-Level Authorization

Feature 6 may use both:

```text
middleware
```

and:

```text
service-level checks
```

but they have different purposes.

Middleware is suitable for:

```text
route requires permission X
```

Service authorization is still required when authorization depends on:

- target resource.
- resource tenant ownership.
- dynamic business context.
- action-specific domain rules.

Do not assume middleware alone is sufficient.

---

# 20. Middleware Authorization

A permission middleware may conceptually:

```text
Authentication Middleware
        ↓
TenantContext Middleware
        ↓
RequirePermission("user.read")
        ↓
Handler
```

It should:

- Use existing Principal.
- Use existing TenantContext.
- Call centralized authorization service.
- Deny safely if required context is missing.
- Avoid embedding role logic.
- Avoid SQL.
- Avoid duplicating permission resolution.

---

# 21. Service-Level Enforcement

Sensitive business operations should enforce authorization at a layer that cannot be bypassed merely by invoking a service from another handler.

The planning agent must decide where permission requirements belong for:

```text
HTTP-only authorization
```

versus:

```text
domain/business authorization
```

A safe principle is:

> Handlers/middleware enforce route-level access; services enforce resource/business invariants where bypass would otherwise be possible.

Do not duplicate identical permission checks unnecessarily across every layer.

---

# 22. Authorization Service Responsibilities

Potential responsibilities:

- Require tenant permission.
- Require platform permission.
- Resolve effective permissions.
- Validate trusted tenant context.
- Check membership-derived authority through Feature 5.
- Return typed authorization errors.
- Provide reusable decision helpers.

It must not:

- Parse HTTP headers.
- Parse JWTs.
- Resolve tenant IDs from routes.
- Execute business repository SQL.
- Duplicate Feature 5 permission storage.
- Create roles or permissions.

---

# 23. Authorization Decision Object

The implementation may simply return:

```text
nil
```

for allowed and:

```text
error
```

for denied.

Avoid building an elaborate policy-decision object unless there is a concrete requirement.

A simple interface may be sufficient:

```text
RequireTenantPermission(...)
error
```

or:

```text
Can(...)
(bool, error)
```

The exact shape should be chosen for clarity and secure defaults.

---

# 24. Permission Code Usage

Required permissions must use Feature 5 stable permission codes.

Do not write random string literals throughout the codebase if central permission constants exist.

Prefer:

```go
permissions.UserRead
```

or equivalent stable constants.

Do not duplicate:

```text
"user.read"
```

in dozens of unrelated files if the project establishes a centralized permission-code package.

---

# 25. Permission Resolution Freshness

Feature 6 must use current Feature 5 server-side state.

Do not authorize using stale:

- Access-token role claims.
- Access-token permission claims.
- Client-supplied role names.
- Cached permission sets.

Initially:

```text
Current database-backed permission state
```

is authoritative.

This ensures:

- Membership revocation takes effect.
- Role removal takes effect.
- Permission removal takes effect.
- Tenant disablement takes effect.

---

# 26. Caching

Do not add authorization/permission caching in Feature 6 unless explicitly approved.

Correctness is more important than premature optimization.

Caching would introduce:

- revocation delays.
- stale permission risks.
- invalidation complexity.

Defer until performance evidence requires it.

---

# 27. Membership Revocation Interaction

Feature 4 membership revocation must immediately remove tenant authority.

Expected:

```text
Session remains valid.
        +
Membership becomes DISABLED.
        ↓
TenantContext cannot be established.
        ↓
Tenant authorization denied.
```

Feature 6 must not allow old role assignments to bypass this boundary.

---

# 28. Role Removal Interaction

If Feature 5 removes a role assignment:

```text
Next permission resolution
→ permission disappears
→ authorization fails if permission was required
```

No access-token renewal should be necessary.

---

# 29. Permission Removal Interaction

If a permission is removed from a role:

```text
Next permission resolution
→ capability disappears
```

No stale token permission should preserve access.

---

# 30. Tenant Disablement Interaction

If a tenant becomes:

```text
DISABLED
```

Feature 4 tenant context must fail.

Feature 6 must not bypass Feature 4 and authorize solely from a role assignment.

---

# 31. Platform Authorization

Platform-scoped operations should use a separate explicit method or scope.

Conceptually:

```text
RequirePlatformPermission(
    principal,
    "role.read",
)
```

Do not accidentally:

```text
RequireTenantPermission(...)
```

for platform administration.

Platform and tenant permission resolution must remain clearly separated.

---

# 32. Production Route Integration

Feature 6 is where routes previously intentionally left unregistered may begin receiving real authorization policies.

Examples from earlier features include:

```text
tenant membership management
role assignment
permission management
```

Feature 6 should not register everything automatically.

For every route added, explicitly document:

```text
Authentication requirement
Tenant-context requirement
Required permission
Resource-scope requirement
```

---

# 33. Route Authorization Matrix

Feature 6 should introduce an explicit authorization matrix for routes that are enabled.

Conceptually:

| Operation | Scope | Required Permission |
|---|---|---|
| Read tenant | TENANT | `tenant.read` |
| Update tenant | TENANT | `tenant.update` |
| Read users | TENANT | `user.read` |
| Create user/member | TENANT | `user.create` |
| Update user/member | TENANT | `user.update` |
| Disable user/member | TENANT | `user.disable` |
| Read roles | TENANT/PLATFORM according to route | `role.read` |
| Assign role | TENANT/PLATFORM according to route | `role.assign` |
| Read permissions | appropriate scope | `permission.read` |

This matrix must use only existing seeded permissions.

Do not invent future booking permissions in Feature 6.

---

# 34. SUPER_ADMIN Route Policy

Platform `SUPER_ADMIN` currently has all seeded Feature 5 permissions.

Feature 6 must explicitly determine:

- Which routes are platform-scoped.
- Which tenant operations, if any, platform administrators may perform.
- Whether platform authority can operate across tenants directly.

Do not assume:

```text
SUPER_ADMIN automatically bypasses every tenant boundary
```

without an explicit policy.

Preferred approach:

- Platform authorization uses platform permissions.
- Tenant operations remain tenant-scoped unless a specific platform administration route exists.

---

# 35. BUSINESS_OWNER Policy

Feature 5 grants BUSINESS_OWNER:

```text
user.read
user.create
user.update
user.disable

tenant.read
tenant.update

role.read
role.assign

permission.read
```

Feature 6 should enforce these permissions explicitly.

BUSINESS_OWNER should not receive:

```text
role.create
role.update
role.delete
permission.assign
```

unless the Feature 5 matrix is later intentionally changed.

---

# 36. STAFF Policy

Feature 5 grants STAFF:

```text
tenant.read
user.read
role.read
permission.read
```

Feature 6 must verify STAFF cannot perform mutation actions requiring:

```text
user.create
user.update
user.disable
tenant.update
role.assign
```

The tests must prove this.

---

# 37. Authorization Error Contract

Feature 6 should primarily use:

```text
PERMISSION_DENIED
TENANT_ACCESS_DENIED
RESOURCE_NOT_FOUND
INVALID_REQUEST
SERVICE_UNAVAILABLE
INTERNAL_ERROR
```

Do not add:

```text
ROLE_NOT_ALLOWED
STAFF_FORBIDDEN
OWNER_ONLY
```

unless a genuine stable business requirement exists.

Authorization should remain capability-based.

---

# 38. Handler Behavior

Handlers should:

- Receive already parsed/authenticated context.
- Invoke service/authorization capabilities.
- Return safe Feature 1 errors.
- Avoid hardcoded role comparisons.
- Avoid querying role/permission repositories directly.
- Avoid SQL.

Bad:

```go
if principal.Role != "BUSINESS_OWNER" {
    ...
}
```

Good:

```text
authorization.RequireTenantPermission(...)
```

---

# 39. Repository Behavior

Repositories should never infer authorization merely from receiving an ID.

Tenant-owned repository queries must accept trusted tenant scope where relevant.

Future repositories should prefer:

```text
FindByIDAndTenant(...)
```

over unsafe global lookups for tenant-owned resources.

Feature 6 should establish this architectural rule.

---

# 40. Trusted vs Untrusted Data

Trusted sources:

```text
auth.Principal
TenantContext
server-side permission resolver
repository-loaded resource tenant ownership
```

Untrusted sources:

```text
request body user ID
X-User-ID
X-Tenant-ID
role names sent by client
permission list sent by client
tenant ID from client without Feature 4 validation
resource ID by itself
```

Authorization must never elevate untrusted inputs into trusted authority without server-side validation.

---

# 41. Cross-Tenant Enforcement

Mandatory invariant:

```text
User A
belongs to Tenant A
has user.read in Tenant A
```

must not allow:

```text
User A
to read Tenant B users/resources
```

even if:

- User A knows Tenant B UUID.
- User A knows resource UUID.
- User A modifies the route.
- User A submits another tenant ID in JSON.
- User A spoofs headers.

---

# 42. Cross-User Enforcement

For operations targeting a user:

```text
Target UserID
```

must be treated as a resource identifier, not authenticated identity.

The actor comes from:

```text
Principal.UserID
```

Do not confuse:

```text
actor user
```

with:

```text
target user
```

This distinction is critical for administrative actions.

---

# 43. Self-Service vs Administrative Operations

Feature 6 must not silently assume:

```text
users can edit themselves
```

or:

```text
BUSINESS_OWNER can modify every user
```

unless the permission matrix and endpoint policy explicitly allow it.

Self-service behavior should remain separate from administrative RBAC behavior unless defined.

Do not invent profile-management rules.

---

# 44. Authorization Middleware Design

Potential middleware factory:

```text
RequireTenantPermission(permissionCode)
```

Conceptually:

```text
func RequireTenantPermission(
    authorizer Authorizer,
    permission PermissionCode,
) Middleware
```

and perhaps:

```text
RequirePlatformPermission(permissionCode)
```

The exact interface should remain minimal.

Do not create a generic policy language or DSL.

---

# 45. Middleware Ordering

Tenant routes requiring permissions should use:

```text
Recovery / Request ID
        ↓
Authentication
        ↓
Tenant Context Resolution
        ↓
Authorization
        ↓
Handler
```

Authorization must not run before authentication or tenant-context resolution where tenant scope is required.

Platform routes may use:

```text
Authentication
        ↓
Platform Authorization
        ↓
Handler
```

---

# 46. Missing Context Behavior

Required behavior:

```text
Missing Principal
→ authentication failure
```

```text
Missing TenantContext on tenant-protected operation
→ TENANT_ACCESS_DENIED or safe internal configuration error according to approved middleware contract
```

The implementation must not panic.

A misconfigured route should fail closed.

---

# 47. Authorization Service Failure

If permission resolution fails unexpectedly:

```text
do not allow request
```

Return the appropriate safe infrastructure/internal error.

Never implement:

```go
if err != nil {
    return allow
}
```

Authorization dependency errors must fail closed.

---

# 48. Public Error Messages

Do not expose detailed authorization internals.

Prefer:

```text
You do not have permission to perform this action.
```

Avoid:

```text
You are STAFF but this route requires BUSINESS_OWNER.
```

Avoid exposing:

```text
permission matrix
internal role IDs
database relationships
```

---

# 49. Audit Boundary

Authorization decisions may eventually be audited, but full audit infrastructure remains outside the current implementation unless Epic 1 already contains an approved audit feature.

Do not add a new audit subsystem merely because authorization exists.

Logging a safe denied-operation event through existing infrastructure may be acceptable.

Never log secrets.

---

# 50. TDD Requirements

Feature 6 must use pure TDD.

Recommended implementation sequence:

1. Inspect Features 1–5.
2. Identify currently unregistered routes intended for authorization.
3. Produce exact route/permission matrix.
4. Write failing authorization service tests.
5. Write failing tenant permission tests.
6. Write failing platform permission tests.
7. Write failing missing-context tests.
8. Write failing cross-tenant tests.
9. Write failing membership-revocation tests.
10. Write failing role-removal tests.
11. Write failing permission-removal tests.
12. Write failing middleware-order tests.
13. Write failing route authorization tests.
14. Write failing IDOR/resource tenant tests where existing resources permit.
15. Implement minimal authorization service.
16. Implement permission middleware.
17. Wire only approved routes.
18. Add resource-scope enforcement where applicable.
19. Run focused tests.
20. Run PostgreSQL integration tests.
21. Run:

```text
go vet ./...
go test ./...
```

22. Conduct independent security review.
23. Stop before beginning Epic 2.

---

# 51. Authorization Service Tests

Test:

```text
User has required tenant permission
→ allowed
```

```text
User lacks required tenant permission
→ PERMISSION_DENIED
```

```text
User has permission in Tenant A
requests same capability in Tenant B
→ denied
```

```text
Missing TenantContext
→ denied safely
```

```text
Disabled membership
→ TENANT_ACCESS_DENIED
```

```text
Revoked membership after prior authorization
→ next request denied
```

```text
Role assignment removed
→ next request denied
```

```text
Permission removed from role
→ next request denied
```

```text
Permission resolver error
→ fail closed
```

---

# 52. Platform Authorization Tests

Test:

```text
SUPER_ADMIN
+
required platform permission
→ allowed
```

Test:

```text
BUSINESS_OWNER tenant permission
→ does not satisfy platform permission
```

Test:

```text
STAFF tenant permissions
→ never create platform authority
```

Test:

```text
No platform role
→ platform permission denied
```

Test:

```text
TenantContext present
→ does not automatically create platform authority
```

---

# 53. BUSINESS_OWNER Tests

Using Feature 5's approved matrix, test that BUSINESS_OWNER can satisfy:

```text
user.read
user.create
user.update
user.disable
tenant.read
tenant.update
role.read
role.assign
permission.read
```

and cannot satisfy:

```text
role.create
role.update
role.delete
permission.assign
```

---

# 54. STAFF Tests

Test STAFF can satisfy:

```text
tenant.read
user.read
role.read
permission.read
```

Test STAFF cannot satisfy:

```text
user.create
user.update
user.disable
tenant.update
role.assign
role.create
role.update
role.delete
permission.assign
```

---

# 55. Middleware Tests

Test:

- Authentication principal is required.
- TenantContext is required for tenant permission middleware.
- Valid permission passes.
- Missing permission returns `PERMISSION_DENIED`.
- Feature 5 resolution error fails closed.
- Tenant A permission cannot satisfy Tenant B route.
- Spoofed role header ignored.
- Spoofed permission header ignored.
- Spoofed tenant header ignored.
- Middleware does not inspect role names.
- Middleware does not query SQL directly.
- Middleware ordering is correct.
- Nil/missing context does not panic.

---

# 56. Route Integration Tests

For every production route enabled by Feature 6, test:

```text
Unauthenticated
→ 401
```

```text
Authenticated but no tenant access
→ 403 TENANT_ACCESS_DENIED
```

```text
Valid tenant access but missing permission
→ 403 PERMISSION_DENIED
```

```text
Required permission present
→ handler executes
```

Also verify:

- Correct error envelope.
- Correct tenant scope.
- No role detail leakage.
- No database error leakage.

---

# 57. Cross-Tenant Security Tests

Mandatory:

```text
User A
BUSINESS_OWNER in Tenant A
STAFF in Tenant B
```

Test:

```text
Tenant A update
→ allowed
```

because BUSINESS_OWNER has:

```text
tenant.update
```

Test:

```text
Tenant B update
→ denied
```

because STAFF lacks:

```text
tenant.update
```

Test:

```text
Tenant B read
→ allowed
```

if Feature 4 membership is active and STAFF has:

```text
tenant.read
```

This is a key proof that authorization is scoped by tenant, not merely user.

---

# 58. Membership Revocation Security Test

Scenario:

```text
User A authenticated.
User A has BUSINESS_OWNER role in Tenant A.
User A has tenant.update.
```

Then:

```text
Membership Tenant A → DISABLED
```

Expected:

```text
Feature 3 session remains valid.
TenantContext resolution fails.
Authorization fails.
```

No access-token renewal is required.

---

# 59. Role Removal Security Test

Scenario:

```text
User A has STAFF in Tenant A.
```

Remove assignment.

Expected:

```text
Next permission resolution
→ no STAFF permissions
→ authorization denied
```

No stale authorization state should remain.

---

# 60. Permission Removal Security Test

Scenario:

```text
Role has tenant.update.
```

Remove:

```text
tenant.update
```

Expected:

```text
Next request requiring tenant.update
→ PERMISSION_DENIED
```

No token refresh required.

---

# 61. IDOR / Resource Security Tests

Where an existing tenant-owned resource exists, test:

```text
User A has read permission in Tenant A.
Resource B belongs to Tenant B.
User A requests Resource B using its UUID.
→ denied/not found according to approved disclosure policy.
```

If no suitable tenant-owned business resource exists yet, Feature 6 must still establish the architectural rule and may use a test resource/fake repository to prove the authorization contract.

Do not create a booking module just for this test.

---

# 62. SQL / Repository Tests

Where Feature 6 requires tenant resource access, test:

- Resource queries include tenant scope.
- Raw global `FindByID` is not used for tenant-owned authorization-sensitive resources where unsafe.
- Tenant ID is trusted context, not raw client input.
- SQL is parameterized.
- Cross-tenant lookup returns no accessible resource.

---

# 63. Route Registration Strategy

Feature 6 should register only routes whose permission policies are explicitly defined.

Candidate previously deferred capabilities include:

```text
Tenant membership management
Role assignment
Role/permission read capabilities
```

Do not expose every internal handler automatically.

Each route must have an entry in the approved authorization matrix.

---

# 64. Candidate Authorization Matrix

Initial matrix may include:

```text
TENANT SCOPE

Read tenant
→ tenant.read

Update tenant
→ tenant.update

Read tenant users/members
→ user.read

Create/add tenant member/user
→ user.create

Update tenant user/member
→ user.update

Disable/revoke tenant member
→ user.disable

Read roles
→ role.read

Assign role
→ role.assign

Read permissions
→ permission.read
```

Platform routes may include:

```text
Read roles
→ role.read

Read permissions
→ permission.read

Create role
→ role.create

Update role
→ role.update

Delete role
→ role.delete

Assign permissions
→ permission.assign
```

However, system roles are currently immutable and dynamic permissions are excluded.

Therefore platform mutation routes should only be enabled if their actual Feature 5 capability exists and policy is explicitly approved.

Do not expose dead or unsupported endpoints merely because permission codes exist.

---

# 65. Public DTOs

Authorization responses should generally not expose authorization internals.

Normal successful business responses should remain business DTOs.

Do not return:

```json
{
  "allowed": true,
  "roles": [...],
  "permissions": [...]
}
```

for every endpoint.

Authorization should usually be invisible when successful.

On denial, return the centralized error contract.

---

# 66. Effective Permission Exposure

Do not create a public:

```text
GET /permissions/effective
```

endpoint automatically.

Frontend permission awareness may be needed later, but exposing permission sets is a separate API design decision.

Feature 6 should focus on backend enforcement.

---

# 67. Frontend Boundary

Frontend RBAC remains outside this backend feature.

Frontend may later hide or show UI based on server-provided authorization data.

But:

```text
hidden button
≠ security
```

The backend must independently enforce every protected operation.

---

# 68. Performance

Do not prematurely optimize authorization.

Correctness priorities:

```text
Current server-side permission state
Current membership state
Current tenant state
Correct resource tenant scope
```

Only add caching after profiling demonstrates a real need.

---

# 69. Security Requirements

Mandatory:

- Default deny.
- Authenticate first.
- Tenant context before tenant permission enforcement.
- Resolve permissions server-side.
- Keep tenant/platform scopes separate.
- Do not trust role/permission client claims.
- Do not trust tenant IDs without Feature 4 validation.
- Do not use role names as the primary authorization contract.
- Prevent cross-tenant access.
- Prevent IDOR/BOLA.
- Fail closed on resolver errors.
- No stale permission caching.
- No Feature 3 token permission authority.
- Parameterized SQL.
- No raw database error exposure.
- Explicit route authorization matrix.
- Explicit resource tenant validation.
- No scattered SUPER_ADMIN bypass checks.

---

# 70. Scope Boundaries

Feature 6 must not implement:

- Booking business functionality.
- Payment authorization.
- Billing.
- Tenant branding.
- Notifications.
- Frontend RBAC.
- Permission caching.
- Policy language/DSL.
- ABAC engine.
- Complex organization hierarchy.
- Audit subsystem.
- MFA.
- OAuth.
- SSO.
- Password reset.
- Tenant invitations unless already part of an approved endpoint.
- Epic 2 business features.

This feature completes the **Identity & Access foundation**.

---

# 71. Acceptance Criteria

Feature 6 is complete when:

- [ ] Central authorization service exists.
- [ ] Tenant permission checks use Feature 5 permission resolution.
- [ ] Platform permission checks use Feature 5 platform resolution.
- [ ] Missing permissions return `PERMISSION_DENIED`.
- [ ] Tenant access failures remain `TENANT_ACCESS_DENIED`.
- [ ] Authorization defaults to deny.
- [ ] Resolver failures fail closed.
- [ ] Tenant and platform scopes remain separate.
- [ ] No hardcoded role bypass exists.
- [ ] SUPER_ADMIN authority flows through permissions.
- [ ] BUSINESS_OWNER permissions match Feature 5.
- [ ] STAFF restrictions match Feature 5.
- [ ] Membership revocation immediately removes tenant authority.
- [ ] Role removal immediately affects authorization.
- [ ] Permission removal immediately affects authorization.
- [ ] No token refresh is needed for permission changes.
- [ ] TenantContext is required for tenant-scoped authorization.
- [ ] Principal is required.
- [ ] Cross-tenant permission leakage is prevented.
- [ ] IDOR/resource tenant checks are established.
- [ ] Authorization middleware exists where appropriate.
- [ ] Service-level resource checks exist where appropriate.
- [ ] Route authorization matrix is explicit.
- [ ] Only approved routes are registered.
- [ ] Feature 1 error infrastructure is reused.
- [ ] Feature 3 authentication infrastructure is reused.
- [ ] Feature 4 tenant context is reused.
- [ ] Feature 5 role/permission resolution is reused.
- [ ] No permission caching exists.
- [ ] SQL is parameterized.
- [ ] Raw database errors do not leak.
- [ ] Tests were written before implementation.
- [ ] Authorization service tests pass.
- [ ] Platform authorization tests pass.
- [ ] Tenant authorization tests pass.
- [ ] Cross-tenant security tests pass.
- [ ] Middleware tests pass.
- [ ] Route integration tests pass.
- [ ] PostgreSQL integration tests pass where configured.
- [ ] `go vet ./...` passes.
- [ ] `go test ./...` passes.
- [ ] No Epic 2 functionality was introduced.

---

# 72. Decisions Requiring Explicit Approval

Before implementation, the planning agent must determine:

1. Exact Authorizer interface.
2. Whether authorization returns `error` or `(bool, error)`.
3. Exact tenant permission-check API.
4. Exact platform permission-check API.
5. Middleware factory shape.
6. Exact missing-TenantContext behavior.
7. Exact resource disclosure policy.
8. Exact IDOR handling strategy.
9. Which existing deferred routes become publicly registered now.
10. Exact route/permission matrix.
11. Which routes remain internal/deferred.
12. Whether role read endpoints are registered.
13. Whether permission read endpoints are registered.
14. Whether membership creation is registered.
15. Whether membership revocation is registered.
16. Whether role assignment is registered.
17. Whether role removal is registered.
18. Whether platform RBAC management routes are registered.
19. How SUPER_ADMIN interacts with tenant operations.
20. Whether SUPER_ADMIN requires explicit tenant context for tenant-scoped routes.
21. Whether platform administrators get separate platform routes.
22. Whether BUSINESS_OWNER may assign STAFF.
23. Whether BUSINESS_OWNER may assign BUSINESS_OWNER.
24. Whether BUSINESS_OWNER may ever assign SUPER_ADMIN.
25. Whether STAFF can manage memberships.
26. Exact self-service behavior, if any.
27. Whether effective permission endpoint remains excluded.
28. Whether role names are ever used directly for policy.
29. Exact middleware ordering.
30. Where service-level authorization is required in addition to middleware.
31. Whether resource ownership checks are implemented now or only architectural contracts/tests.
32. Whether any existing Feature 1 error codes need expansion.
33. Whether authorization denial is always HTTP 403.
34. Whether inaccessible tenant resources become 404 or 403.
35. Whether missing tenant context from route misconfiguration is treated as 403 or internal error.
36. Exact integration-test strategy.
37. Whether PostgreSQL integration tests must execute actual migrations.
38. Confirmation that permission caching remains excluded.
39. Confirmation that access-token role/permission authority remains excluded.
40. Confirmation that Epic 2 business logic remains outside scope.

Do not silently invent these decisions.

---

# 73. PLANNING PROMPT FOR COPILOT / OPUS

```text
We are now implementing Epic 1, Feature 6 —
Authorization & Access Enforcement.

This is the final feature in Epic 1.

Features 1–5 are complete.

Authoritative documentation:

1. Master Project Specification.
2. Epic 1 — Identity & Access specification.
3. Feature 1 — Application Error Infrastructure.
4. Feature 2 — User Identity.
5. Feature 3 — Authentication & Session Foundation.
6. Feature 4 — Tenant Membership & Context.
7. Feature 5 — Roles & Permissions.
8. Feature 6 — Authorization & Access Enforcement specification.

IMPORTANT:

DO NOT IMPLEMENT CODE YET.

First inspect the actual repository.

Produce a detailed implementation plan for Feature 6 only.

Preserve:

API → Handler → Service → Repository → Database

Feature 6 must use:

Feature 3
→ Principal{UserID, SessionID}

Feature 4
→ trusted TenantContext{TenantID}

Feature 5
→ current server-side effective permission resolution

Feature 6 answers:

"May this authenticated user perform this specific action in this scope?"

Do NOT redesign Features 1–5.

Inspect:

- auth.Principal.
- Authentication middleware.
- TenantContext.
- Tenant-context middleware.
- Membership services.
- Role/permission models.
- Effective permission resolver.
- Deferred/unregistered handlers from Features 4 and 5.
- Application router.
- Existing error infrastructure.
- Existing integration tests.
- Migration conventions.

Your plan must cover:

1. Authorizer interface.
2. Tenant-scoped authorization.
3. Platform-scoped authorization.
4. Permission enforcement.
5. Default-deny behavior.
6. Missing-context behavior.
7. Authorization middleware.
8. Middleware ordering.
9. Service-level authorization.
10. Resource-level authorization.
11. IDOR/BOLA prevention.
12. Resource tenant enforcement.
13. Cross-tenant isolation.
14. SUPER_ADMIN behavior.
15. BUSINESS_OWNER behavior.
16. STAFF behavior.
17. Membership-revocation effects.
18. Role-removal effects.
19. Permission-removal effects.
20. Route authorization matrix.
21. Which deferred routes become public.
22. Which routes remain deferred.
23. Feature 1 error mapping.
24. Public disclosure policy.
25. Database/repository scope requirements.
26. TDD sequence.
27. Authorization service tests.
28. Middleware tests.
29. Cross-tenant security tests.
30. Route integration tests.
31. IDOR/resource tests.
32. PostgreSQL integration tests.
33. Files to create.
34. Files to modify.
35. Explicit scope exclusions.

SECURITY REQUIREMENTS:

- Default deny.
- Fail closed.
- Authenticate before authorization.
- Resolve trusted tenant context before tenant authorization.
- Use permission codes, not role-name shortcuts.
- Resolve permissions from current server-side state.
- Never trust client role/permission claims.
- Never trust X-User-ID.
- Never trust X-Tenant-ID.
- Never trust tenant IDs without Feature 4 validation.
- Prevent cross-tenant privilege leakage.
- Prevent IDOR/BOLA.
- Tenant-owned resources must enforce tenant scope.
- SUPER_ADMIN must not be implemented through scattered bypass checks.
- No permission caching.
- No role/permission authority from Feature 3 tokens.
- Raw database errors must never reach clients.
- SQL must be parameterized.

PURE TDD IS REQUIRED.

Show:

RED
→ GREEN
→ REFACTOR

for each major behavior.

DO NOT IMPLEMENT:

- Epic 2 business functionality.
- Booking business logic.
- Billing.
- Tenant branding.
- Notifications.
- Frontend RBAC.
- Policy DSL.
- Generic ABAC engine.
- Permission caching.
- Authentication redesign.
- Token redesign.
- Audit infrastructure unless already approved separately.

At the end provide:

DECISIONS REQUIRING APPROVAL

For each unresolved decision provide:

- Decision.
- Recommended option.
- Alternatives.
- Security implications.
- Complexity implications.
- Why you recommend it.

Do not write production code until the plan is approved.
```

---

# 74. IMPLEMENTATION PROMPT

Use this only after reviewing and approving the Feature 6 plan.

```text
The Feature 6 implementation plan and explicit decisions are approved.

Implement only:

Epic 1 — Feature 6: Authorization & Access Enforcement.

Features 1–5 are immutable dependencies unless an actual blocking defect
is discovered.

Preserve:

API → Handler → Service → Repository → Database

Use pure TDD:

Requirement
→ RED failing test
→ confirm failure
→ minimum implementation
→ GREEN
→ refactor
→ security/edge cases

Use:

Feature 3:
Principal{UserID, SessionID}

Feature 4:
Trusted TenantContext{TenantID}

Feature 5:
Server-side effective permission resolution

Mandatory requirements:

- Default deny.
- Fail closed.
- Use permission codes as authorization contracts.
- Do not scatter role-name checks.
- Do not scatter SUPER_ADMIN bypass checks.
- Tenant authorization requires trusted TenantContext.
- Platform authorization remains separate.
- Cross-tenant role/permission leakage must be impossible.
- Membership revocation immediately removes tenant authority.
- Role removal immediately affects authorization.
- Permission removal immediately affects authorization.
- No token refresh required for permission changes.
- Prevent IDOR/BOLA.
- Tenant-owned resource access must include tenant scope.
- Do not trust client role, permission, user, or tenant claims.
- No permission caching.
- Use Feature 1 typed errors.
- Use parameterized SQL.
- Never expose raw database errors.

Only register routes explicitly approved in the authorization matrix.

Do not expose internal handlers merely because they exist.

Do NOT implement:

- Booking business functionality.
- Billing.
- Branding.
- Notifications.
- Frontend RBAC.
- Generic policy engine.
- ABAC framework.
- Permission caching.
- Authentication redesign.
- Token redesign.
- Epic 2 functionality.

If a blocking defect is discovered in Features 1–5:

STOP.

Report:

1. Defect.
2. Evidence.
3. Why Feature 6 cannot safely continue.
4. Minimum proposed fix.

Do not silently modify completed features.

After implementation run:

go vet ./...
go test ./...

and all configured PostgreSQL integration tests.

Report:

1. Files created.
2. Files modified.
3. Authorization service added.
4. Middleware added.
5. Routes registered.
6. Exact route-permission matrix implemented.
7. Cross-tenant tests.
8. IDOR/resource tests.
9. Membership-revocation tests.
10. Role/permission change tests.
11. PostgreSQL integration-test results.
12. go vet result.
13. go test result.
14. Any deviation from approved plan.
15. Confirmation that no Epic 2 functionality was introduced.
```

---

# 75. INDEPENDENT SECURITY REVIEW PROMPT

```text
SECURITY REVIEW ONLY.

Do not modify files.

Review:

Epic 1 — Feature 6: Authorization & Access Enforcement

Authoritative sources:

1. Master Project Specification.
2. Epic 1 specification.
3. Feature 1 specification.
4. Feature 2 specification.
5. Feature 3 specification.
6. Feature 4 specification.
7. Feature 5 specification.
8. Feature 6 specification.
9. Approved Feature 6 implementation plan.
10. Approved Feature 6 decisions.

Inspect actual production code, tests, route registration, repositories,
middleware, services, and migrations.

Verify:

AUTHENTICATION BOUNDARY

- Principal comes only from Feature 3.
- Client-supplied identity cannot override Principal.
- Missing authentication fails safely.

TENANT BOUNDARY

- TenantContext comes only from Feature 4 validation.
- Client tenant IDs cannot establish authority.
- Missing membership denies tenant access.
- Disabled membership denies tenant access.
- Disabled tenant denies tenant access.

PERMISSION BOUNDARY

- Effective permissions come from Feature 5.
- No token role/permission claims are trusted.
- No arbitrary client permission list is trusted.
- No permission cache preserves stale access.

AUTHORIZATION

- Default deny.
- Fail closed.
- Missing permission returns PERMISSION_DENIED.
- Missing tenant access returns TENANT_ACCESS_DENIED.
- Permission resolver failures never allow access.
- Role names are not used as widespread authorization shortcuts.

SUPER_ADMIN

- No scattered hardcoded super-admin bypass.
- Platform authority flows through Feature 5 permissions.
- Tenant behavior matches the approved policy.

BUSINESS_OWNER

Verify allowed permissions:

user.read
user.create
user.update
user.disable
tenant.read
tenant.update
role.read
role.assign
permission.read

Verify denied permissions:

role.create
role.update
role.delete
permission.assign

STAFF

Verify allowed permissions:

tenant.read
user.read
role.read
permission.read

Verify mutation permissions are denied.

CROSS-TENANT SECURITY

Test/inspect:

- Tenant A permissions cannot authorize Tenant B actions.
- Route tenant substitution fails.
- Resource UUID alone grants nothing.
- Spoofed X-Tenant-ID fails.
- Spoofed X-User-ID fails.
- Role ID alone grants nothing.
- Permission code supplied by client grants nothing.

IDOR / BOLA

- Tenant-owned resources are queried/validated with trusted tenant scope.
- Cross-tenant resources cannot be accessed merely by ID.
- Resource existence is not unnecessarily disclosed.

REVOCATION

- Membership disablement immediately removes tenant authority.
- Role removal immediately changes authorization.
- Permission removal immediately changes authorization.
- No token refresh is required.
- Authentication session remains separate where expected.

MIDDLEWARE

- Correct ordering:
  Authentication
  → TenantContext where required
  → Authorization
  → Handler

- Nil/missing context does not panic.
- Middleware does not execute SQL directly.
- Middleware delegates permission resolution correctly.

SERVICES

- Sensitive business/resource checks cannot be bypassed by invoking
  services through another handler where service-level enforcement is
  required.

ROUTES

- Every registered protected route has an explicit authorization policy.
- No unrestricted RBAC administrative route remains.
- Internal/test handlers are not accidentally public.
- Route matrix matches approved decisions.

ERRORS

- Feature 1 error contract reused.
- PERMISSION_DENIED correctly maps.
- TENANT_ACCESS_DENIED correctly maps.
- No raw database/internal errors leak.
- Error messages do not reveal internal role structures.

DATABASE

- SQL is parameterized.
- Tenant-owned repository queries use tenant scope where applicable.
- Cross-tenant queries cannot bypass authorization boundaries.

TESTING

Verify explicit tests for:

- allowed permission
- missing permission
- tenant mismatch
- platform vs tenant separation
- SUPER_ADMIN
- BUSINESS_OWNER
- STAFF
- membership revocation
- role removal
- permission removal
- resolver failure
- missing Principal
- missing TenantContext
- spoofed headers
- route substitution
- IDOR
- middleware ordering
- fail-closed behavior

Run or inspect:

go vet ./...
go test ./...

and all configured PostgreSQL integration tests.

Classify findings:

CRITICAL
IMPORTANT
MINOR

CRITICAL includes:

- cross-tenant access
- authorization bypass
- fail-open behavior
- privilege escalation
- stale authorization authority
- client-controlled authorization
- IDOR/BOLA
- super-admin bypass flaws

Do not confuse stylistic preferences with security/specification defects.

End with:

FEATURE 6 VERDICT

Choose exactly one:

APPROVED
APPROVED AFTER TARGETED FIXES
NOT APPROVED

Then list only required fixes before Epic 1 can be declared complete.

Do not modify files.
```

---

# 76. Context Restoration Summary

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
F5 Roles & Permissions

CURRENT:

F6 Authorization & Access Enforcement

FEATURE 6 PURPOSE:

Answer:

"May this authenticated user perform this specific action in this scope?"

SECURITY PIPELINE:

Authentication
→ Principal

Tenant Context
→ Trusted TenantID

Permissions
→ Current server-side effective permissions

Authorization
→ Required permission + scope + optional resource tenant

ARCHITECTURE:

API → Handler → Service → Repository → Database

AUTHORIZATION:

Prefer permission checks.

Do not scatter role checks.

DO NOT:

if role == BUSINESS_OWNER

Prefer:

RequirePermission(user.update)

DEFAULT:

DENY.

FAILURE:

FAIL CLOSED.

TENANT AUTHORIZATION:

Principal.UserID
+
TenantContext.TenantID
+
Required Permission
→ decision

PLATFORM AUTHORIZATION:

Principal.UserID
+
Platform Permission
→ decision

SUPER_ADMIN:

Must gain authority through Feature 5 permission assignments.

Do not implement hardcoded bypass logic.

BUSINESS_OWNER:

Allowed:

user.read
user.create
user.update
user.disable
tenant.read
tenant.update
role.read
role.assign
permission.read

Not allowed:

role.create
role.update
role.delete
permission.assign

STAFF:

Allowed:

tenant.read
user.read
role.read
permission.read

Mutation permissions denied.

CROSS-TENANT:

Permission in Tenant A must NEVER authorize Tenant B.

RESOURCE ACCESS:

Permission alone is insufficient.

Must also verify:

resource.tenant_id == trusted TenantContext.TenantID

IDOR/BOLA:

Knowing resource ID never grants authority.

REVOCATION:

Membership disabled
→ tenant authority gone immediately

Role removed
→ capability gone immediately

Permission removed
→ capability gone immediately

No access-token refresh required.

TOKENS:

Do NOT use roles/permissions from access tokens as authority.

CACHE:

No permission caching.

ERRORS:

TENANT_ACCESS_DENIED
→ invalid tenant access

PERMISSION_DENIED
→ tenant access valid but capability missing

RESOURCE_NOT_FOUND
→ may be used for non-disclosing cross-tenant resource access according
   to approved route/resource policy

TDD:

RED
→ GREEN
→ REFACTOR
→ security tests

FINAL VALIDATION:

go vet ./...
go test ./...
PostgreSQL integration tests

FEATURE 6 COMPLETION:

Completes Epic 1 Identity & Access.

NEXT:

Epic 2 should begin only after Feature 6 receives an independent
security review and Epic 1 is declared complete.
```

---

# 77. Workflow

```text
Feature 6 Specification
        ↓
Planning prompt
        ↓
Repository inspection
        ↓
Detailed authorization plan
        ↓
DECISIONS REQUIRING APPROVAL
        ↓
Review decisions
        ↓
Approve route-permission matrix
        ↓
Implementation with TDD
        ↓
go vet ./...
go test ./...
Integration tests
        ↓
Independent security review
        ↓
Targeted fixes
        ↓
FEATURE 6 COMPLETE
        ↓
EPIC 1 COMPLETE
```

Do not allow production implementation until the **route-permission matrix and SUPER_ADMIN behavior** are explicitly approved.