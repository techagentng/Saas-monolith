# Multi-Tenant Booking System
# Epic 1 — Feature 4: Tenant Membership & Context

**Document Type:** Feature Implementation Specification / Context Restoration Document  
**Project:** Multi-Tenant Booking System  
**Epic:** Epic 1 — Identity & Access  
**Feature:** Feature 4 — Tenant Membership & Context  
**Backend:** Go  
**Database:** PostgreSQL  
**Architecture:** Modular Monolith  
**Request Flow:** API → Handler → Service → Repository → Database  
**Development Method:** Pure TDD / Test-First Development  

---

# 1. Purpose

This document defines the authoritative implementation requirements for:

**Epic 1, Feature 4 — Tenant Membership & Context**

Feature 4 establishes the relationship between authenticated users and tenants.

It answers:

> Which tenants is this authenticated user allowed to operate within?

It also establishes:

> Which tenant is this request currently operating within?

Feature 4 does **not** yet answer:

> What actions is this user allowed to perform inside the tenant?

That belongs to later features:

```text
F4 Tenant Membership & Context
        ↓
F5 Roles & Permissions
        ↓
F6 Authorization & Access Enforcement
```

Feature 4 must therefore establish tenant access boundaries without prematurely implementing role or permission authorization.

---

# 2. Previous Feature Dependencies

Feature 4 depends on completed:

```text
F1 — Application Error Infrastructure
F2 — User Identity
F3 — Authentication & Session Foundation
```

## Feature 1 provides

- Stable application error codes.
- Typed application errors.
- `%w` wrapping.
- `errors.As`.
- Centralized HTTP error mapping.
- Safe public error responses.

## Feature 2 provides

- User identity.
- User IDs.
- User status.
- User repository.

## Feature 3 provides

- Login.
- Sessions.
- Authentication middleware.
- Authenticated Principal.
- `UserID`.
- `SessionID`.

Feature 4 must reuse these systems.

Do not create another authentication mechanism.

---

# 3. Architecture

The architecture remains:

```text
API → Handler → Service → Repository → Database
```

Tenant membership belongs within the Tenant domain/module.

Conceptually:

```text
Authenticated Request
       ↓
Authentication Middleware
       ↓
Principal
       ↓
Tenant Context Resolution
       ↓
Tenant Membership Verification
       ↓
Handler
       ↓
Service
       ↓
Repository
       ↓
PostgreSQL
```

Important:

```text
Authentication
≠
Tenant Membership
≠
Authorization
```

Authentication proves:

> Who are you?

Tenant membership proves:

> Are you associated with this tenant?

Authorization later proves:

> Are you allowed to perform this action?

These concerns must remain separate.

---

# 4. Feature Goal

Implement the minimum secure tenant-membership foundation required by the multi-tenant system.

Feature 4 must support:

1. Persistent tenant identity where required.
2. Persistent tenant membership.
3. Linking users to tenants.
4. Removing/revoking tenant membership.
5. Looking up a user's tenant memberships.
6. Checking whether a user belongs to a tenant.
7. Resolving tenant context for a request.
8. Preventing arbitrary tenant selection.
9. Preventing cross-tenant access.
10. Establishing a trusted Tenant Context.
11. Reusing the authenticated Principal from Feature 3.
12. Tenant-scoped repository behavior.
13. TDD and tenant-isolation tests.

---

# 5. In Scope

Feature 4 includes:

- Tenant identity required for membership.
- Tenant membership model.
- Tenant membership persistence.
- Tenant membership repository.
- Tenant membership service.
- Membership creation.
- Membership retrieval.
- Membership revocation/removal.
- Membership status.
- Request tenant-context resolution.
- Trusted tenant context.
- Tenant membership middleware/resolver where appropriate.
- Tenant isolation tests.
- Required database migrations.
- PostgreSQL integration tests.
- Feature 1 error reuse.
- Feature 3 Principal reuse.

---

# 6. Explicitly Out of Scope

Do not implement:

- Tenant branding.
- Tenant custom domains.
- Tenant settings.
- Booking logic.
- Staff scheduling.
- Roles.
- Permissions.
- RBAC.
- Permission checks.
- Role assignment.
- Super-admin authorization.
- Resource-level authorization.
- Booking ownership checks.
- Payment behavior.
- Notifications.
- Tenant subscription/billing.
- Frontend tenant switcher UI.
- Tenant invitations unless separately approved.
- Complex organization hierarchies.

Feature 4 is about:

```text
User ↔ Tenant relationship
+
trusted tenant request context
```

not tenant administration as a whole.

---

# 7. Tenant Identity

A tenant represents a business/account boundary.

Conceptually:

```text
Tenant
├── ID
├── Name
├── Status
├── CreatedAt
└── UpdatedAt
```

Only fields required by membership/context should be introduced.

Do not add:

```text
Logo
Brand colors
Domain
Booking settings
Payment settings
Notification settings
```

Those belong to later Tenant/Branding features.

---

# 8. Tenant ID Strategy

Tenant IDs should follow the established project ID strategy.

Since previous user/session IDs use UUIDs, the expected direction is:

```text
TenantID = UUID
```

Do not introduce sequential externally exposed IDs unless explicitly approved.

Malformed tenant IDs must be rejected safely.

---

# 9. Tenant Membership Model

Conceptually:

```text
TenantMembership
├── ID
├── TenantID
├── UserID
├── Status
├── CreatedAt
└── UpdatedAt
```

A membership represents:

> User X belongs to Tenant Y.

The membership does **not** yet define:

```text
role
permissions
authorization rules
```

Those belong to Feature 5.

---

# 10. Membership Status

Use a minimal status model.

Recommended initial statuses:

```text
ACTIVE
DISABLED
```

Default:

```text
ACTIVE
```

An inactive/disabled membership must not grant tenant access.

Do not introduce unnecessary states such as:

```text
INVITED
PENDING
SUSPENDED
EXPIRED
DELETED
```

unless separately approved.

---

# 11. Membership Uniqueness

A user must not have duplicate memberships for the same tenant.

The database must enforce:

```text
UNIQUE (tenant_id, user_id)
```

Do not rely only on service-level checks.

Expected duplicate behavior:

```text
Create Membership
      ↓
Existing Tenant/User Membership
      ↓
Database Unique Constraint
      ↓
Stable Application Error
```

The exact error code must be approved if Feature 1 does not already contain an appropriate code.

Do not expose PostgreSQL duplicate-key errors.

---

# 12. Tenant Membership Relationship

The schema conceptually becomes:

```text
users
  │
  │
  └──── tenant_memberships ──── tenants
```

A user may belong to:

```text
Tenant A
Tenant B
Tenant C
```

unless product rules later restrict membership count.

Feature 4 should support a many-to-many relationship.

---

# 13. Tenant Access Rule

A user is allowed to establish tenant context only when:

```text
Authenticated user ID
        =
Membership user ID

AND

Requested tenant ID
        =
Membership tenant ID

AND

Membership status
        =
ACTIVE

AND

Tenant status permits access
```

Do not trust a tenant ID merely because the client supplied it.

---

# 14. Tenant Context

Feature 4 introduces a trusted tenant context.

Conceptually:

```go
type TenantContext struct {
    TenantID string
}
```

or equivalent using the approved UUID type.

Tenant context should be created only after membership has been validated.

The context represents:

> This authenticated request is operating within Tenant X.

It must not be populated directly from untrusted client input.

---

# 15. Request Tenant Selection

The implementation plan must define how a request identifies its intended tenant.

Possible approaches:

```text
Route:
    /api/v1/tenants/{tenantID}/...

Header:
    X-Tenant-ID

Subdomain:
    tenant.example.com
```

For Feature 4, the simplest explicit API approach is preferred.

Recommended initial direction:

```text
/api/v1/tenants/{tenantID}/...
```

This makes tenant context explicit and testable.

Even if the client supplies the tenant ID in the route, the system must verify membership before establishing trusted context.

---

# 16. Do Not Trust Client Tenant Headers

If a header-based approach is introduced later:

```text
X-Tenant-ID
```

the value is only a **requested tenant**, not trusted tenant context.

Never do:

```text
tenantContext = request.Header["X-Tenant-ID"]
```

without membership verification.

The flow must be:

```text
Client tenant identifier
      ↓
Parse identifier
      ↓
Authenticated Principal
      ↓
Membership lookup
      ↓
Membership verification
      ↓
Trusted TenantContext
```

---

# 17. Tenant Context Middleware

Feature 4 may introduce tenant-context middleware.

Conceptually:

```text
Authentication Middleware
        ↓
Principal
        ↓
Tenant Context Middleware
        ↓
Extract requested TenantID
        ↓
Membership Service
        ↓
Verify ACTIVE membership
        ↓
Trusted TenantContext
        ↓
Handler
```

Tenant-context middleware must not:

- Check roles.
- Check permissions.
- Perform resource authorization.
- Determine booking access.
- Determine administrative privileges.

Its responsibility is only:

> Verify this authenticated identity may operate within this tenant.

---

# 18. Principal and Tenant Context Separation

Do not modify Feature 3 Principal to permanently include TenantID.

Feature 3 Principal remains:

```text
Principal
├── UserID
└── SessionID
```

Feature 4 adds separate request context:

```text
TenantContext
└── TenantID
```

Therefore:

```text
Principal
+
TenantContext
```

represent the authenticated user and active tenant.

This keeps authentication independent from tenant selection.

---

# 19. Tenant Switching

A user belonging to multiple tenants may operate in different tenant contexts.

