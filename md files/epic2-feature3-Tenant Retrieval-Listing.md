# EPIC 02 — Tenant Management

## Feature 3 — Tenant Retrieval & Listing

### Agent-Ready Context & Delivery Specification

---

# 1. Purpose

This document is the authoritative context-preservation specification for:

> **Epic 02 — Feature 3: Tenant Retrieval & Listing**

Feature 3 introduces secure tenant retrieval capabilities on top of the tenant persistence, membership, authentication, authorization, and owner-provisioning infrastructure already completed.

This document should allow a fresh planning or implementation agent to understand Feature 3 without reconstructing Epic 02 from conversation history.

The actual repository remains authoritative for implementation details.

Agents MUST inspect the repository before modifying code.

---

# 2. Completed Foundation

The following work is considered complete.

## Epic 01 — Identity & Access

Epic 01 provides the existing infrastructure for:

```text id="r6zq0d"
Users
Authentication
Roles
Permissions
Tenant Memberships
Membership Status
Role Assignment
Authorization
Tenant Context
Structured Errors
Audit foundations
```

Existing role concepts include:

```text id="b4n8zw"
SUPER_ADMIN
BUSINESS_OWNER
STAFF
```

Do NOT redesign these concepts during Feature 3.

---

# 3. Epic 02 Feature 1 — COMPLETE

Feature 1 established Tenant Core Model & Persistence.

The persisted tenant concept is approximately:

```text id="zwrbz1"
Tenant
├── ID
├── Name
├── Slug
├── Status
├── CreatedAt
└── UpdatedAt
```

Existing tenant statuses currently use the repository's established semantics:

```text id="7sq2s3"
ACTIVE
DISABLED
```

Do not redesign tenant lifecycle in Feature 3.

Feature 8 owns broader lifecycle management.

Tenant persistence supports approximately:

```text id="e8x6mz"
Create
FindByID
```

A forward migration added slug persistence and database uniqueness.

Existing migration history must remain immutable.

---

# 4. Epic 02 Feature 2 — COMPLETE

Feature 2 implemented:

> Tenant Creation & Owner Provisioning

The successful onboarding flow is:

```text id="3fpz7f"
Authenticated User
        ↓
POST /api/v1/tenants
        ↓
TenantService
        ↓
BEGIN TRANSACTION
        ↓
Create Tenant
        ↓
Create ACTIVE Membership
        ↓
Assign existing BUSINESS_OWNER
        ↓
COMMIT
```

Important established policy:

> Any authenticated platform user may create a tenant.

Tenant creation requires:

```text id="pufhqd"
Authentication              YES
Existing Tenant Context     NO
Tenant Permission           NO
```

The authenticated principal becomes the initial owner.

The request cannot choose another owner.

---

# 5. Feature 2 Transaction Guarantee

Tenant creation, membership creation, and BUSINESS_OWNER assignment are atomic.

Failure during provisioning results in:

```text id="6hcsdf"
ROLLBACK

Tenant       ❌
Membership   ❌
Role         ❌
```

Feature 3 MUST NOT modify this transaction behaviour.

---

# 6. Existing Slug Boundary

Feature 1 persists slug.

Feature 2 accepts caller-supplied slug during creation.

The structured error:

```text id="s9okem"
TENANT_SLUG_TAKEN
```

exists for duplicate slug and maps to:

```text id="xgwk1q"
HTTP 409
```

Feature 3 does NOT expand slug behaviour.

The following remain Feature 5 responsibilities:

```text id="6n1swe"
slug generation
slug normalization
reserved slugs
FindBySlug
public tenant resolution
public URL routing
slug updates
```

---

# 7. Feature 3 Goal

Feature 3 implements:

> **Secure retrieval and listing of tenants visible to the authenticated user or appropriately authorized platform actor.**

Feature 3 answers questions such as:

```text id="92nh4q"
Which tenants does this user belong to?

What tenant information may this user retrieve?

Can this authenticated user retrieve this specific tenant?

Can a platform administrator list tenants globally?
```

The feature must distinguish between:

```text id="utqjvf"
Tenant retrieval
```

and:

```text id="bb07xx"
Tenant authorization
```

Possessing a tenant ID must never itself grant access.

---

# 8. Core Security Principle

The most important Feature 3 invariant is:

> A user must not gain access to Tenant B simply because they know Tenant B's UUID.

Conceptually:

```text id="unrk7z"
User belongs to Tenant A

GET Tenant A
→ ALLOWED

GET Tenant B
→ DENIED
```

unless the authenticated actor has an explicitly supported platform-level authorization allowing that access.

---

# 9. Feature Scope

Feature 3 should provide the minimum retrieval operations needed by the product.

At minimum, planning must evaluate:

```text id="a8jsdo"
Get Tenant By ID

List My Tenants
```

and determine whether Feature 3 should also include:

```text id="7v09bb"
Platform Tenant Listing
```

for SUPER_ADMIN.

Do not automatically implement all three merely because they are possible.

Inspect existing product/authorization conventions first.

---

# 10. Get Tenant By ID

Feature 3 should support retrieving a tenant that the authenticated actor is authorized to access.

Conceptually:

```text id="8iz01q"
Authenticated Request
        ↓
Tenant ID
        ↓
Resolve Tenant
        ↓
Verify Access
        ↓
Return Tenant
```

Do NOT implement:

```text id="4kv0gp"
FindByID(id)
→ return tenant to anyone
```

at the HTTP/service boundary.

Repository retrieval and authorization are separate concerns.

---

# 11. List My Tenants

A normal authenticated user should be able to retrieve tenants in which they hold an appropriate membership.

Conceptually:

```text id="csovgx"
Authenticated User
        ↓
Memberships
        ↓
Tenant IDs
        ↓
Tenant information
```

The implementation should use existing Epic 01 membership semantics.

Do NOT create a second concept such as:

```text id="k0x9l6"
tenant_users
owned_tenants
user_tenants_v2
```

if `tenant_memberships` already defines the relationship.

---

# 12. Membership Status

Membership status matters.

Inspect the existing membership model before deciding exact behaviour.

Expected security direction:

```text id="tvmmzh"
ACTIVE membership
→ tenant visible

Suspended/revoked/disabled membership
→ tenant not normally visible
```

Use the actual membership statuses established by Epic 01.

Do not invent new membership states.

The plan must explicitly determine whether inactive memberships:

* disappear from `List My Tenants`, or
* appear with restricted metadata.

Default security preference is to exclude memberships that no longer grant tenant access unless existing product semantics require otherwise.

---

# 13. BUSINESS_OWNER and STAFF

Both BUSINESS_OWNER and STAFF may need access to tenant information if their membership/authorization permits it.

Do not hard-code:

```text id="51upn7"
if role == BUSINESS_OWNER
```

as the general tenant-access rule unless existing authorization architecture explicitly requires it.

Prefer existing membership and permission mechanisms.

Feature 3 should not create role-specific shortcuts that bypass Epic 01 authorization.

---

# 14. SUPER_ADMIN

Feature 3 planning must inspect existing SUPER_ADMIN semantics.

Do not assume:

```text id="zw2v5x"
SUPER_ADMIN = unrestricted bypass everywhere
```

unless Epic 01 explicitly establishes that behaviour.

If SUPER_ADMIN has platform-level tenant visibility, reuse the existing authorization mechanism.

Do not add hidden bypass logic such as:

```go id="9l61nu"
if role == "SUPER_ADMIN" {
    return everything
}
```

inside repositories.

Repositories should not know about HTTP/platform role policy unless that is already an established architecture.

---

# 15. Authorization vs Membership

Feature 3 must clearly distinguish:

```text id="8lnwb5"
Membership
```

from:

```text id="oifvbm"
Permission
```

Membership answers:

> Does this user belong to this tenant?

Authorization answers:

> May this user perform this operation in this tenant?

Inspect Epic 01 to determine which combination is required for tenant retrieval.

Do not duplicate checks already provided by tenant-context/authorization infrastructure.

---

# 16. Tenant Context

Epic 01 already contains tenant-context infrastructure.

Inspect it before designing retrieval routes.

Potentially, an endpoint such as:

```text id="iwlywb"
GET /api/v1/tenants/{tenantID}
```

may naturally use:

```text id="jmq7bf"
Authentication
↓
Tenant Context
↓
Authorization
↓
Handler
```

But do not assume this automatically.

Determine how existing tenant middleware behaves and whether using it for the retrieval endpoint would create circular or redundant lookup behaviour.

Document that decision explicitly.

