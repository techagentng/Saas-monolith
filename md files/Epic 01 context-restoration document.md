# EPIC 02 — Tenant Management

## Agent-Ready Master Context Specification

This document is the authoritative context-preservation specification for:

> **EPIC 02 — Tenant Management**

It should be provided to any coding or planning agent that needs to work on Epic 02 without relying on previous chat history.

This specification does **not** itself authorize implementation of every feature at once.

Epic 02 must be delivered **feature by feature**, with each feature separately planned, reviewed, implemented, tested, and accepted.

---

# 1. Project Architecture

The backend is a Go modular monolith.

The primary application flow is:

```text
API
↓
Handler
↓
Service
↓
Repository
↓
Database
```

A module is an organizational boundary around related functionality.

A module is **not** another architectural layer.

The codebase should continue following:

* idiomatic Go
* SOLID principles
* explicit dependency boundaries
* handler/service/repository separation
* PostgreSQL persistence
* strong automated testing
* test-driven development where practical
* structured error handling
* secure multi-tenant design
* minimal abstractions
* compatibility with future modular extraction if microservices are ever required

Do not introduce architectural patterns merely because they may theoretically be useful later.

---

# 2. Existing Epic 01 Foundation

Epic 01 — Identity & Access is considered implemented and working.

Epic 02 must build on Epic 01 rather than duplicate it.

Epic 01 provides or may provide foundations around:

* users
* authentication
* roles
* permissions
* authorization
* tenant membership concepts
* BUSINESS_OWNER role
* STAFF role
* SUPER_ADMIN role
* permission enforcement
* audit infrastructure
* structured application errors
* middleware
* migrations
* repositories
* service conventions
* test conventions

Every agent working on Epic 02 must inspect the repository before assuming exact implementation details.

Existing Epic 01 code is authoritative.

Do not redesign Epic 01 as part of Epic 02.

---

# 3. Epic 02 Goal

Epic 02 introduces the business tenant as a first-class domain concept.

A **tenant** represents an independent business using the SaaS platform.

Examples:

```text
Platform
│
├── Tenant A — Salon
├── Tenant B — Barbershop
├── Tenant C — Photography Studio
└── Tenant D — Consulting Business
```

Each tenant will ultimately own or scope business data such as:

```text
Tenant
│
├── Staff
├── Services
├── Customers
├── Bookings
├── Availability
├── Notifications
├── Payments
├── Branding
├── Settings
└── Reports
```

Epic 02 must establish the tenant boundaries required for those later modules.

---

# 4. Core Architectural Principle

Epic 01 answers:

```text
Who is this user?
What are they allowed to do?
```

Epic 02 answers:

```text
Which tenant is this operation occurring within?
```

Later features will combine these questions:

```text
Request
↓
Authentication
↓
Tenant Resolution
↓
Membership Validation
↓
Authorization
↓
Handler
↓
Service
↓
Repository
↓
Database
```

The eventual security model must prevent a user authorized in one tenant from accessing another tenant's data.

---

# 5. Tenant Domain Concept

The core Tenant entity is expected to contain approximately:

```text
Tenant
├── ID
├── Name
├── Slug
├── Status
├── CreatedAt
└── UpdatedAt
```

Initial tenant lifecycle states are expected to include:

```text
ACTIVE
SUSPENDED
INACTIVE
```

Exact types, package structure, UUID implementation, timestamps, and persistence representation must follow existing repository conventions where possible.

Do not invent competing infrastructure if the project already has established patterns.

---

# 6. Epic 02 Feature Breakdown

Epic 02 is divided into the following implementation features.

## Feature 1 — Tenant Core Model & Persistence

Establish the tenant domain and persistence foundation.

Includes:

* Tenant domain model
* tenant status representation
* tenant database schema
* migration
* repository contract
* persistence implementation
* persistence-focused tests
* required tenant-domain error definitions

Does not include higher-level onboarding workflows.

---

## Feature 2 — Tenant Creation & Owner Provisioning

Implement tenant onboarding.

Expected flow:

```text
Authenticated User
↓
Create Tenant Request
↓
Validate Input
↓
Create Tenant
↓
Create/Associate Membership
↓
Assign BUSINESS_OWNER
↓
Commit Transaction
```

This feature must reuse Epic 01 membership and role infrastructure.

Tenant creation and owner provisioning should be atomic.

---

## Feature 3 — Tenant Retrieval & Listing

Implement controlled access to tenant information.

Potential operations include:

```text
Get Tenant
Get My Tenants
Get Tenant By ID
Platform Tenant Listing
```

Visibility must respect authentication, membership, roles, permissions, and platform-level access.

---