Conceptually:

```text
User
├── Tenant A
├── Tenant B
└── Tenant C
```

Switching tenants should conceptually mean:

> Make the next request within another authorized TenantContext.

Do not modify authentication tokens every time the user switches tenants.

Do not embed mutable tenant context into access tokens during Feature 4 unless explicitly approved.

The preferred approach is:

```text
Access Token
→ identifies User + Session

Request
→ identifies intended Tenant

Membership validation
→ establishes TenantContext
```

---

# 20. Tenant Context Must Not Be Token Authority

Do not blindly trust tenant IDs inside access tokens.

Feature 3 access tokens should remain concerned with:

```text
UserID
SessionID
```

Tenant membership may change while a session remains active.

If TenantID were trusted solely from an old token:

```text
membership removed
but
token still says Tenant A
```

which creates stale authorization state.

Therefore membership validation remains server-side.

---

# 21. Membership Creation

Feature 4 must support creating a user-to-tenant membership.

Conceptual service flow:

```text
Membership Creation Request
        ↓
Validate User
        ↓
Validate Tenant
        ↓
Ensure relationship does not already exist
        ↓
Create membership
        ↓
Return safe membership representation
```

However:

> Who is allowed to create memberships?

is an authorization question.

Since Feature 5/6 are not yet implemented, production access policy for membership-management endpoints must be treated carefully.

Handlers may be implemented and tested independently without prematurely declaring them publicly accessible.

---

# 22. Membership Retrieval

The service should support:

```text
Find membership by TenantID + UserID
List memberships for UserID
```

Internal membership lookup is essential for context resolution.

Public listing endpoints must not be introduced without clear access rules.

---

# 23. Membership Revocation

Feature 4 must support removing or disabling a membership.

Preferred approach:

```text
status = DISABLED
```

rather than destructive deletion where historical relationship may matter.

However, whether memberships are:

```text
hard deleted
or
status-disabled
```

must be explicitly approved.

Once membership is no longer active:

```text
future tenant-context resolution
→ TENANT_ACCESS_DENIED
```

or the approved equivalent.

---

# 24. Existing Sessions After Membership Revocation

Membership revocation must take effect without requiring logout.

Example:

```text
User logged in
       ↓
Membership to Tenant A removed
       ↓
Existing Feature 3 session remains valid
       ↓
Next request to Tenant A
       ↓
Membership check fails
       ↓
Tenant context denied
```

Do not revoke the entire authentication session merely because one tenant membership was removed.

Authentication and tenant membership remain independent.

---

# 25. Tenant Isolation

Tenant isolation is the core security requirement of Feature 4.

Every tenant-scoped operation must eventually include:

```text
Authenticated User
      ↓
Trusted TenantContext
      ↓
Tenant-scoped query
```

Repositories for future tenant-owned resources must not rely only on:

```sql
WHERE id = $1
```

They should use:

```sql
WHERE id = $1
AND tenant_id = $2
```

where appropriate.

Feature 4 establishes this convention for later modules.

---

# 26. Cross-Tenant Attack Example

Given:

```text
User A belongs to Tenant A

User B belongs to Tenant B
```

User A must not gain access by changing:

```text
/api/v1/tenants/TENANT_A/...
```

to:

```text
/api/v1/tenants/TENANT_B/...
```

The system must:

```text
Authenticate User A
      ↓
Requested Tenant B
      ↓
Membership check
      ↓
No ACTIVE membership
      ↓
DENY
```

---

# 27. Error Contract

Feature 4 must reuse Feature 1.

Relevant existing error codes include:

```text
INVALID_REQUEST
VALIDATION_FAILED
TENANT_NOT_FOUND
TENANT_ACCESS_DENIED
USER_NOT_FOUND
SERVICE_UNAVAILABLE
INTERNAL_ERROR
```

Potentially required membership-specific errors must be approved before being added.

Possible candidates:

```text
TENANT_MEMBERSHIP_NOT_FOUND
TENANT_MEMBERSHIP_ALREADY_EXISTS
```

Only add them if they provide real semantic value.

Do not add speculative error codes.

---

# 28. Information Disclosure

Tenant membership errors must not leak unnecessary tenant information.

For example:

```text
requested tenant does not exist
```

versus:

```text
tenant exists but user is not a member
```

may reveal information.

For tenant-context resolution, consider collapsing inaccessible tenant conditions into:

```text
TENANT_ACCESS_DENIED
```

or an equivalent safe behavior.

The implementation plan must explicitly decide what public distinction is allowed.

Internal diagnostics may retain more specific causes.

---

# 29. Tenant Database Requirements

Create only the schema needed for Feature 4.

Likely tables:

```text
tenants
tenant_memberships
```

Conceptual tenants table:

```text
id
name
status
created_at
updated_at
```

Conceptual membership table:

```text
id
tenant_id
user_id
status
created_at
updated_at
```

Required constraints:

```text
PRIMARY KEY
FOREIGN KEY → tenants
FOREIGN KEY → users
UNIQUE (tenant_id, user_id)
NOT NULL
```

Do not create:

```text
roles
permissions
bookings
branding
tenant settings
payment configuration
```

---

# 30. Tenant Status

Use a minimal tenant status model.

Recommended:

```text
ACTIVE
DISABLED
```

Default:

```text
ACTIVE
```

Disabled tenants should not allow tenant context to be established.

Do not introduce billing/subscription status into tenant identity.

---

# 31. Repository Responsibilities

Likely repository abstractions:

```text
TenantRepository
TenantMembershipRepository
```

Tenant repository may support:

```text
Create
FindByID
```

Membership repository may support:

```text
Create
FindByTenantAndUser
ListByUser
Disable / Revoke
```

Exact signatures must be driven by tests.

Repositories must:

- Use parameterized SQL.
- Enforce database semantics.
- Translate expected database errors.
- Preserve wrapped causes.
- Avoid HTTP logic.
- Avoid authorization logic.

---

# 32. Service Responsibilities

Likely services:

```text
TenantService
TenantMembershipService
```

or cohesive equivalents.

Membership service owns:

- Membership creation workflow.
- Membership lookup.
- Membership-status checks.
- Tenant-status checks.
- Context eligibility.
- Membership revocation.

Service must not:

- Execute SQL.
- Parse HTTP.
- Check roles.
- Check permissions.
- Authorize bookings/resources.

---

# 33. Tenant Context Resolver Responsibility

Tenant context resolution must conceptually answer:

```text
Can User X establish context for Tenant Y?
```

The resolver/service should:

1. Receive authenticated UserID.
2. Receive requested TenantID.
3. Validate tenant identifier.
4. Load tenant/membership state.
5. Verify tenant active.
6. Verify membership active.
7. Return trusted TenantContext.

It should not return roles or permissions yet.

---

# 34. TDD Requirements

Feature 4 must use pure TDD.

Recommended order:

1. Inspect completed F1–F3 implementations.
2. Resolve Feature 4 decisions.
3. Produce implementation plan.
4. Write failing tenant-model tests.
5. Write failing membership-model tests.
6. Write failing membership service tests.
7. Write failing tenant-context resolution tests.
8. Write failing repository tests.
9. Write failing migration/PostgreSQL tests.
10. Write failing middleware/context tests.
11. Write failing handler tests.
12. Write failing tenant-isolation/security tests.
13. Implement minimum code.
14. Run focused tests.
15. Run PostgreSQL integration tests.
16. Run full suite.
17. Independent review.

---

# 35. Required Membership Service Tests

Test:

- Membership creation succeeds.
- Duplicate membership fails.
- Missing tenant fails safely.
- Missing user fails safely.
- Membership defaults to approved status.
- Membership lookup succeeds.
- Disabled membership does not establish tenant access.
- Disabled tenant does not establish tenant access.
- Active membership + active tenant establishes context.
- Repository errors remain wrapped.
- No role or permission checks occur.

---

# 36. Required Tenant Context Tests

Test:

```text
Authenticated User A + Tenant A membership
→ context succeeds
```

Test:

```text
Authenticated User A + Tenant B without membership
→ access denied
```

Test:

```text
User has DISABLED Tenant B membership
→ access denied
```

Test:

```text
Tenant disabled
→ access denied
```

Test:

```text
Malformed TenantID
→ INVALID_REQUEST
```

Test:

```text
Client changes TenantID
→ membership re-evaluated
```

Tenant context must never be established solely because the client supplied a valid UUID.

---

# 37. Required Repository Tests

Test:

- Tenant insert.
- Tenant find by ID.
- Membership insert.
- Membership find by tenant/user.
- Membership list by user.
- Duplicate membership database constraint.
- Foreign-key behavior.
- Membership status.
- Tenant status.
- Parameterized SQL.
- Safe missing-row handling.
- Raw database errors are not public.
- No role/permission columns are introduced.

---

# 38. Required Middleware Tests

If tenant-context middleware is implemented, test:

- Valid Principal required.
- Valid TenantID required.
- Active membership succeeds.
- Missing membership fails.
- Disabled membership fails.
- Disabled tenant fails.
- Principal UserID cannot be spoofed.
- TenantContext cannot be injected using arbitrary client headers.
- Middleware establishes only TenantID.
- Middleware does not add roles.
- Middleware does not add permissions.
- Middleware does not perform resource authorization.

---

# 39. Required Security Tests

Explicitly verify:

- User from Tenant A cannot establish Tenant B context.
- Valid tenant UUID alone does not grant access.
- Disabled membership cannot be used.
- Membership removal takes effect during existing authenticated sessions.
- Client cannot spoof UserID.
- Client cannot spoof trusted TenantContext.
- Tenant IDs are not trusted solely from access-token claims.
- Membership uniqueness is enforced by database.
- SQL remains parameterized.
- Database errors remain sanitized.
- No tenant secrets/settings are exposed.
- No role/permission behavior is accidentally introduced.

---

# 40. Tenant Isolation Tests

These tests are mandatory.

Example:

```text
Given:
User A belongs to Tenant A.
User B belongs to Tenant B.

When:
User A requests context for Tenant B.

Then:
Tenant access is denied.
```

Also test:

```text
User A cannot enumerate Tenant B through context APIs.

User A cannot establish Tenant B context by supplying Tenant B UUID.

User A cannot use an expired/disabled membership.

Removing User A from Tenant A immediately prevents future Tenant A context resolution.

User A's Feature 3 authentication session remains valid after Tenant A membership removal.
```

---

# 41. API Endpoint Scope

Potential minimum endpoints:

```text
POST /api/v1/tenants
POST /api/v1/tenants/{tenantID}/members
GET  /api/v1/users/{userID}/tenants
DELETE or PATCH /api/v1/tenants/{tenantID}/members/{userID}
```

However:

> Who may create tenants or memberships?

is an authorization question.

Since F5/F6 are not implemented yet, these handlers may be developed/tested without fully exposing them as unauthenticated production routes.

The implementation plan must explicitly distinguish:

```text
handler capability
```

from:

```text
approved production access policy
```

Do not silently create unsecured administrative APIs.

---

# 42. Context-Scoped Future APIs

Future modules should eventually follow patterns such as:

```text
GET /api/v1/tenants/{tenantID}/bookings
POST /api/v1/tenants/{tenantID}/bookings
```

Before these reach handlers:

```text
Authentication
      ↓
Tenant Context
      ↓
Future Authorization
```

Feature 4 prepares this boundary.

It must not implement Booking yet.

---

# 43. Transactions

Membership creation generally requires one insert.

A broad transaction is unnecessary unless a concrete workflow includes multiple atomic writes.

Do not create a generic transaction abstraction prematurely.

Database uniqueness remains the final concurrency authority.

---

# 44. Concurrency

Concurrent duplicate membership creation may occur.

The database uniqueness constraint:

```text
UNIQUE (tenant_id, user_id)
```

must prevent duplicates.

Service-level existence checks may improve error clarity but must not be the only protection.

Repository code must safely translate concurrent uniqueness violations.

---

# 45. Observability

Tenant-access failures should be compatible with future structured logs/metrics.

Useful future diagnostic dimensions may include:

```text
request_id
user_id
tenant_id
error_code
```

Do not add high-cardinality identifiers as metric labels.

Do not introduce a logging/metrics system solely for Feature 4.

---

# 46. Scope Control

Feature 4 MUST NOT implement:

```text
Roles
Permissions
RBAC
Authorization Middleware
Booking Authorization
Tenant Branding
Tenant Settings
Tenant Billing
Custom Domains
Notifications
Payments
Staff Scheduling
Frontend Tenant Switcher
```

Do not introduce permission concepts into the membership model.

---

# 47. Acceptance Criteria

Feature 4 is complete when:

- [ ] Tenant identity required by membership exists.
- [ ] Tenant IDs follow the approved project strategy.
- [ ] Tenant membership model exists.
- [ ] User-to-tenant many-to-many membership is supported.
- [ ] Membership uniqueness is database-enforced.
- [ ] Membership status exists.
- [ ] Tenant status exists.
- [ ] Active membership can be resolved.
- [ ] Disabled membership cannot establish context.
- [ ] Disabled tenant cannot establish context.
- [ ] User memberships can be retrieved internally.
- [ ] Membership can be revoked/disabled according to approved semantics.
- [ ] Membership revocation takes effect without terminating Feature 3 session.
- [ ] Tenant context can be resolved from Principal + requested tenant.
- [ ] Tenant context cannot be established from tenant ID alone.
- [ ] Client cannot spoof authenticated UserID.
- [ ] Client cannot spoof trusted TenantContext.
- [ ] Cross-tenant access attempts are denied.
- [ ] Feature 1 errors are reused.
- [ ] Expected errors survive `%w` wrapping.
- [ ] SQL is parameterized.
- [ ] Raw database errors are not exposed.
- [ ] Required migrations exist.
- [ ] PostgreSQL uniqueness/foreign-key tests pass.
- [ ] Service tests pass.
- [ ] Repository tests pass.
- [ ] Tenant-context tests pass.
- [ ] Security/isolation tests pass.
- [ ] Full repository tests pass.
- [ ] No role implementation exists.
- [ ] No permission implementation exists.
- [ ] No authorization enforcement exists.

---