---

# 17. Repository Requirements

Feature 1 already provides approximately:

```text id="w36fbx"
TenantRepository.Create
TenantRepository.FindByID
```

Feature 3 should add only retrieval methods genuinely required.

Potential candidates include:

```text id="gijv3b"
FindByUserID
ListByUserID
List
```

Do NOT choose names before inspecting repository conventions.

Avoid speculative methods such as:

```text id="68xjfj"
SearchAdvanced
FindAllWithPermissions
GetDashboardTenants
FindTenantAndEverything
```

Repository interfaces should remain narrow.

---

# 18. Avoid N+1 Queries

`List My Tenants` should not automatically become:

```text id="7ld1xf"
Find memberships
        ↓
for every membership:
    FindByID(tenantID)
```

if a simple joined query can retrieve the required tenant information safely.

Planning must inspect current repository conventions and expected scale.

Prefer a clear tenant/membership query that avoids obvious N+1 behaviour without introducing premature query frameworks.

---

# 19. Pagination

Planning must explicitly determine whether tenant listing requires pagination now.

For:

```text id="ygx87a"
List My Tenants
```

most users may belong to relatively few tenants.

However:

```text id="j1y7bi"
SUPER_ADMIN global tenant listing
```

could eventually contain thousands.

Do not blindly implement pagination everywhere.

If global listing is included, pagination should strongly be considered.

Follow existing pagination conventions if they exist.

Do not invent a second pagination contract.

---

# 20. Tenant Data Returned

Feature 3 returns tenant identity/profile information appropriate to this stage.

Likely fields include:

```text id="z6zglb"
id
name
slug
status
created_at
updated_at
```

Inspect existing API response conventions.

Do not expose:

* membership database internals unnecessarily
* role database internals
* permission tables
* PostgreSQL fields
* internal audit information
* future settings
* future branding
* billing information

Use an explicit public response DTO if that is the project's existing convention.

---

# 21. Disabled Tenants

Existing tenant lifecycle currently includes:

```text id="i3wrzj"
ACTIVE
DISABLED
```

Feature 3 must decide retrieval behaviour for `DISABLED` tenants based on existing product/security semantics.

Do NOT redesign lifecycle states.

Possible behaviour could distinguish:

```text id="e9ww1k"
normal tenant user
vs
platform administrator
```

but this must follow existing authorization policy.

Feature 8 owns suspension/reactivation behaviour.

---

# 22. Error Contract

Reuse the existing structured error system.

Potential relevant errors include:

```text id="fn82kh"
TENANT_NOT_FOUND
TENANT_ACCESS_DENIED
TENANT_MEMBERSHIP_REQUIRED
INVALID_REQUEST
INVALID_CREDENTIALS
```

Do NOT automatically add all of them.

Inspect existing error codes first.

Only introduce a new stable error code if Feature 3 has a genuine business/API need not already represented.

Errors must continue to use:

```text id="7vcr8c"
errors.Is
errors.As
%w
AppError
ErrorCode
```

according to established project conventions.

Never branch on:

```go id="24i3qn"
err.Error()
```

Never expose SQL/database internals.

---

# 23. Not Found vs Forbidden Security Decision

Feature 3 planning must explicitly evaluate this security question:

Suppose:

```text id="pvtdj4"
Tenant B exists

User A does not belong to Tenant B

User A requests Tenant B
```

Should the API return:

```text id="pd3m37"
403 TENANT_ACCESS_DENIED
```

or intentionally:

```text id="j9s6hm"
404 TENANT_NOT_FOUND
```

to reduce tenant enumeration?

Do not make this decision accidentally.

Inspect existing Epic 01 authorization behaviour.

Recommend the option most consistent with current security conventions.

The chosen behaviour must be tested.

---

# 24. Authentication

All private Feature 3 endpoints require authentication.

Unauthenticated users must not retrieve private tenant information.

Reuse existing authentication middleware.

Do not create a new auth mechanism.

---

# 25. Public Tenant Lookup Is NOT Feature 3

Feature 3 is private authenticated tenant retrieval.

Do not confuse:

```text id="t5ex51"
GET my tenant
```

with:

```text id="67jy5c"
resolve public salon booking page from slug
```

Public tenant lookup belongs to Feature 5.

Feature 3 must not introduce unauthenticated `FindBySlug`.

