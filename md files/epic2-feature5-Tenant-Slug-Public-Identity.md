# EPIC 02 — TENANT MANAGEMENT

## Feature 5 — Tenant Slug & Public Identity

### Agent-Ready Context & Delivery Specification

---

# 1. Purpose

This document is the authoritative context-preservation specification for:

> **Epic 02 — Feature 5: Tenant Slug & Public Identity**

Feature 5 turns the tenant slug from a persisted database field into a stable, validated public identity that can later support public booking URLs.

This feature builds on:

```text
Epic 01 — Identity & Access
Epic 02 F1 — Tenant Core Model & Persistence
Epic 02 F2 — Tenant Creation & Owner Provisioning
Epic 02 F3 — Tenant Retrieval & Listing
Epic 02 F4 — Tenant Profile Management
```

All of those are considered complete.

The actual repository remains authoritative for implementation details.

Planning and implementation agents must inspect the repository before making changes.

---

# 2. Feature Goal

Feature 5 establishes a safe public tenant identity.

Conceptually:

```text
Tenant
├── ID
│   └── internal/security identity
│
└── Slug
    └── public URL identity
```

Example:

```text
Tenant Name:
Acme Beauty Studio

Tenant ID:
550e8400-e29b-41d4-a716-446655440000

Tenant Slug:
acme-beauty-studio
```

Later, the public web experience may use URLs such as:

```text
/book/acme-beauty-studio
```

or another routing scheme chosen by the frontend/product.

Feature 5 must make slug lookup safe and predictable.

---

# 3. Existing Slug Foundation

Feature 1 already established slug persistence.

The tenant currently contains approximately:

```text
Tenant
├── ID
├── Name
├── Slug
├── Status
├── Description
├── ContactEmail
├── ContactPhone
├── Timezone
├── CreatedAt
└── UpdatedAt
```

Exact repository state must be inspected.

Slug persistence currently includes a database uniqueness constraint.

Do NOT recreate the slug column.

Do NOT rewrite the existing slug migration.

---

# 4. Existing Tenant Creation

Feature 2 currently accepts a tenant slug during onboarding.

Conceptually:

```json
{
  "name": "Acme Beauty Studio",
  "slug": "acme-beauty-studio"
}
```

The backend already protects uniqueness through the database.

Feature 2 also established:

```text
TENANT_SLUG_TAKEN
→ HTTP 409
```

Feature 5 must build on that behaviour rather than duplicate it.

---

# 5. Existing Profile Management

Feature 4 established that:

```text
Name
```

is editable, while:

```text
Slug
```

remains protected.

This is deliberate.

A business may change:

```text
Acme Salon
→
Acme Beauty Studio
```

without automatically changing:

```text
acme-salon
```

Feature 5 must preserve that separation.

---

# 6. Core Feature 5 Responsibilities

Feature 5 should evaluate and implement the minimum coherent set of slug/public-identity behaviours.

Likely responsibilities:

```text
Slug validation
Slug normalization
Reserved slug protection
Lookup tenant by slug
Public-safe tenant identity response
Slug uniqueness conflict handling
```

Planning must determine whether slug updates belong in this feature or should remain immutable after creation.

Do not assume slug mutation automatically belongs here.

---

# 7. Public Identity Principle

The slug is a human-friendly identifier.

The UUID remains the authoritative internal tenant identifier.

Conceptually:

```text
Public:
acme-beauty-studio

Internal:
550e8400-e29b-41d4-a716-446655440000
```

The system should never replace UUID-based tenant security with slug-based authorization.

A slug lookup may resolve:

```text
slug
→ Tenant
```

but internal tenant-scoped operations should continue using the tenant ID/security context established by the backend.

---

# 8. Slug Format

Feature 5 should define a strict canonical slug format.

Recommended shape:

```text
lowercase letters
numbers
hyphens
```

Examples:

```text
acme-salon
barber-247
studio-one
nnamdi-beauty
```

Reject or normalize values such as:

```text
Acme Salon
ACME-SALON
acme_salon
acme@salon
```

according to the approved normalization strategy.

---

# 9. Recommended Canonical Format

The planning agent should evaluate a rule approximately like:

```regex
^[a-z0-9]+(?:-[a-z0-9]+)*$
```

with an explicit length bound.

Potential limits:

```text
minimum: 3
maximum: 63
```

but these are not mandatory until confirmed.

The plan must justify chosen limits.

Do not allow:

```text
leading hyphen
trailing hyphen
double separator sequences
spaces
uppercase in persisted canonical form
```

---

# 10. Normalization

Feature 5 must decide whether callers submit already-normalized slugs or whether the service normalizes them.

Preferred product behaviour:

```text
"Acme Beauty Studio"
→
"acme-beauty-studio"
```

or:

```text
"Acme-Beauty-Studio"
→
"acme-beauty-studio"
```

However, normalization must be deliberate and deterministic.

Do not silently implement complex transliteration without a defined product requirement.

---

# 11. Slug Generation

Planning must explicitly answer:

> Should the backend generate a slug automatically from the tenant name?

There are two valid approaches.

## Option A — Caller supplies slug

```text
name = Acme Beauty Studio
slug = acme-beauty-studio
```

Backend validates and normalizes it.

## Option B — Backend generates slug

```text
name = Acme Beauty Studio
↓
acme-beauty-studio
```

If conflict:

```text
acme-beauty-studio already exists
```

the system needs a conflict strategy.

Do NOT automatically introduce:

```text
acme-beauty-studio-1
acme-beauty-studio-2
```

unless product requirements explicitly choose that behaviour.

Feature 2 currently accepts slug input, so the planning agent must strongly consider preserving that established API contract rather than rewriting onboarding.

---

# 12. Reserved Slugs

Certain slugs should not be assignable to businesses because they conflict with platform routes, platform identity, or future operations.

Potential reserved values include concepts such as:

```text
admin
api
login
logout
register
signup
dashboard
settings
support
help
pricing
about
book
booking
auth
www
app
system
platform
```

Do NOT blindly hard-code this exact list.

The planning agent must inspect:

* current routes
* planned public URL shape
* admin routes
* auth routes

and create the smallest justified reserved set.

---

# 13. Reserved Slug Ownership

The reserved-slug rules should live in one clear place.

Avoid duplicating lists across:

```text
handler
service
frontend
database
```

The backend must remain authoritative.

The frontend may mirror rules for UX, but backend validation decides acceptance.

---

# 14. Existing Slug Uniqueness

PostgreSQL already enforces slug uniqueness.

Feature 5 must preserve:

```text
unique(slug)
```

or its actual existing constraint.

Application-level pre-checks may improve UX but must never replace database uniqueness.

Correct race-safe pattern:

```text
validate
↓
attempt write
↓
database unique constraint remains final authority
```

---

# 15. TENANT_SLUG_TAKEN

Reuse existing:

```text
TENANT_SLUG_TAKEN
```

Do NOT invent:

```text
SLUG_DUPLICATE
TENANT_NAME_TAKEN
PUBLIC_IDENTITY_EXISTS
```

for the same condition.

The existing structured error contract should remain authoritative.

---

# 16. New Slug Errors

Planning should determine whether Feature 5 needs a new structured error such as:

```text
TENANT_SLUG_INVALID
```

for invalid syntax/reserved slug conditions.

Only add a stable code if the frontend needs to distinguish slug validation from generic request validation.

Possible distinction:

```text
TENANT_SLUG_INVALID
→ syntax/normalization failure

TENANT_SLUG_TAKEN
→ valid slug but already owned
```

Do not create excessive codes.

---

# 17. Public Lookup

Feature 5 should introduce tenant lookup by slug.

Conceptually:

```text
slug
↓
TenantRepository.FindBySlug
↓
Public Tenant Identity
```

This will support future public booking experiences.

The planning agent must determine the correct endpoint and response scope.

---

# 18. Public Lookup Endpoint

Potential route:

```text
GET /api/v1/public/tenants/{slug}
```

or another route consistent with existing API conventions.

Alternative:

```text
GET /api/v1/tenants/by-slug/{slug}
```

is less clearly public.

The planning agent must inspect current public/private routing conventions.

The endpoint should make its unauthenticated/public nature obvious.

---

# 19. Public Endpoint Authentication