# 48. Decisions Requiring Explicit Approval

The implementation plan must identify/resolve:

1. Tenant ID strategy.
2. Exact minimal tenant fields.
3. Tenant status values.
4. Default tenant status.
5. Membership ID strategy.
6. Membership status values.
7. Default membership status.
8. Hard-delete vs disable membership.
9. Whether disabled membership can later be reactivated.
10. Duplicate-membership error behavior.
11. Whether new membership-specific error codes are needed.
12. Public distinction between nonexistent tenant and inaccessible tenant.
13. Tenant-context identification strategy.
14. Route parameter vs header.
15. Whether tenant ID appears in access-token claims.
16. Whether server-side membership is checked on every tenant-scoped request.
17. Whether membership state is cached.
18. Whether multiple tenant memberships per user are allowed.
19. Whether tenant creation itself is part of F4.
20. Who may create tenants.
21. Who may add memberships.
22. Who may remove memberships.
23. Whether administrative membership endpoints are registered before F6.
24. Exact minimum endpoint set.
25. Membership revocation HTTP semantics.
26. User membership list API exposure.
27. Tenant list exposure.
28. Migration naming.
29. Repository contracts.
30. Confirmation roles/permissions remain excluded.
31. Confirmation authentication tokens remain tenant-neutral.
32. Confirmation tenant branding/settings remain excluded.

Do not begin production implementation until these decisions have been reviewed.

---

# 49. Recommended Planning Direction

Use this as the preferred baseline:

```text
Tenant ID:
UUID

Tenant:
ID
Name
Status
CreatedAt
UpdatedAt

Tenant Status:
ACTIVE
DISABLED

Membership ID:
UUID

Membership:
ID
TenantID
UserID
Status
CreatedAt
UpdatedAt

Membership Status:
ACTIVE
DISABLED

Relationship:
Many-to-many User ↔ Tenant

Duplicate prevention:
UNIQUE (tenant_id, user_id)

Membership revocation:
Set status = DISABLED

Authentication token:
Remain tenant-neutral.

Access token claims:
UserID + SessionID only.

Tenant context:
Resolved server-side per tenant-scoped request.

Tenant identifier:
Route parameter preferred.

Context establishment:
Principal UserID
+
Requested TenantID
+
ACTIVE membership
+
ACTIVE tenant

Cross-tenant:
Default deny.

Roles:
Deferred to F5.

Authorization:
Deferred to F6.
```

---

# 50. Copilot Planning Prompt

```text
You are preparing the implementation plan for Epic 1, Feature 4 —
Tenant Membership & Context in the Multi-Tenant Booking System.

Read and inspect:

1. Master Specification.
2. Epic 1 — Identity & Access specification.
3. Feature 1 — Application Error Infrastructure.
4. Feature 2 — User Identity.
5. Feature 3 — Authentication & Session Foundation.
6. Feature 4 — Tenant Membership & Context specification.
7. The actual completed F1–F3 implementation.

Do NOT implement production code yet.

FEATURE PURPOSE

Feature 4 establishes:

- Tenant identity required for membership.
- User-to-tenant membership.
- Membership persistence.
- Membership status.
- Tenant status.
- Tenant context resolution.
- Tenant isolation.
- Trusted TenantContext for authenticated requests.

Preserve:

API → Handler → Service → Repository → Database

Authentication proves identity.

Tenant membership proves whether the authenticated user may operate within
the requested tenant.

Authorization is NOT part of Feature 4.

INSPECT THE EXISTING CODEBASE FIRST

Determine:

- Existing UUID strategy.
- Existing Principal implementation.
- Existing authentication middleware/context.
- Existing database driver.
- Existing migration style.
- Existing repository style.
- Feature 1 error infrastructure.
- Existing HTTP handler conventions.

Do not duplicate existing infrastructure.

YOUR PLAN MUST ADDRESS

1. Files to create or modify.
2. Tenant domain model.
3. Tenant status.
4. Tenant database schema.
5. Tenant repository responsibilities.
6. Membership domain model.
7. Membership status.
8. Membership database schema.
9. Membership uniqueness.
10. Membership repository interface.
11. PostgreSQL repository implementation.
12. Membership service responsibilities.
13. Tenant-context resolver design.
14. Tenant-context middleware design if used.
15. Principal integration.
16. How requested TenantID is supplied.
17. Why TenantID is or is not included in access tokens.
18. Whether membership is checked on every tenant-scoped request.
19. Tenant status checking.
20. Membership revocation.
21. Existing session behavior after membership revocation.
22. Duplicate membership handling.
23. Information-disclosure behavior.
24. Existing Feature 1 errors to reuse.
25. Whether new membership error codes are needed.
26. Exact database constraints.
27. Concurrency behavior.
28. Transaction requirements.
29. Exact endpoint capabilities.
30. Which endpoints should remain unregistered/policy-deferred until F6.
31. Detailed TDD sequence.
32. Model tests.
33. Service tests.
34. Repository tests.
35. PostgreSQL integration tests.
36. Tenant-context tests.
37. Middleware tests.
38. Handler/API tests.
39. Cross-tenant security tests.
40. Scope boundaries and risks.

PREFERRED BASELINE

Unless existing project conventions justify otherwise:

- Tenant IDs use UUID.
- Membership IDs use UUID.
- Users may belong to multiple tenants.
- Tenant statuses: ACTIVE / DISABLED.
- Membership statuses: ACTIVE / DISABLED.
- Defaults: ACTIVE.
- Membership revocation disables the membership.
- Database enforces UNIQUE (tenant_id, user_id).
- Access tokens remain tenant-neutral.
- Feature 3 Principal remains UserID + SessionID.
- TenantContext is separate from Principal.
- Tenant identifier comes from route context.
- Membership is revalidated server-side for tenant-scoped requests.
- Disabled membership denies tenant context.
- Disabled tenant denies tenant context.
- Removing tenant membership does not terminate the user's authentication session.
- Cross-tenant access is default-deny.
- Roles/permissions remain deferred.
- Authorization remains deferred.

CRITICAL SECURITY RULES

- Never trust tenant ID merely because the client supplied it.
- Never trust arbitrary X-User-ID headers.
- Never trust arbitrary TenantContext headers.
- Never establish tenant context without authenticated Principal.
- Never establish tenant context without active membership.
- Never allow disabled tenants/memberships.
- Never use access-token tenant claims as the sole tenant authority.
- Never expose raw PostgreSQL errors.
- Use parameterized SQL.
- Reuse Feature 1 errors.
- Use %w wrapping.
- Do not classify application behavior through err.Error().

DO NOT IMPLEMENT

- Roles.
- Permissions.
- RBAC.
- Authorization middleware.
- Booking authorization.
- Branding.
- Tenant settings.
- Billing.
- Custom domains.
- Notifications.
- Frontend tenant switching.

Do not write production code in this step.

End your response with:

DECISIONS REQUIRING APPROVAL

List every unresolved business, security, schema, API, and context decision
that must be approved before Copilot begins implementation.
```