---

# 26. API Candidates

Planning should evaluate routes consistent with the current API.

Likely examples:

```text id="7e6zva"
GET /api/v1/tenants
```

for:

```text id="m3ysxl"
List My Tenants
```

and:

```text id="42xdn8"
GET /api/v1/tenants/{tenantID}
```

for authorized retrieval.

Do not blindly adopt these if current routing conventions indicate another design.

If SUPER_ADMIN global listing is needed, do not overload normal-user semantics ambiguously.

---

# 27. Handler Responsibilities

Handlers remain thin.

Expected pattern:

```text id="ng8vqz"
Authenticate through middleware
↓
Read principal
↓
Parse route/query parameters
↓
Call service
↓
Map result/error
↓
Return response
```

Do not perform repository queries directly from handlers.

Do not embed membership SQL or role logic in handlers.

---

# 28. Service Responsibilities

The service owns retrieval business policy.

Potential responsibilities:

```text id="axrhvz"
Determine authenticated actor
↓
Verify membership/access
↓
Retrieve tenant(s)
↓
Apply tenant visibility rules
↓
Return domain/public result
```

Do not move transport logic into services.

---

# 29. Repository Responsibilities

Repositories remain persistence-focused.

They may:

```text id="3o83z7"
query tenant
query tenant/member relationship
list visible tenant records
```

They should not decide HTTP status codes.

They should not contain hidden SUPER_ADMIN bypasses unless the established repository architecture already explicitly supports platform-level filtering.

---

# 30. Tenant Isolation

Feature 7 later performs broad tenant-isolation enforcement across tenant-owned business resources.

However, Feature 3 itself MUST securely isolate tenant retrieval.

Do not postpone obvious tenant retrieval security until Feature 7.

Feature 7 will generalize isolation across resources such as:

```text id="up6lyd"
Bookings
Services
Staff
Customers
Payments
```

Feature 3 must already protect the Tenant resource itself.

---

# 31. TDD Requirements

Feature 3 must be developed test-first.

Planning must produce a test matrix before implementation.

At minimum evaluate the following scenarios.

---

## List My Tenants — Active Membership

```text id="x4phtn"
User belongs to Tenant A
User belongs to Tenant B

GET my tenants

→ Tenant A
→ Tenant B
```

---

## List My Tenants — Isolation

```text id="2i51ye"
User belongs to Tenant A

Tenant B exists
but user has no membership

GET my tenants

→ Tenant A
→ NOT Tenant B
```

This is a critical security test.

---

## Inactive Membership

```text id="3mij8u"
User has inactive/revoked membership

GET my tenants

→ tenant excluded
```

if that matches existing membership semantics.

---

## Get Tenant — Authorized

```text id="q0e5bn"
User has valid membership/access

GET tenant

→ 200
```

---

## Get Tenant — Cross-Tenant Attempt

```text id="pdr6ac"
User belongs to Tenant A

GET Tenant B

→ DENIED
```

Expected 403/404 depends on the approved security decision.

---

## Get Tenant — Missing Tenant

```text id="z8m4se"
Tenant ID does not exist

→ TENANT_NOT_FOUND
```

according to existing error mapping.

---

## Unauthenticated

```text id="qz0cdg"
GET tenants

without authentication

→ 401
```

---

## Disabled Tenant

Test expected behaviour according to the Feature 3 policy established during planning.

---

## Repository Filtering

Prove database query semantics do not accidentally return memberships/tenants belonging to another user.

---

# 32. SUPER_ADMIN Tests

If global listing/retrieval is included in Feature 3, explicitly test:

```text id="26nclu"
authorized SUPER_ADMIN
→ permitted platform visibility
```

and:

```text id="9nsdr6"
ordinary tenant user
→ cannot obtain platform-wide listing
```

Do not add these tests if global listing is intentionally deferred.

---

# 33. Empty List Behaviour

A valid authenticated user with no active tenant memberships should normally receive:

```json id="j19io9"
[]
```

rather than:

```text id="futnpq"
404
```

unless existing API conventions explicitly dictate otherwise.

Planning must confirm the project's list-response convention.

---

# 34. Ordering

If listing multiple tenants, define deterministic ordering.

Do not rely on unspecified PostgreSQL row order.

Possible ordering might be:

```text id="c3vbn6"
created_at ASC
```

or:

```text id="81t1ri"
name ASC
```

Use product/repository conventions where available.

The exact choice should be documented and tested only if it is part of the API contract.

---

# 35. Database Drift

Previous work discovered that the shared local development `public.tenants` schema may not match migration history.

This remains an environment issue.

Do NOT:

* rewrite old migrations
* alter production architecture around the drift
* destructively reset shared data

Use isolated test schemas/databases where necessary.

Feature 3 code must target the authoritative migration-defined schema.

---

# 36. Migration Expectations

Feature 3 should normally require:

```text id="jq8qqo"
NO NEW MIGRATION
```

because retrieval should operate on existing:

```text id="pdwj98"
tenants
tenant_memberships
roles
user_roles
```

structures.

If repository inspection identifies a genuinely necessary index for an actual Feature 3 query, planning must justify it before implementation.

Do not modify migrations:

```text id="abgzi7"
000001–000007
```

If a new index/schema change is truly required, create a new forward migration.

Never rewrite history.

---

# 37. Performance

Feature 3 should avoid obvious problems such as:

```text id="gklq8f"
N+1 tenant queries
unbounded platform listing
unindexed high-frequency joins
loading permissions per tenant unnecessarily
```

Do not prematurely optimize for millions of tenants.

Use simple PostgreSQL queries consistent with actual access patterns.

---

# 38. Strict Non-Goals

Feature 3 MUST NOT implement:

```text id="5m3m1s"
Tenant profile updates
Tenant deletion
Tenant suspension
Tenant reactivation
Slug generation
Slug normalization
FindBySlug
Public tenant lookup
Tenant branding
Tenant settings
Custom domains
Booking functionality
Service management
Staff scheduling
Customer management
Payments
Notifications
Subscriptions
Billing
Tenant-wide resource isolation framework
```

Those belong to later features.

---

# 39. Preserve Feature 2

Do not modify the semantics of:

```text id="dksn06"
POST /api/v1/tenants
```

Feature 3 must not alter:

* who can create tenants
* owner provisioning
* BUSINESS_OWNER assignment
* creation transaction boundaries
* duplicate slug behaviour
* creation response semantics

Feature 2 remains closed.

---

# 40. Preserve Epic 01

Do not modify without explicit justification:

```text id="gh8p64"
Role catalog
Permission catalog
Authentication
Token semantics
Membership semantics
BUSINESS_OWNER semantics
STAFF semantics
SUPER_ADMIN semantics
Role assignment semantics
Authorization architecture
```

Feature 3 consumes those capabilities.

It does not redesign them.

---

# 41. Planning-Agent Instructions

When planning Feature 3:

1. Inspect the repository first.
2. Do not implement.
3. Identify existing membership queries that can be reused.
4. Identify existing tenant-context behaviour.
5. Identify existing authorization permissions applicable to tenant retrieval.
6. Determine whether Get Tenant should use tenant middleware.
7. Determine whether cross-tenant unauthorized retrieval returns 403 or 404.
8. Determine whether SUPER_ADMIN global listing belongs in Feature 3.
9. Determine whether pagination is required.
10. Determine the smallest repository additions.
11. Produce the TDD matrix.
12. Identify exact files expected to change.
13. Protect F1/F2/Epic 01 behaviour.

---

# 42. Required Planning Output

A planning agent must return exactly these sections.

## 1. Current Repository Findings

Inspect and report:

* TenantRepository
* MembershipRepository
* membership status semantics
* tenant context
* authorization service
* relevant permissions
* SUPER_ADMIN behaviour
* error codes
* HTTP mappings
* handlers
* routes
* API DTO conventions
* pagination conventions
* integration-test infrastructure

Distinguish observed facts from recommendations.

---

## 2. Feature 3 Scope

State exactly which retrieval/listing capabilities should be delivered.

Explicitly state whether global SUPER_ADMIN listing is included or deferred.

---

## 3. Access-Control Model

Define:

```text id="7xrytz"
who can list
who can retrieve
membership requirements
permission requirements
SUPER_ADMIN behaviour
disabled membership behaviour
disabled tenant behaviour
```

---

## 4. API Contract

For every proposed endpoint specify:

```text id="myh4vw"
Method
Path
Authentication
Tenant context requirement
Permission requirement
Request parameters
Success response
Expected failures
```