Public tenant identity lookup should generally NOT require authentication.

Expected:

```text
Customer
↓
public booking URL
↓
resolve tenant slug
```

Customers should not need to log in just to identify the business.

This is different from:

```text
GET /api/v1/tenants/{tenantID}
```

which remains a private authenticated tenant operation from Feature 3.

---

# 20. Public Response Must Be Minimal

Public slug lookup must NOT expose the full internal tenant object automatically.

The response should include only fields safe for public use.

Potentially:

```text
id?
name
slug
status?
description
timezone?
```

Planning must decide whether the internal UUID should be publicly returned.

Strongly evaluate whether it is necessary.

Do NOT expose:

```text
contact email unless intentionally public
contact phone unless intentionally public
membership
roles
permissions
created_at
updated_at
internal status details
billing
settings
```

without explicit product need.

---

# 21. Public Tenant DTO

Prefer a dedicated DTO such as conceptually:

```go
type PublicTenantIdentity struct {
    Name        string
    Slug        string
    Description *string
}
```

rather than reusing the admin-facing `PublicTenant` blindly.

The public booking surface and authenticated dashboard have different data-exposure requirements.

---

# 22. Disabled Tenant Behaviour

Feature 5 must define what happens when a slug belongs to a disabled tenant.

A disabled business should generally not have a public booking identity available.

Possible public result:

```text
404
```

rather than revealing:

```text
"This business is disabled"
```

unless product requirements require a public unavailable page.

This decision must remain consistent with future lifecycle behaviour in Feature 8.

Do not implement lifecycle transitions here.

---

# 23. Missing Slug

Public request:

```text
GET /public/tenants/nonexistent-business
```

should normally produce:

```text
404
```

with a stable public not-found response.

This is different from private tenant enumeration behavior.

Public slug resolution inherently needs to tell the browser whether a public business identity exists.

Do not reuse private 403 enumeration semantics blindly.

---

# 24. Public Enumeration Consideration

Slugs are deliberately public identifiers.

Therefore it is acceptable that:

```text
existing public slug
→ business identity

nonexistent public slug
→ 404
```

This is fundamentally different from exposing arbitrary tenant UUID existence.

The planning agent should explicitly distinguish:

```text
Public slug identity
```

from:

```text
Private tenant UUID
```

security policies.

---

# 25. FindBySlug Repository Method

Feature 5 likely requires a narrow method such as:

```go
FindBySlug(ctx, slug)
```

This method should:

```text
use canonical slug
query exact match
return one tenant
map no rows appropriately
wrap DB failures
```

Do not turn the repository into generic search functionality.

---

# 26. Slug Update Question

Planning must explicitly resolve whether an existing tenant may change its slug.

This matters because changing:

```text
acme-salon
```

to:

```text
acme-beauty
```

could break existing links.

Possible policy:

```text
slug immutable after creation
```

This is the safest initial SaaS behaviour.

Alternative:

```text
authorized BUSINESS_OWNER may change slug
```

would require:

```text
tenant.update or dedicated permission
conflict validation
reserved slug validation
old URL behaviour
redirect strategy
audit
```

That is substantially more complex.

Preferred Feature 5 default:

> Keep slug immutable after tenant creation unless there is a compelling current product requirement.

---

# 27. Slug Rename Redirects

Do NOT introduce:

```text
slug history
redirect table
301 redirects
alias resolution
```

unless slug mutation is explicitly approved.

These are future concerns.

---

# 28. Feature 2 Creation Integration

Feature 5 should strengthen slug validation used during Feature 2 tenant creation.

Currently creation already accepts slug.

Feature 5 may centralize:

```text
normalize slug
validate syntax
check reserved values
```

before the tenant creation transaction executes.

Do NOT break Feature 2 atomic provisioning.

Slug validation should happen before opening the transaction where practical.

---

# 29. Shared Slug Domain Logic

Avoid duplicating slug logic in:

```text
TenantService.Create
PublicSlugService
Handler
```

Prefer a narrow shared domain/helper component if justified.

Possible concepts:

```text
NormalizeSlug(...)
ValidateSlug(...)
```

Keep it simple.

Do not create a large slug subsystem.