## Feature 4 — Tenant Profile Management

Allow authorized users to modify tenant profile information.

Potential profile fields include:

```text
Business Name
Business Description
Contact Email
Contact Phone
Timezone
Country
```

Do not mix every possible setting into the tenant table.

Business configuration belongs in appropriate settings boundaries.

---

## Feature 5 — Tenant Slug & Public Identity

Introduce stable public tenant identifiers.

Example:

```text
tracebook.com/book/acme-salon
```

The slug:

```text
acme-salon
```

identifies the tenant publicly.

Responsibilities may include:

* slug generation
* validation
* normalization
* uniqueness
* reserved slugs
* lookup by slug
* public tenant resolution

Potential structured errors include:

```text
TENANT_SLUG_TAKEN
TENANT_SLUG_INVALID
TENANT_NOT_FOUND
```

---

## Feature 6 — Tenant Context Resolution

Introduce reliable tenant context for authenticated requests.

Conceptually:

```text
HTTP Request
↓
Authentication
↓
Resolve Tenant
↓
Validate Membership
↓
Resolve Permissions
↓
Tenant Context
↓
Handler
```

Tenant context may eventually contain concepts such as:

```text
TenantID
UserID
Role
Permissions
```

Exact implementation must follow existing middleware and context conventions.

Never blindly trust client-provided tenant identifiers.

---

## Feature 7 — Tenant Isolation Enforcement

Explicitly enforce and test tenant data isolation.

Core rule:

```text
Tenant A
must not access
Tenant B resources
```

Tenant-owned repository queries should eventually scope by tenant.

Example principle:

```sql
SELECT *
FROM bookings
WHERE id = $1
AND tenant_id = $2;
```

instead of relying only on resource IDs.

This feature should contain significant integration and security testing.

---

## Feature 8 — Tenant Lifecycle Management

Implement controlled tenant-state transitions.

Examples:

```text
ACTIVE
↓
SUSPENDED
↓
ACTIVE
```

and potentially:

```text
INACTIVE
```

Suspension should generally retain tenant data while preventing normal tenant operations.

Tenant deletion must not be casually introduced.

Potential errors include:

```text
TENANT_SUSPENDED
TENANT_INACTIVE
TENANT_STATUS_INVALID
```

---

## Feature 9 — Tenant Settings Foundation

Introduce an explicit tenant-configuration boundary.

Potential areas include:

```text
timezone
currency
locale
booking configuration
notification preferences
cancellation configuration
```

The objective is not to implement every future setting.

The objective is to prevent tenant configuration from becoming scattered throughout unrelated modules.

---

## Feature 10 — Tenant Branding Foundation

Establish tenant-specific branding support.

Potential configuration includes:

```text
Tenant Branding
├── Logo
├── Business Name
├── Primary Colour
├── Secondary Colour
└── Theme Configuration
```

The frontend should eventually be able to:

```text
Resolve Tenant
↓
Load Branding
↓
Render Tenant Experience
```

Full white-label/custom-domain behaviour can be developed separately.

---

## Feature 11 — Tenant Audit & Administrative Events

Integrate tenant operations with the existing audit infrastructure.

Potential events include:

```text
TENANT_CREATED
TENANT_UPDATED
TENANT_SUSPENDED
TENANT_REACTIVATED
TENANT_SETTINGS_UPDATED
TENANT_BRANDING_UPDATED
```

Audit records should answer:

```text
Who?
What?
Which tenant?
When?
```

Do not introduce a second audit framework.

---

## Feature 12 — Tenant Error Contract & Security Hardening

Consolidate tenant-specific failure contracts and security guarantees.

Potential error codes may include:

```text
TENANT_NOT_FOUND
TENANT_ALREADY_EXISTS
TENANT_SLUG_TAKEN
TENANT_SLUG_INVALID
TENANT_ACCESS_DENIED
TENANT_SUSPENDED
TENANT_INACTIVE
TENANT_MEMBERSHIP_REQUIRED
TENANT_STATUS_INVALID
```

Only define errors when actual features require them.

Avoid speculative error catalogs.

---

# 7. Error Management Standard

The project uses structured application errors.

Go errors must not be treated merely as user-facing strings.

Stable string error codes should represent expected business failures.

Example concept:

```go
type ErrorCode string
```

Examples:

```text
TENANT_NOT_FOUND
TENANT_SUSPENDED
TENANT_SLUG_TAKEN
```

Application code must not branch on:

```go
err.Error()
```

Expected structured errors should retain their identity while being wrapped through:

```text
Repository
↓
Service
↓
Handler
```

Use existing Go error conventions such as:

```text
errors.Is
errors.As
%w wrapping
```

