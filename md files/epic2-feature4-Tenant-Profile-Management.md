# EPIC 02 — TENANT MANAGEMENT

## Feature 4 — Tenant Profile Management

### Agent-Ready Context & Delivery Specification

---

# 1. Purpose

This document is the authoritative context-preservation specification for:

> **Epic 02 — Feature 4: Tenant Profile Management**

Feature 4 enables authorized tenant users to view and modify the business profile information associated with an existing tenant.

This feature builds on:

```text
Epic 01 — Identity & Access
Epic 02 Feature 1 — Tenant Core Model & Persistence
Epic 02 Feature 2 — Tenant Creation & Owner Provisioning
Epic 02 Feature 3 — Tenant Retrieval & Listing
```

Those features are considered complete and must not be redesigned as part of Feature 4.

The actual repository remains authoritative for implementation details.

Any planning or implementation agent must inspect the current repository before proposing changes.

---

# 2. Feature 4 Goal

Feature 4 introduces controlled updates to tenant business-profile information.

Conceptually:

```text
Authenticated User
        ↓
Tenant Context
        ↓
Authorization
        ↓
Update Tenant Profile
        ↓
Persist Changes
        ↓
Return Updated Tenant
```

Feature 4 answers:

> What business information may an authorized tenant user change?

and:

> How do we update that information without allowing cross-tenant modification?

---

# 3. Existing Tenant Foundation

Feature 1 established the core tenant representation.

The current tenant entity is approximately:

```text
Tenant
├── ID
├── Name
├── Slug
├── Status
├── CreatedAt
└── UpdatedAt
```

Exact fields and types must be confirmed from the repository.

Feature 4 must not casually redesign that model.

---

# 4. Existing Tenant Creation

Feature 2 established:

```text
POST /api/v1/tenants
```

and atomically provisions:

```text
Tenant
+
ACTIVE Membership
+
BUSINESS_OWNER
```

for the authenticated creator.

Feature 4 must NOT modify:

* tenant onboarding
* owner provisioning
* BUSINESS_OWNER assignment
* creation transaction semantics
* creation authorization policy

---

# 5. Existing Tenant Retrieval

Feature 3 establishes secure:

```text
List My Tenants
Get Tenant By ID
```

with tenant-aware access controls.

Feature 4 must reuse those existing tenant-access and authorization boundaries.

Do not create a second mechanism for determining whether a user may access a tenant.

---

# 6. Feature 4 Scope

Feature 4 should implement the minimum business-profile update capability.

Potential profile fields include:

```text
Business Name
Business Description
Contact Email
Contact Phone
Timezone
Country
```

However, do NOT automatically add all of these fields.

The planning agent must inspect:

* current tenant schema
* existing profile fields
* existing product requirements
* current migrations
* frontend needs

and recommend the smallest coherent profile model.

---

# 7. Core Distinction — Identity vs Profile vs Settings

Feature 4 must maintain clear boundaries.

## Tenant Identity

Core identity includes concepts such as:

```text
ID
Slug
Status
CreatedAt
```

These are not ordinary profile-edit fields.

---

## Tenant Profile

Business-facing descriptive information may include:

```text
Name
Description
Contact Email
Contact Phone
Country
Timezone
```

This is Feature 4 territory.

---

## Tenant Settings

Configuration such as:

```text
Currency
Locale
Booking rules
Cancellation policy
Notification preferences
Business hours configuration
```

belongs primarily to:

> Feature 9 — Tenant Settings Foundation

Do not turn Feature 4 into a generic settings system.

---

## Branding

Fields such as:

```text
Logo
Primary Color
Secondary Color
Theme
```

belong to:

> Feature 10 — Tenant Branding Foundation

Do not add branding to Feature 4.

---

# 8. Slug Boundary

Slug exists in tenant persistence.

However, Feature 5 owns:

```text
Slug generation
Slug normalization
Reserved slugs
Slug public identity
FindBySlug
Slug-change policy
Public tenant routing
```