---

# 51. Implementation Prompt After Plan Approval

```text
The Epic 1, Feature 4 — Tenant Membership & Context implementation plan and
decision addendum have been approved.

Implement Feature 4 only.

Follow pure TDD:

Define behavior
→ Write failing test
→ Confirm failure
→ Implement minimum code
→ Pass test
→ Refactor only where justified
→ Add security/edge cases

Reuse existing Feature 1–3 infrastructure.

Preserve:

API → Handler → Service → Repository → Database

Authentication Principal remains the trusted identity source.

TenantContext must only be established after:

Authenticated Principal
+
Requested TenantID
+
ACTIVE Membership
+
ACTIVE Tenant

MANDATORY SECURITY REQUIREMENTS

- Do not trust tenant IDs solely because the client supplies them.
- Do not trust client identity headers.
- Do not allow cross-tenant context.
- Do not establish context for disabled membership.
- Do not establish context for disabled tenant.
- Membership revocation must affect future context checks.
- Membership revocation must not terminate unrelated authentication sessions.
- Access tokens remain tenant-neutral unless explicitly approved otherwise.
- SQL must be parameterized.
- Database uniqueness must enforce membership uniqueness.
- Feature 1 errors must be reused.
- Use %w and errors.As-compatible error wrapping.
- Never expose raw database errors.

DO NOT IMPLEMENT

- Roles.
- Permissions.
- RBAC.
- Authorization.
- Booking access policies.
- Tenant branding.
- Tenant settings.
- Billing.
- Custom domains.
- Frontend tenant switching.

After implementation:

1. Run focused Feature 4 tests.
2. Run PostgreSQL integration tests.
3. Run tenant-isolation tests.
4. Run:

   go vet ./...
   go test ./...

5. Report:
   - Files created.
   - Files modified.
   - Migrations.
   - Tests.
   - Integration results.
   - Full test result.
   - Any deviations.
   - Any intentionally deferred security/access-policy concerns.

Stop after Feature 4.
Do not begin Feature 5.
```

---

# 52. Claude Haiku Review Prompt