---

# 30. Handler Responsibilities

Public slug lookup handler should:

```text
read slug route parameter
↓
perform transport-level checks
↓
call service
↓
serialize public DTO
```

It should not:

```text
query DB directly
implement reserved list
perform authorization logic
construct SQL
```

---

# 31. Service Responsibilities

Feature 5 service should own public identity rules such as:

```text
normalize/validate slug
resolve tenant
apply public visibility rule
return public identity
```

If slug creation validation is shared with Feature 2, centralize carefully.

---

# 32. Repository Responsibilities

Repository should:

```text
persist/query canonical slug
```

It should not:

```text
know HTTP routes
know public page layout
know reserved route names
know frontend routing
```

Reserved-name policy belongs above persistence.

---

# 33. Public Slug Case Handling

Canonical persisted slugs should be lowercase.

A request for:

```text
Acme-Salon
```

may either:

```text
normalize → acme-salon
```

or:

```text
reject
```

The planning agent must choose one policy.

Preferred UX:

```text
normalize case before lookup
```

while persisted form remains canonical lowercase.

---

# 34. Trailing/Leading Spaces

Input such as:

```text
" acme-salon "
```

should not become a separate identity.

Normalize surrounding whitespace before validation where appropriate.

Persist only canonical form.

---

# 35. Unicode

Planning must explicitly decide how Unicode names/slugs are handled.

Simplest Feature 5 policy:

```text
slug supports ASCII lowercase letters, digits, hyphens
```

Tenant names may still contain Unicode.

Do not build transliteration/i18n slug generation unless required.

Example:

```text
Business Name:
Élite Salon

Slug supplied:
elite-salon
```

is acceptable.

---

# 36. Database Collation

Do not rely on case-insensitive collation to enforce slug semantics.

Persist canonical lowercase slugs and enforce uniqueness over canonical values.

If existing DB uniqueness is case-sensitive, application normalization becomes important.

---

# 37. Slug Length

Choose an explicit length constraint compatible with the existing database column.

Current column may be:

```text
VARCHAR(255)
```

but that does not mean public slugs should be 255 characters long.

Planning should choose a smaller product-level maximum if appropriate.

Do not alter the DB column solely for aesthetic reasons unless needed.

---

# 38. Slug Validation Location

Slug validation should happen before persistence.

Do not let invalid slug syntax reach PostgreSQL as the primary validation mechanism.

The DB remains responsible for uniqueness.

Application layer owns syntax/business validation.

---

# 39. Creation Request Compatibility

Feature 2's request currently resembles:

```json
{
  "name": "Acme Salon",
  "slug": "acme-salon"
}
```

Feature 5 should preserve backwards compatibility unless explicitly approved otherwise.

Do not suddenly remove `slug` from the creation DTO and auto-generate it without an intentional API-version decision.

---

# 40. Public Routing vs Custom Domains

Feature 5 establishes public slug identity.

It does NOT implement:

```text
customer custom domains
tenant-specific DNS
SSL certificates
domain ownership verification
CNAME setup
```

Those are later white-label/domain features.

---

# 41. Public Routing vs Branding

Feature 5 may return:

```text
name
slug
description
```

but must not introduce:

```text
logo
colors
theme
fonts
```

Branding belongs to Feature 10.

---

# 42. Public Routing vs Booking

Feature 5 resolves the business.

It does NOT implement:

```text
service listing
availability
booking creation
customer checkout
appointment confirmation
```

Those belong to later business epics/features.

---

# 43. API Example

Potential future flow:

```text
GET /api/v1/public/tenants/acme-salon
        ↓
{
  "name": "Acme Salon",
  "slug": "acme-salon",
  "description": "..."
}
```

Then later:

```text
GET /api/v1/public/tenants/acme-salon/services
```

may exist in another feature.

Do not build those subroutes now.

---

# 44. Error Contract

Planning must inspect existing error infrastructure.

Likely relevant codes:

```text
TENANT_SLUG_TAKEN
TENANT_NOT_FOUND
VALIDATION_FAILED
INVALID_REQUEST
INTERNAL_ERROR
```

Potential new code:

```text
TENANT_SLUG_INVALID
```