Therefore Feature 4 should NOT casually allow:

```text
PATCH tenant
{
    "slug": "new-value"
}
```

unless repository inspection reveals an already-approved slug update contract.

Default Feature 4 direction:

> Slug is NOT editable through the general tenant profile endpoint.

---

# 9. Status Boundary

Tenant status currently uses existing lifecycle semantics such as:

```text
ACTIVE
DISABLED
```

Feature 8 owns tenant lifecycle management.

Therefore Feature 4 must NOT allow profile updates to modify:

```text
status
```

Do not accept lifecycle state in the profile update DTO.

---

# 10. Tenant ID Boundary

Tenant ID is immutable.

Feature 4 must never allow a request to replace or mutate:

```text
Tenant.ID
```

The tenant being updated comes from:

```text
route/context
```

not from a body-controlled tenant ID.

---

# 11. Expected Endpoint

Planning should evaluate an endpoint approximately like:

```text
PATCH /api/v1/tenants/{tenantID}
```

or another route consistent with the current project.

PATCH is preferred conceptually because profile updates may be partial.

However, the planning agent must inspect existing HTTP conventions before finalizing.

Potential alternative:

```text
PUT /api/v1/tenants/{tenantID}/profile
```

Do not choose solely from preference.

Use existing route style where appropriate.

---

# 12. Authentication

Tenant profile modification must require authentication.

Expected:

```text
Unauthenticated
→ 401
```

using the project's existing authentication error contract.

Do not create a new auth mechanism.

---

# 13. Tenant Context

The update request must operate inside an established tenant context.

Conceptually:

```text
Authentication
        ↓
Tenant Resolution
        ↓
Membership
        ↓
Authorization
        ↓
Update Profile
```

Reuse the existing tenant-context mechanism where appropriate.

Do not trust the tenant ID simply because it appears in the URL.

---

# 14. Authorization

Feature 4 must determine which existing permission controls tenant profile modification.

Inspect the Epic 01 permission catalog.

Do NOT invent a new permission before checking whether one already exists.

Potential existing concepts might resemble:

```text
tenant.update
tenant.manage
tenant.settings.update
```

but use actual repository findings.

The planning agent must explicitly report:

```text
Permission used
Scope
Which roles currently receive it
Why it applies to Feature 4
```

Do not authorize profile updates through hard-coded role checks such as:

```go
if role == "BUSINESS_OWNER"
```

if the existing permission architecture already supports capability checks.

---

# 15. BUSINESS_OWNER vs STAFF

The system already has:

```text
BUSINESS_OWNER
STAFF
```

Feature 4 should follow permissions rather than assuming all staff may or may not edit profile information.

For example:

```text
BUSINESS_OWNER
+ tenant profile update permission
→ ALLOWED

STAFF
without permission
→ DENIED
```

The role-permission matrix remains authoritative.

Do not change it during Feature 4 unless an explicit product decision requires a new permission.

---

# 16. SUPER_ADMIN

Do not introduce implicit:

```text
if SUPER_ADMIN → bypass everything
```

logic.

Inspect existing PLATFORM vs TENANT authorization semantics.

If SUPER_ADMIN already has a supported mechanism to manage arbitrary tenants, reuse it.

If not, do not expand SUPER_ADMIN behaviour inside Feature 4.

---

# 17. Update DTO

The API should use an explicit update request DTO.

Do not decode directly into:

```go
model.Tenant
```

because that risks clients controlling protected fields.

The DTO should expose only Feature 4-editable fields.

Conceptually:

```text
UpdateTenantProfileRequest
├── Name?
├── Description?
├── ContactEmail?
├── ContactPhone?
├── Timezone?
└── Country?
```

Exact fields must follow the approved schema.

It must NOT expose:

```text
ID
Slug
Status
CreatedAt
UpdatedAt
OwnerID
Role
Permissions
```

---

# 18. PATCH Semantics