```text
Review only. Do not modify files.

Review Epic 1, Feature 4 — Tenant Membership & Context.

Authoritative requirements:

1. Master Specification.
2. Epic 1 specification.
3. Feature 1 specification.
4. Feature 2 specification.
5. Feature 3 specification.
6. Feature 4 specification.
7. Approved Feature 4 plan.
8. Approved Feature 4 decision addendum.

Inspect the actual implementation.

ARCHITECTURE

Verify:

- Handler → Service → Repository separation.
- Authentication Principal is reused.
- TenantContext is separate from authentication Principal.
- No SQL exists in service/handler.
- No HTTP behavior exists in repositories.
- Roles/permissions are not implemented.

TENANT MEMBERSHIP

Verify:

- User↔Tenant membership is represented correctly.
- Many-to-many behavior is supported.
- Duplicate membership is database-enforced.
- Membership status works.
- Tenant status works.
- Disabled memberships cannot establish context.
- Disabled tenants cannot establish context.
- Membership revocation semantics match the approved plan.

TENANT CONTEXT

Verify:

- Authenticated Principal is required.
- Requested TenantID is validated.
- Membership is verified server-side.
- TenantContext is only created after successful verification.
- Client input cannot directly create trusted TenantContext.
- Tenant ID from access token is not used as sole authority.
- Membership revocation takes effect for existing sessions.

ISOLATION

Explicitly attempt to find cross-tenant vulnerabilities.

Verify:

- User A from Tenant A cannot establish Tenant B context.
- Valid knowledge of Tenant B UUID does not grant access.
- Client cannot spoof UserID.
- Client cannot spoof membership.
- Client cannot spoof TenantContext.
- Disabled relationships are denied.
- Information-disclosure behavior matches the approved policy.

DATABASE

Verify:

- Correct foreign keys.
- UNIQUE (tenant_id, user_id).
- Parameterized SQL.
- Correct indexes.
- Correct status persistence.
- Database errors are safely wrapped.
- Raw driver errors are not exposed.

ERROR HANDLING

Verify:

- Feature 1 infrastructure is reused.
- TENANT_ACCESS_DENIED is used appropriately.
- TENANT_NOT_FOUND behavior matches approved disclosure policy.
- Any new membership codes were approved.
- %w preserves expected errors.
- No err.Error() parsing.

TESTING

Verify:

- Model tests.
- Service tests.
- Repository tests.
- PostgreSQL integration tests.
- Middleware/context tests.
- Cross-tenant isolation tests.
- Disabled membership tests.
- Disabled tenant tests.
- Membership revocation tests.
- Concurrency duplicate tests.

SCOPE

Confirm no implementation of:

- Roles.
- Permissions.
- RBAC.
- Authorization.
- Booking authorization.
- Branding.
- Billing.
- Tenant settings.
- Custom domains.
- Frontend tenant switcher.

Classify findings as:

CRITICAL
IMPORTANT
MINOR

Distinguish actual specification/security violations from stylistic preferences.

If required tests are missing, state that the feature is not
specification-complete.

Conclude:

APPROVED

or

APPROVED AFTER IMPORTANT FIXES

or

NOT APPROVED

Do not modify files.
```

---

# 53. Context Restoration Summary

```text
PROJECT:
Multi-Tenant Booking System

EPIC:
Epic 1 — Identity & Access

FEATURE:
Feature 4 — Tenant Membership & Context

COMPLETED:
F1 Error Infrastructure
F2 User Identity
F3 Authentication & Sessions

ARCHITECTURE:
API → Handler → Service → Repository → Database

FEATURE 4 PURPOSE:
Connect authenticated users to tenants and establish trusted tenant context.

CORE RELATIONSHIP:

User
  ↕
TenantMembership
  ↕
Tenant

MEMBERSHIP:
Many-to-many.

DEFAULT STATUS:
ACTIVE

DISABLED:
Cannot establish tenant context.

TENANT CONTEXT:

Authenticated Principal
+
Requested TenantID
+
Active Tenant
+
Active Membership
=
Trusted TenantContext

AUTHENTICATION PRINCIPAL:
UserID + SessionID

TENANT CONTEXT:
TenantID

KEEP THEM SEPARATE.

ACCESS TOKEN:
Remain tenant-neutral.

DO NOT TRUST:
Client TenantID by itself.
X-User-ID.
X-Tenant-ID as trusted context.
Tenant claims without membership validation.

DATABASE:
UNIQUE (tenant_id, user_id)

CROSS-TENANT:
Default deny.

MEMBERSHIP REMOVAL:
Immediately blocks future tenant context.
Does not invalidate user's authentication session.

ERRORS:
Reuse Feature 1.

Likely:
INVALID_REQUEST
VALIDATION_FAILED
TENANT_NOT_FOUND
TENANT_ACCESS_DENIED
USER_NOT_FOUND
SERVICE_UNAVAILABLE
INTERNAL_ERROR

ROLES:
Deferred to F5.

AUTHORIZATION:
Deferred to F6.

DO NOT IMPLEMENT:
Roles
Permissions
RBAC
Booking authorization
Branding
Billing
Tenant settings
Frontend tenant switching

TESTING:
TDD.
PostgreSQL integration.
Cross-tenant isolation tests mandatory.

STOP:
Do not begin F5 until F4 has been independently reviewed.
```