only if justified.

For each error, define:

```text
Code
Scenario
Originating layer
HTTP status
Public/business vs system
Wrapping behavior
```

---

# 45. Public Not Found Error

The public slug endpoint may safely use:

```text
TENANT_NOT_FOUND
→ 404
```

or a dedicated public not-found code if existing architecture requires it.

Do not leak internal DB errors.

---

# 46. Reserved Slug Error

Reserved values may be treated as:

```text
TENANT_SLUG_INVALID
```

rather than creating:

```text
TENANT_SLUG_RESERVED
```

unless the frontend genuinely needs that distinction.

Prefer fewer stable error codes.

---

# 47. Authorization

Public slug lookup should normally require:

```text
Authentication: NO
Tenant Context: NO
Tenant Permission: NO
```

This endpoint resolves the tenant identity itself.

Do not apply private tenant middleware to public slug lookup.

---

# 48. Security Boundary

The public endpoint must remain read-only.

Never allow:

```text
slug
→ mutate tenant
```

without authenticated tenant permissions.

Feature 5 public lookup is discovery only.

---

# 49. Rate Limiting

Do not introduce a new rate-limiting subsystem solely for Feature 5 unless one already exists.

Public endpoints may eventually need abuse controls, but that is infrastructure/security-hardening work.

Note the risk if relevant; do not expand scope unnecessarily.

---

# 50. Caching

Do NOT add Redis or application caching for slug resolution.

A simple indexed/unique PostgreSQL lookup is sufficient for now.

Caching can be added when evidence warrants it.

---

# 51. Indexes

The existing unique slug constraint should already provide an index suitable for:

```text
WHERE slug = $1
```

Planning must verify this.

Do not add redundant slug indexes.

---

# 52. Migration Expectations

Default expectation:

```text
NO NEW MIGRATION
```

because slug persistence and uniqueness already exist.

However, a new migration may be justified if the current database lacks a necessary constraint that Feature 5 makes explicit.

Never modify existing migrations.

Inspect latest migration after F4.

If F4 introduced:

```text
000008
```

then any necessary Feature 5 migration would use the next available number.

Do not assume one is required.

---

# 53. Public DTO & F4 Fields

Feature 4 added tenant profile fields.

Feature 5 planning must decide whether public lookup exposes:

```text
Description
```

and potentially other contact fields.

Preferred conservative default:

```text
Name
Slug
Description
```

Do NOT automatically expose:

```text
ContactEmail
ContactPhone
Timezone
```

until product requirements say they are public.

This prevents accidental disclosure of business data intended only for dashboard use.

---

# 54. Internal UUID Exposure

Planning must explicitly answer whether public slug lookup returns tenant ID.

Preferred conservative approach:

```text
Do not expose internal tenant UUID unless downstream public API design actually needs it.
```

Public booking routes can remain slug-scoped externally and resolve internally.

Do not expose identifiers merely because they exist.

---

# 55. TDD Requirements

Feature 5 must be implemented test-first.

Planning must define tests before implementation.

---

# 56. Slug Domain Tests

At minimum test:

```text
valid lowercase slug accepted
uppercase normalization/rejection according to policy
spaces normalized/rejected
underscores rejected
special characters rejected
leading hyphen rejected
trailing hyphen rejected
double hyphen behavior
too-short slug
too-long slug
reserved slug rejected
```

Use the actual approved rules.

---

# 57. Tenant Creation Integration Tests

Feature 2 regression/Feature 5 integration should prove:

```text
valid canonical slug
→ tenant creation succeeds

invalid slug
→ rejected before persistence

reserved slug
→ rejected

duplicate slug
→ TENANT_SLUG_TAKEN

invalid slug failure
→ no tenant
→ no membership
→ no BUSINESS_OWNER assignment
```

Atomicity must remain intact.

---

# 58. Repository Tests

At minimum:

```text
FindBySlug existing
→ tenant returned

FindBySlug missing
→ TENANT_NOT_FOUND or approved internal equivalent

case/canonical handling
→ consistent behavior

DB failure
→ safely wrapped
```

---

# 59. Public Service Tests

At minimum:

```text
active tenant slug
→ public identity returned

missing slug
→ 404 contract

disabled tenant
→ hidden according to approved public policy

public response excludes private fields
```

---

# 60. Public Handler Tests

At minimum:

```text
valid slug
→ 200

missing slug
→ 404

invalid syntax
→ 400 if validation occurs before lookup

no authentication
→ endpoint still reachable
```

---

# 61. Route Tests

Use the real application routing.

Prove:

```text
public slug endpoint works without authentication
```

and does NOT accidentally inherit:

```text
auth middleware
tenant middleware
tenant permission middleware
```

unless product policy changes.

---

# 62. Security Tests

Explicitly prove the public endpoint does NOT expose:

```text
membership
roles
permissions
private profile fields
internal DB errors
billing
settings
```

Also prove public lookup cannot mutate tenant state.

---

# 63. Disabled Tenant Test

If disabled tenants are hidden publicly:

```text
slug exists
tenant DISABLED
↓
public lookup
→ 404
```

or the approved unavailable response.

Test this explicitly.

---

# 64. Reserved Slug Test

Every reserved slug strategy should have representative tests.

Do not necessarily write a separate test for every string if a table-driven test is cleaner.

---

# 65. Feature 4 Regression

Feature 5 must not break:

```text
PATCH /api/v1/tenants/{tenantID}
```

Name updates must still NOT mutate slug automatically.

This regression is mandatory.

---

# 66. Feature 3 Regression

Private retrieval/listing remains UUID/membership/permission-based.

Public slug lookup must not weaken:

```text
GET /api/v1/tenants/{tenantID}
```

security.

---

# 67. Feature 2 Regression

Tenant onboarding must continue to atomically create:

```text
Tenant
Membership
BUSINESS_OWNER
```

Slug validation should integrate without breaking rollback guarantees.

---

# 68. Strict Non-Goals

Feature 5 MUST NOT implement:

```text
Slug rename history
Redirect aliases
Custom domains
DNS
Branding
Tenant lifecycle changes
Tenant settings
Booking creation
Service management
Availability
Staff scheduling
Customers
Payments
Notifications
Subscriptions
Billing
SEO pages
Public marketing page generation
```

Do not begin Feature 6.

---

# 69. Protect Epic 01

Do not redesign:

```text
Authentication
Membership
Roles
Permissions
Tenant context
Authorization
Error infrastructure
```

Public slug lookup should not require those systems unnecessarily.

---

# 70. Protect Feature 1

Do not break:

```text
Tenant persistence
Slug unique constraint
Tenant model
FindByID
```

---

# 71. Protect Feature 2

Do not break:

```text
POST /api/v1/tenants
atomic provisioning
BUSINESS_OWNER assignment
TENANT_SLUG_TAKEN
```

---

# 72. Protect Feature 3

Do not alter private tenant retrieval/listing security.

Public slug identity is a separate read path.

---

# 73. Protect Feature 4

Do not alter:

```text
PATCH /api/v1/tenants/{tenantID}
tenant.update
profile fields
Name/Slug separation
```

Slug changes must not happen as a side effect of profile changes.

---

# 74. Planning-Agent Instructions

When planning Feature 5:

1. Inspect current tenant creation slug behavior.
2. Inspect unique constraint name.
3. Inspect current slug validation, if any.
4. Inspect API/public route conventions.
5. Inspect tenant profile DTOs.
6. Inspect disabled tenant behavior.
7. Decide canonical slug format.
8. Decide normalization policy.
9. Decide reserved slug policy.
10. Decide whether slug changes are allowed.
11. Decide public lookup route.
12. Decide public DTO.
13. Decide whether UUID is exposed.
14. Decide error contract.
15. Verify whether any migration is needed.
16. Define TDD matrix.
17. Do not implement.

---

# 75. Required Planning Output

Return exactly these sections.

## 1. Current Repository Findings

Inspect:

* Tenant model
* slug migration
* creation DTO
* Feature 2 slug handling
* TENANT_SLUG_TAKEN
* TenantRepository
* Feature 4 tenant profile fields
* route conventions
* public route conventions
* disabled tenant behavior
* error infrastructure
* test infrastructure

Separate facts from recommendations.

---