If PATCH is used, distinguish:

```text
field omitted
```

from:

```text
field intentionally cleared
```

where necessary.

For example:

```text
contact_phone omitted
→ leave unchanged
```

versus:

```text
contact_phone = ""
→ maybe clear phone
```

The planning agent must inspect current DTO/validation patterns.

Do not blindly use pointer fields unless they are justified by partial-update semantics.

---

# 19. Validation

Feature 4 validation may include:

```text
Name not empty if provided
Email valid if provided
Phone reasonable if provided
Timezone valid if provided
Country format valid if provided
Length limits
```

Do not over-engineer international validation.

Use existing validators/libraries if present.

Validation should be split appropriately among:

```text
Transport
Business/domain
Database constraints
```

Do not duplicate all validation at every layer.

---

# 20. Tenant Name

If tenant name is editable, define rules clearly.

Potential rules:

```text
Trim whitespace
Cannot become empty
Maximum length
```

Use existing creation constraints where possible.

Do not silently introduce different rules between:

```text
Create Tenant
```

and:

```text
Update Tenant
```

unless product requirements justify them.

---

# 21. Description

If description is added, determine:

```text
nullable?
empty string allowed?
maximum length?
TEXT vs VARCHAR?
```

Do not assume.

Planning must justify schema decisions.

---

# 22. Contact Email

Contact email is business contact information.

It is NOT automatically:

```text
the owner's login email
```

Keep business contact identity separate from authentication identity.

Do not update the authenticated user's account email through the tenant-profile endpoint.

---

# 23. Contact Phone

Contact phone belongs to the tenant/business profile.

Do not accidentally write it into:

```text
users
```

or owner membership data.

---

# 24. Timezone

Timezone is particularly important for a booking system.

A tenant's timezone will eventually influence:

```text
Availability
Appointment times
Notifications
Business hours
Reports
```

If Feature 4 adds timezone, use a stable representation.

Prefer recognized timezone identifiers conceptually such as:

```text
Africa/Lagos
Europe/London
America/New_York
```

rather than raw numeric UTC offsets if practical.

The planning agent must inspect existing project dependencies/conventions before selecting validation strategy.

---

# 25. Country

Country may be useful for:

```text
regional settings
phone formatting
currency defaults
future billing
```

but Feature 4 should not derive or implement those downstream behaviours yet.

If country is persisted, keep the representation simple and stable.

Do not build a geographic domain framework.

---

# 26. Database Design

If the current `tenants` table lacks required Feature 4 profile columns, use a new forward migration.

Existing migrations are immutable.

Do NOT modify:

```text
000001–000007
```

or any migration already introduced by Feature 3.

If the latest migration number has advanced, inspect it and create the next one.

Potential profile additions might conceptually resemble:

```sql
description TEXT
contact_email VARCHAR(...)
contact_phone VARCHAR(...)
timezone VARCHAR(...)
country VARCHAR(...)
```

Do not copy this blindly.

The planning agent must propose the exact schema after inspecting existing migrations.

---

# 27. Nullable vs Required

Do not make every new profile field mandatory.

A newly onboarded tenant may initially have only:

```text
Name
Slug
```

Feature 4 may be where the rest of the profile is completed.

Therefore evaluate whether optional fields should be nullable.

The plan must explicitly justify:

```text
NOT NULL
vs
NULL
vs
DEFAULT ''
```

Prefer clear database semantics over arbitrary defaults.

---

# 28. Repository Design

Feature 4 likely needs a narrow update operation.

Do not add full generic CRUD.

Potential concept:

```text
UpdateProfile(...)
```

or:

```text
Update(...)
```

depending on repository conventions.

The plan must determine whether the repository should accept:

```text
Tenant
```

or:

```text
TenantProfilePatch
```

or explicit update parameters.

Avoid abstractions that allow updating protected fields accidentally.

---

# 29. Safe SQL Updates

The repository must never construct field names from untrusted user input.