---

## 5. Service Design

Describe the minimum service operations and responsibilities.

Do not write production code.

---

## 6. Repository Design

List exact proposed repository additions.

For every method explain:

* why required
* parameters
* result
* filtering
* ordering
* expected errors
* transaction requirement, if any

---

## 7. Query Strategy

Describe the PostgreSQL query strategy.

Explicitly address:

* membership filtering
* tenant filtering
* N+1 avoidance
* deterministic ordering
* indexes already available
* whether any new index is genuinely required

---

## 8. Error Contract

For each relevant failure:

```text id="t5z1s9"
CODE
Scenario
Originating layer
HTTP status
Business/system classification
Wrapping behaviour
```

Explicitly resolve 403 vs 404 for unauthorized tenant IDs.

---

## 9. Security Requirements

List concrete security invariants.

At minimum address:

* tenant-ID enumeration
* cross-tenant retrieval
* inactive membership
* SUPER_ADMIN
* request-controlled tenant IDs
* repository filtering

---

## 10. TDD Test Matrix

Use:

```text id="6z0v8c"
Test
Layer
Scenario
Expected Result
```

Separate:

```text id="bd6r8b"
Service tests
Repository/integration tests
Handler tests
Route tests
Security tests
Regression tests
```

---

## 11. Files Expected To Change

List exact repository paths after inspection.

Use:

```text id="38bx12"
NEW
MODIFY
UNCHANGED
```

---

## 12. Migration Assessment

State:

```text id="w3v3dm"
No migration required
```

or justify a new forward migration.

Never modify 000001–000007.

---

## 13. Implementation Order

Give the future implementation agent a TDD-first sequence.

---

## 14. Risks / Architectural Concerns

Pay particular attention to:

```text id="9q3hzv"
cross-tenant data leakage
N+1 queries
duplicated authorization
role-based shortcuts
SUPER_ADMIN bypasses
tenant enumeration
inactive memberships
pagination
scope creep into Feature 5/7
```

---

## 15. Acceptance Criteria

Every criterion must be objectively testable.

---

## 16. Explicit Non-Changes

Protect:

* Epic 01
* Feature 1
* Feature 2
* migrations 000001–000007
* creation transaction
* role/permission catalogs
* future Epic 02 features

---

# 43. Implementation-Agent Instructions

Once the Feature 3 plan has been separately reviewed and approved, the implementation agent must:

```text id="2zz2vg"
Inspect
↓
Write failing tests
↓
Implement smallest change
↓
Run focused tests
↓
Run security tests
↓
Run Epic 01 regression tests
↓
Run F1/F2 regression tests
↓
Run full test suite
↓
Run go vet
↓
Run formatting checks
↓
Review diff
```

Do not silently expand the approved plan.

---

# 44. Definition of Done

Feature 3 is complete when the platform can securely answer:

```text id="k8ohdp"
Which tenants may this authenticated user see?

May this authenticated user retrieve this tenant?
```

without exposing another tenant's information.

At minimum, the completed feature should prove:

```text id="9l39qb"
User A + Tenant A membership
→ Tenant A visible

User A + no Tenant B membership
→ Tenant B not visible

User A requests Tenant B directly
→ securely denied

Unauthenticated request
→ denied

No membership
→ empty tenant list

Inactive membership
→ does not grant normal tenant visibility
```

Any SUPER_ADMIN behaviour included in the approved plan must also be explicitly tested.

---

# 45. Context Restoration

If this document is being used after context loss:

1. Treat Epic 01 as complete.
2. Treat Epic 02 Feature 1 as complete.
3. Treat Epic 02 Feature 2 as complete.
4. Do not reimplement F1/F2.
5. Inspect the current repository.
6. Plan Feature 3 before implementing it.
7. Preserve migration history.
8. Preserve tenant creation atomicity.
9. Preserve existing role/membership semantics.
10. Continue from Feature 3 only.

---

# FINAL RULE

Feature 3 is a **security boundary**, not merely two GET endpoints.

The implementation must never equate:

```text id="dx1bxb"
"I know this TenantID"
```

with:

```text id="3weqhl"
"I am authorized to see this tenant."
```

Repository retrieval, membership, authentication, and authorization must remain explicit and testable.

Do not begin Feature 4 as part of this feature.