where consistent with the existing implementation.

The stable code represents machine-readable identity.

The message represents presentation.

HTTP status codes describe transport semantics and must not replace domain error codes.

Unexpected system failures must never expose:

* SQL statements
* credentials
* connection strings
* stack traces
* internal database messages
* infrastructure details

They should be mapped safely at the API boundary.

This follows the project's established error-management approach from the provided error-management specification.

---

# 8. Multi-Tenant Security Principles

Every Epic 02 feature must preserve the following principles.

## 8.1 Tenant IDs Are Security Boundaries

Tenant IDs are not merely organizational metadata.

They participate in authorization and data isolation.

---

## 8.2 Never Trust Tenant IDs From the Client

A request containing:

```text
tenant_id = X
```

does not mean the authenticated user may access Tenant X.

Tenant membership and authorization must be verified independently.

---

## 8.3 Repository Scoping

Tenant-owned resources should eventually be queried using tenant-aware predicates.

Avoid repository methods that make cross-tenant access easy by accident.

---

## 8.4 Deny by Default

If tenant context, membership, authorization, or lifecycle status cannot be verified, access should fail safely.

---

## 8.5 Suspended Membership Is Not Active Membership

Membership status must continue to follow Epic 01 semantics.

Epic 02 must not create alternative membership logic.

---

## 8.6 SUPER_ADMIN Does Not Eliminate Tenant Boundaries

Platform-level privileges must remain explicit.

Do not make every query globally accessible simply because SUPER_ADMIN exists.

---

# 9. Database Design Principles

Tenant persistence must follow existing PostgreSQL and migration conventions.

General expectations include:

* stable primary keys
* appropriate foreign keys
* unique constraints where required
* database constraints for important invariants
* safe NOT NULL decisions
* explicit lifecycle status constraints where appropriate
* timestamps
* predictable rollback migrations
* tenant-aware indexes when justified

Do not introduce unnecessary indexes without an expected access pattern.

Do not use application validation as the only protection for critical uniqueness/integrity rules that PostgreSQL can safely enforce.

---

# 10. Repository Design Principles

Repositories should expose behaviour required by current use cases.

Avoid speculative generic CRUD interfaces.

Do not automatically create:

```text
Create
Read
Update
Delete
List
Search
Count
Exists
BulkCreate
```

for every entity.

Each repository method must be justified by a current feature.

Repository interfaces should remain narrow enough to support dependency inversion without becoming generic frameworks.

Avoid:

```text
BaseRepository
GenericRepository
UniversalCRUDRepository
```

unless the project already has a compelling established convention.

---

# 11. Service Layer Principles

Business operations belong in services rather than handlers or repositories.

Examples of service responsibilities may include:

* validation
* state transitions
* orchestration
* membership checks
* authorization coordination
* transaction boundaries
* business rules
* error mapping

Repositories should focus on persistence.

Handlers should focus on transport.

---

# 12. Handler Principles

Handlers should remain thin.

Typical responsibilities:

```text
Parse Request
↓
Validate Transport-Level Input
↓
Call Service
↓
Map Result/Error
↓
Return API Response
```

Do not embed significant tenant business logic in handlers.

---

# 13. Testing Strategy

Epic 02 follows strong TDD practices.

Each feature plan should define tests before implementation.

Testing may include:

```text
Domain Unit Tests
Service Unit Tests
Repository Tests
Database Integration Tests
HTTP/Handler Tests
Authorization Tests
Tenant-Isolation Tests
Regression Tests
```

Only use levels relevant to the feature being implemented.

Do not write meaningless tests purely for coverage.

Tests should prove contracts and business/security behaviour.

---

# 14. Critical Security Test Matrix

As tenant-aware functionality grows, testing must eventually prove scenarios such as:

```text
Tenant A Owner → Tenant A Resource
ALLOWED

Tenant A Owner → Tenant B Resource
DENIED

Tenant A Staff → Tenant B Resource
DENIED

Revoked Membership → Tenant Resource
DENIED

Suspended Membership → Tenant Resource
DENIED

Unauthorized User → Tenant Resource
DENIED

Appropriately Authorized SUPER_ADMIN → Platform Operation
ALLOWED
```

Not every test belongs in Feature 1.

Each should be introduced when the corresponding behaviour exists.

---

# 15. Migration Safety

Never rewrite old migrations merely to make new development convenient.

Once a migration has become part of the working project history, new schema changes should generally use new migrations.

Every migration should consider:

```text
UP
and
DOWN
```

behaviour.

Avoid destructive migration changes without explicit justification.

---

# 16. Backward Compatibility

Epic 02 must not break Epic 01.