Use parameterized SQL.

If dynamic partial-update SQL is used, the set of columns must be controlled by code.

Do not concatenate request keys directly into SQL.

---

# 30. Tenant-Scoped Update Predicate

The SQL update should target the tenant explicitly.

Conceptually:

```sql
UPDATE tenants
SET ...
WHERE id = $tenantID
```

Authorization must already have established that the caller may operate on that tenant.

Do not allow the body to provide the update target.

---

# 31. UpdatedAt

Feature 4 is the first feature that genuinely updates tenant rows.

The planning agent must inspect how timestamps are handled across the project.

Determine whether to:

```text
set updated_at in SQL
```

or use an established database trigger/convention.

Feature 1 explicitly deferred adding a tenant-only trigger.

Do not introduce inconsistent timestamp behaviour.

---

# 32. Lost Updates / Concurrency

Do not over-engineer optimistic locking unless the project already has a convention for it.

Feature 4 does not automatically require:

```text
version columns
ETags
distributed locks
```

A normal PostgreSQL update is sufficient unless repository evidence says otherwise.

---

# 33. Returned Tenant

A successful profile update should return the updated public tenant representation according to existing API conventions.

Conceptually:

```text
200 OK
```

with:

```json
{
  "id": "...",
  "name": "...",
  "slug": "...",
  "status": "...",
  "created_at": "...",
  "updated_at": "..."
}
```

plus approved new profile fields.

Do not expose internal database details.

---

# 34. Cross-Tenant Security

Critical test:

```text
User A belongs to Tenant A

User A sends update against Tenant B

→ DENIED

Tenant B remains unchanged
```

The test must prove both:

```text
request denied
```

and:

```text
data unchanged
```

---

# 35. Missing Permission

If the caller is an active member but lacks the required profile-update permission:

```text
→ 403
```

using existing authorization error semantics.

The database must remain unchanged.

---

# 36. Disabled Membership

A disabled/revoked/inactive membership must not permit profile modification.

Reuse existing membership semantics.

Do not invent new membership states.

---

# 37. Disabled Tenant

Feature 8 owns lifecycle.

However Feature 4 must determine whether a disabled tenant may be edited.

Inspect existing tenant-context semantics.

If existing middleware already blocks disabled tenants:

```text
DISABLED tenant
→ tenant context denied
```

preserve that rule.

Do not bypass it in the update service.

---

# 38. Not Found / Enumeration

Reuse Feature 3's established tenant visibility/error policy.

Do not create a new difference where:

```text
GET inaccessible tenant → 403
PATCH inaccessible tenant → 404
```

unless existing architecture explicitly requires that distinction.

Feature 4 should remain consistent with the approved security model.

---

# 39. Structured Error Handling

Reuse existing AppError infrastructure.

Potential existing errors might include:

```text
INVALID_REQUEST
TENANT_NOT_FOUND
TENANT_ACCESS_DENIED
PERMISSION_DENIED
INTERNAL_ERROR
```

Use actual codes.

Only introduce new Feature 4 errors if there is a clear business distinction callers need.

Do not create speculative codes for every validation failure.

Never branch on:

```go
err.Error()
```

Preserve:

```text
errors.Is
errors.As
%w
```

and safe API mapping.

---

# 40. Audit Boundary

Feature 11 owns Tenant Audit & Administrative Events.

However, inspect whether Epic 01 already automatically audits sensitive updates.

If profile updates are already expected to flow through an existing generic audit mechanism, reuse it.

Do NOT build a second tenant-audit system.

Do NOT expand Feature 11 prematurely.

---

# 41. API Candidate

Expected conceptual route:

```text
PATCH /api/v1/tenants/{tenantID}
```

Possible middleware chain:

```text
Authentication
↓
Tenant Context
↓
Permission Authorization
↓
Tenant Handler
```

Planning must verify exact existing middleware ordering.

---

# 42. Handler Responsibilities