## 2. Feature 5 Scope

Explicitly state:

```text
Slug Validation: YES/NO
Slug Normalization: YES/NO
Reserved Slugs: YES/NO
FindBySlug: YES/NO
Public Lookup Endpoint: YES/NO
Slug Mutation: YES/NO
```

Explain each.

---

## 3. Canonical Slug Contract

Specify:

```text
Allowed characters
Minimum length
Maximum length
Case handling
Whitespace handling
Hyphen rules
Unicode policy
```

---

## 4. Normalization Strategy

Explain exactly how input becomes canonical form.

Give examples.

---

## 5. Reserved Slug Strategy

List categories of reserved values and the source of truth.

Do not overbuild.

---

## 6. Tenant Creation Integration

Explain how Feature 5 changes or strengthens existing Feature 2 creation slug handling.

Preserve atomicity.

---

## 7. Slug Mutation Decision

State:

```text
Existing tenant slug editable?
YES / NO
```

Explain consequences and rationale.

---

## 8. Public Lookup API

Specify:

```text
Method
Path
Authentication
Tenant Context
Permission
Input
Success response
Failure responses
```

---

## 9. Public DTO

List exact fields exposed publicly.

Explicitly state whether internal TenantID is returned.

---

## 10. Repository Design

List minimum additions such as `FindBySlug`.

For each:

```text
Purpose
Input
Output
Errors
Filtering
```

---

## 11. Service Design

Describe slug validation/public identity service responsibilities.

Avoid duplicate logic.

---

## 12. Error Contract

For each failure:

```text
CODE
Scenario
Layer
HTTP Status
Business/System
Wrapping
```

---

## 13. Security Requirements

Address:

```text
public vs private tenant identity
disabled tenant
private field exposure
slug enumeration
UUID exposure
cross-tenant security
SQL injection
reserved route conflicts
```

---

## 14. TDD Test Matrix

Separate:

```text
domain
Feature 2 integration
repository
service
handler
route
security
regression
```

Use:

```text
Test
Layer
Scenario
Expected Result
```

---

## 15. Files Expected To Change

Use actual repository paths.

Separate:

```text
NEW
MODIFY
UNCHANGED
```

---

## 16. Migration Assessment

State whether a migration is required.

Default expectation:

```text
NO
```

Explain why.

---

## 17. Implementation Order

Give a TDD-first sequence.

---

## 18. Risks / Architectural Concerns

Address:

```text
breaking existing links
route collisions
case-sensitive duplicates
race conditions
slug over-normalization
public data leakage
disabled tenant exposure
Feature 2 regression
Feature 4 Name/Slug coupling
scope creep into custom domains/booking
```

---

## 19. Acceptance Criteria

Every criterion must be objectively testable.

---

## 20. Explicit Non-Changes

Protect:

```text
Epic 01
F1
F2 atomicity
F3 private retrieval
F4 profile update
role catalog
permission catalog
migrations history
future F6+
```

---

# 76. Definition of Done

Feature 5 is complete when the platform has a stable, validated public tenant identity.

At minimum prove:

```text
Valid slug accepted

Invalid slug rejected

Reserved slug rejected

Duplicate slug → TENANT_SLUG_TAKEN

Tenant creation remains atomic

Public tenant can be resolved by slug

Missing public slug → 404

Disabled tenant not publicly exposed

Public response exposes only approved fields

No authentication required for public identity lookup

Private UUID retrieval security remains unchanged

Name change does not change slug

No old migration modified
```

---

# 77. Context Restoration

If this file is used after context loss:

```text
Epic 01     COMPLETE
Epic 02 F1  COMPLETE
Epic 02 F2  COMPLETE
Epic 02 F3  COMPLETE
Epic 02 F4  COMPLETE
Epic 02 F5  CURRENT
```

Do not reimplement completed features.

Inspect repository state before planning F5.

---

# FINAL RULE

Feature 5 is about:

> **stable public tenant identity**

It is NOT about:

```text
Branding
Custom domains
Bookings
SEO
Lifecycle
Settings
```

The slug should become a clean, safe, canonical public identifier without weakening internal tenant security or coupling tenant name changes to URL identity.

Do not begin Feature 6.