Agents must preserve:

* login behaviour
* authentication contracts
* token behaviour
* role catalog
* permission codes
* authorization semantics
* membership semantics
* seeded roles
* seeded permissions
* audit behaviour
* error infrastructure
* existing API contracts
* existing migration history
* existing tests

If modifying Epic 01 code is truly necessary, it must be explicitly identified in the feature plan before implementation.

---

# 17. Shared Non-Goals

Unless a specific feature explicitly requires them, Epic 02 must not introduce:

* microservices
* Kafka
* RabbitMQ
* distributed events
* Redis caching
* tenant sharding
* tenant-per-database architecture
* generic DDD framework
* generic repository frameworks
* generic service frameworks
* speculative domain events
* CQRS
* event sourcing
* payment processing
* subscriptions
* billing
* customer booking workflows
* scheduling engines
* staff availability engines

The system remains a modular monolith.

---

# 18. Implementation Workflow

Every Epic 02 feature follows this workflow.

```text
1. Provide Epic 02 master context to Haiku

2. Ask Haiku to inspect the repository and PLAN the feature

3. Haiku must not implement

4. Bring Haiku's plan back for architectural review

5. Review the plan for:
   - scope correctness
   - architecture
   - security
   - tenant isolation
   - TDD quality
   - error handling
   - migration safety
   - compatibility with Epic 01
   - unnecessary abstractions

6. Correct/finalize the plan

7. Give the approved plan to Sonnet

8. Sonnet implements only the approved feature

9. Run focused tests

10. Run relevant regression tests

11. Review implementation

12. Accept feature

13. Move to next feature
```

Do not allow an implementation agent to silently expand the feature boundary.

---

# 19. Planning-Agent Rules

When Haiku is acting as the planning agent:

Haiku must:

* inspect the repository
* report actual existing patterns
* distinguish repository facts from recommendations
* identify exact files likely to change
* define tests first
* propose minimal implementation
* identify security risks
* identify dependencies
* identify explicit non-changes

Haiku must NOT:

* implement code
* modify files
* generate patches
* redesign unrelated modules
* create speculative abstractions

---

# 20. Implementation-Agent Rules

When Sonnet is acting as the implementation agent:

Sonnet must:

* follow the approved plan
* inspect files before modifying them
* preserve user-edited code
* make the smallest required production changes
* write tests as specified
* respect module boundaries
* reuse existing infrastructure
* run focused tests
* report any deviation from the approved plan

Sonnet must NOT:

* expand feature scope
* refactor unrelated modules
* replace existing architecture
* alter role/permission catalogs without approval
* bypass tests
* silently modify migration history
* invent new infrastructure where existing infrastructure works

---

# 21. Feature Delivery Order

Unless an architectural discovery requires adjustment, Epic 02 should proceed in this sequence:

```text
Feature 1
Tenant Core Model & Persistence

↓

Feature 2
Tenant Creation & Owner Provisioning

↓

Feature 3
Tenant Retrieval & Listing

↓

Feature 4
Tenant Profile Management

↓

Feature 5
Tenant Slug & Public Identity

↓

Feature 6
Tenant Context Resolution

↓

Feature 7
Tenant Isolation Enforcement

↓

Feature 8
Tenant Lifecycle Management

↓

Feature 9
Tenant Settings Foundation

↓

Feature 10
Tenant Branding Foundation

↓

Feature 11
Tenant Audit & Administrative Events

↓

Feature 12
Tenant Error Contract & Security Hardening
```

Each feature must be independently reviewable and testable.

---

# 22. Definition of Done for Epic 02

Epic 02 is complete only when the platform has a secure, tested tenant foundation supporting:

* tenant persistence
* tenant creation
* tenant ownership
* controlled tenant retrieval
* tenant profile management
* public tenant identity
* tenant context
* tenant isolation
* lifecycle controls
* tenant settings foundation
* tenant branding foundation
* tenant auditing
* structured tenant errors
* regression compatibility with Epic 01

Most importantly:

> A user authenticated and authorized within Tenant A must not gain access to Tenant B merely by changing an identifier in a request.

That security invariant should influence every later tenant-aware module.

---

# 23. Context Restoration Instruction

If this document is being used after context loss, treat it as the authoritative high-level specification for Epic 02.

Then:

1. Inspect the actual repository.
2. Determine which Epic 02 features are already complete.
3. Do not reimplement completed work.
4. Preserve Epic 01 behaviour.
5. Continue from the next incomplete feature.
6. Use the established planning → review → implementation → testing workflow.
7. Prefer the current repository state over assumptions in this document where implementation details differ.
8. Report meaningful conflicts before changing architecture.