Handler should:

```text
read route/context
↓
decode request
↓
perform transport validation
↓
call service
↓
serialize response/error
```

Handler must NOT:

```text
write SQL
resolve roles manually
perform membership queries directly
modify tenant model arbitrarily
```

---

# 43. Service Responsibilities

The service should own business update semantics.

Potential responsibilities:

```text
Validate requested fields
Apply update rules
Call repository
Return updated tenant
```

If authorization is fully enforced by middleware, avoid duplicating permission checks unnecessarily.

If service-level access checks are part of the existing architecture, reuse them consistently.

---

# 44. Repository Responsibilities

Repository owns persistence only.

It should not:

```text
decide HTTP status
check role names
parse auth tokens
read request bodies
```

Repository errors should remain structured/wrapped according to existing conventions.

---

# 45. PATCH with No Fields

The planning agent must decide:

```text
PATCH {}
```

Should it return:

```text
400 INVALID_REQUEST
```

or be accepted as a no-op?

Recommend explicit rejection unless existing API conventions favor no-op updates.

This prevents meaningless update requests.

---

# 46. Unknown JSON Fields

Follow the project's existing JSON decoding convention.

If unknown fields are ignored globally, do not change global behavior during Feature 4.

But protected fields must still be impossible to mutate because they are absent from the request DTO.

For example:

```json
{
  "status": "ACTIVE"
}
```

must not change tenant status even if unknown fields are ignored.

---

# 47. SQL Injection

All update values must be parameterized.

Dynamic partial updates must whitelist column names in code.

Never build SQL like:

```text
"SET " + requestField + " = ..."
```

from arbitrary client input.

---

# 48. TDD Requirements

Feature 4 must be implemented test-first.

Planning must produce tests at all applicable layers.

---

# 49. Repository / Integration Tests

At minimum consider:

### Successful Profile Update

```text
Existing Tenant
↓
Update allowed profile fields
↓
row changes
↓
updated_at changes
```

---

### Protected Fields Remain Unchanged

Verify profile update cannot modify:

```text
ID
Slug
Status
CreatedAt
```

through the update contract.

---

### Invalid / Empty Name

If name is updated to an invalid value:

```text
update rejected
tenant unchanged
```

---

### Optional Fields

If optional profile fields are introduced:

```text
set value
clear value if supported
leave omitted value unchanged
```

---

### Missing Tenant

Verify existing not-found/access-denied semantics.

---

### Context Cancellation

Where existing repository tests follow this pattern:

```text
cancelled context
→ no successful update
```

---

# 50. Service Tests

At minimum:

```text
valid update succeeds
invalid update rejected before repository write
empty patch rejected
protected fields cannot be updated
repository failure propagates safely
```

If service-level authorization exists:

```text
cross-tenant denied
missing permission denied
inactive membership denied
```

---

# 51. Handler Tests

At minimum:

```text
unauthenticated → 401
malformed tenant ID → established error
malformed JSON → 400
empty request → 400
invalid field → 400
valid update → 200
service AppError → correctly mapped
```

---

# 52. Route / Middleware Tests

The Feature 3 routing defect demonstrated why full-chain tests are necessary.

Feature 4 MUST include route-level tests through the real middleware stack.

At minimum:

```text
unauthenticated → 401

valid auth + no membership → denied

valid membership + missing update permission → denied

valid membership + permission → update succeeds

cross-tenant attempt → denied and other tenant unchanged
```

Do not rely only on handler tests.

---

# 53. Security Tests

Explicitly test:

```text
Tenant A user cannot update Tenant B.

Client cannot update tenant ID.

Client cannot update status.

Client cannot change slug.

Client cannot assign owner.

Client cannot change roles/permissions.

Missing permission denies mutation.

Inactive membership denies mutation.
```

These tests are mandatory security requirements.

---

# 54. Migration Tests

If Feature 4 introduces profile columns, test migration/schema contracts.

Examples:

```text
columns exist
nullable semantics correct
length constraints correct where applicable
down migration removes only Feature 4 columns
```

Do not test PostgreSQL itself unnecessarily.

Test the schema decisions this feature owns.

---

# 55. Existing Migration History

Do NOT rewrite old migrations.

Inspect the current latest migration number.

If Feature 3 introduced no migration and Feature 2 ended at:

```text
000007
```

then Feature 4 schema evolution may begin at:

```text
000008
```

but verify actual repository state first.

Do not assume numbering.

---

# 56. Local Database Drift

Previous work found possible drift in the shared development database.

Do not modify production code to compensate.

Use isolated test schemas/databases.

Do not destructively reset shared development data.

Separate environment repair from feature implementation.

---

# 57. Frontend Contract

Feature 4 will eventually power a dashboard page such as:

```text
Settings / Business Profile
```

The frontend may need to:

```text
load tenant
↓
edit business profile
↓
PATCH profile
↓
invalidate/refetch tenant query
```

Do not return dashboard-specific combined payloads.

Keep the backend response focused on tenant profile information.

---

# 58. Strict Non-Goals

Feature 4 MUST NOT implement:

```text
Tenant slug editing
Slug normalization
Public slug lookup
Tenant deletion
Tenant suspension/reactivation
Tenant settings system
Booking settings
Notification settings
Branding
Logo upload
Colors/theme
Custom domains
Bookings
Services
Staff scheduling
Customers
Payments
Subscriptions
Billing
Global tenant administration
```

Do not begin Feature 5.

---

# 59. Protect Epic 01

Do not redesign:

```text
Authentication
Membership
Roles
Permissions
Authorization
Tenant context
SUPER_ADMIN
BUSINESS_OWNER
STAFF
Role assignment
Error infrastructure
```

Feature 4 consumes these capabilities.

---

# 60. Protect Feature 1

Do not break:

```text
Tenant Create
Tenant FindByID
Slug uniqueness
Tenant core model
```

Only evolve the model/schema where Feature 4 explicitly requires profile fields.

---

# 61. Protect Feature 2

Do not alter:

```text
POST /api/v1/tenants
owner provisioning
BUSINESS_OWNER assignment
atomic creation transaction
TENANT_SLUG_TAKEN
```

If new profile columns are optional, tenant creation should continue working without requiring Feature 4 fields.

---

# 62. Protect Feature 3

Do not alter Feature 3 retrieval/listing semantics unnecessarily.

Feature 4 should reuse:

```text
tenant context
tenant.read policy
enumeration-safe access behavior
membership isolation
```

where applicable.

If Feature 4 requires a write permission distinct from `tenant.read`, use the existing permission catalog if available.

---

# 63. Planning-Agent Instructions

When planning Feature 4:

1. Inspect the current repository.
2. Inspect existing tenant fields.
3. Inspect current permission catalog.
4. Determine which profile fields belong in F4.
5. Determine whether schema evolution is needed.
6. Determine PATCH vs PUT.
7. Determine DTO partial-update semantics.
8. Determine write permission.
9. Determine middleware chain.
10. Determine disabled-tenant behavior.
11. Determine error contract.
12. Determine `updated_at` strategy.
13. Define TDD tests before implementation.
14. List exact files.
15. Do not implement.

---

# 64. Required Planning Output

The planning agent must return exactly these sections.

## 1. Current Repository Findings

Inspect and report:

* Tenant model
* tenant migrations
* TenantRepository
* TenantService
* TenantHandler
* Feature 3 routes
* tenant-context middleware
* relevant permission codes
* role-permission grants
* error codes
* validation conventions
* timestamp conventions
* API DTO conventions
* integration-test infrastructure

Separate observed facts from recommendations.

---

## 2. Feature 4 Scope

Explicitly list which profile fields will be added/editable.

List fields deliberately excluded.

---

## 3. Profile Data Model

For each proposed field state:

```text
Field
Go type
Database type
Nullable?
Validation
Default
Reason
```

---

## 4. Migration Design

If schema changes are needed:

```text
migration number
UP behavior
DOWN behavior
constraints
defaults
nullability
```

Do not modify existing migrations.

---

## 5. API Contract

Specify:

```text
Method
Path
Authentication
Tenant context
Permission
Request DTO
Response DTO
Status codes
Errors
```

---

## 6. Authorization Model

State:

```text
required permission
scope
roles currently granted it
membership requirement
disabled membership behavior
disabled tenant behavior
SUPER_ADMIN behavior
```

Use repository facts.

---

## 7. Update Semantics

Explain:

```text
PATCH vs PUT
partial fields
omitted values
clear/null behavior
empty patch
protected fields
```

---

## 8. Service Design

List proposed service methods and responsibilities.

---

## 9. Repository Design

List the smallest required repository change.

For each method include:

```text
Purpose
Input
Output
Updateable columns
Errors
Context handling
```

---

## 10. SQL Strategy

Explain:

* parameterization
* partial update strategy
* safe column selection
* updated_at handling
* row-not-found handling

---

## 11. Error Contract

For each failure:

```text
CODE
Scenario
Layer
HTTP Status
Business/System
Wrapping
```

Reuse existing codes where possible.

---

## 12. Security Requirements

Address:

```text
cross-tenant update
tenant ID immutability
slug protection
status protection
permission enforcement
inactive membership
disabled tenant
SQL injection
request-controlled protected fields
```

---

## 13. TDD Test Matrix

Use:

```text
Test
Layer
Scenario
Expected Result
```

Separate:

* migration/integration
* repository
* service
* handler
* route/app
* security
* regression

---

## 14. Files Expected To Change

Use exact inspected paths.

Separate:

```text
NEW
MODIFY
UNCHANGED
```

---

## 15. Implementation Order

Provide a TDD-first implementation sequence for the future implementation agent.

---

## 16. Risks / Architectural Concerns

Address:

```text
scope creep into settings
scope creep into slug/lifecycle
cross-tenant mutation
over-posting/mass assignment
unsafe dynamic SQL
timestamp inconsistency
profile-vs-user identity confusion
nullable-field complexity
migration drift
authorization duplication
```

---

## 17. Acceptance Criteria

Every item must be objectively testable.

---

## 18. Explicit Non-Changes

Protect:

```text
Epic 01
Feature 1
Feature 2
Feature 3
role catalog
permission catalog unless explicitly approved
creation transaction
slug behavior
lifecycle behavior
future F5+
```

---

# 65. Definition of Done

Feature 4 is complete only when an authorized tenant user can update approved business-profile fields while all protected tenant identity/security fields remain unchanged.

At minimum prove:

```text
Authorized member + correct permission
→ profile update succeeds

Member without update permission
→ denied

User from Tenant A updating Tenant B
→ denied

Unauthenticated
→ denied

Malformed request
→ rejected

Slug update attempt
→ no slug change

Status update attempt
→ no status change

Tenant ID update attempt
→ no ID change

Profile fields persist correctly

updated_at reflects successful mutation

Epic 01 + F1 + F2 + F3 remain green
```

---

# 66. Context Restoration

If this document is used after context loss:

```text
Epic 01     COMPLETE
Epic 02 F1  COMPLETE
Epic 02 F2  COMPLETE
Epic 02 F3  COMPLETE
Epic 02 F4  CURRENT FEATURE
```

Do not reimplement prior features.

Inspect the repository and plan Feature 4 only.

---

# FINAL RULE

Feature 4 is:

> **Tenant Profile Management**

It is NOT:

```text
Tenant Settings
Branding
Slug Management
Lifecycle Management
Role Management
Booking Configuration
```

Keep the feature narrow, secure, and tenant-isolated.

The tenant profile update must never become a generic “update any Tenant field” endpoint.

Do not begin Feature 5.
