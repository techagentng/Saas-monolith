# Multi-Tenant Booking System — Master Project Specification

**File:** `00-MASTER-PROJECT-SPECIFICATION.md`  
**Status:** Authoritative project context  
**Architecture:** Modular monolith  
**Backend:** Go  
**Database:** PostgreSQL  
**Frontend:** Next.js + TypeScript  
**Development Method:** Pure TDD / Test-First Development

---

## 1. Purpose

This document is the master context and architectural source of truth for the Multi-Tenant Booking System.

It exists so that a developer or coding agent can reconstruct the project's intent, architecture, security model, engineering rules, and delivery methodology without relying on previous chat history.

Feature and Epic specifications may provide more detailed rules, but they must remain consistent with this document.

If a lower-level specification conflicts with this document, the conflict must be identified before implementation rather than silently resolved by the agent.

---

## 2. Product Overview

The system is a multi-tenant booking platform.

A tenant represents an independent business operating on the platform. Each tenant can have its own users, roles, permissions, booking-related resources, branding, notifications, and other business data.

The platform must provide strong isolation between tenants while allowing the platform's super administrators to manage the system globally.

The system is intended to grow feature-by-feature into a complete booking platform without prematurely introducing distributed-system complexity.

---

## 3. Core Architectural Decision

The backend is a **modular monolith**.

A module is an organizational boundary that groups related business capabilities. A module is **not** an additional architectural layer.

The request flow is:

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

This separation must remain consistent throughout the project.

### Handler

Responsible for HTTP/API concerns:

- Parse requests
- Validate transport-level input
- Obtain trusted request/authentication context
- Call services
- Map application errors to HTTP responses

Handlers must not contain core business rules.

### Service

Responsible for:

- Business rules
- Domain validation
- Authorization decisions
- Orchestration
- Use-case execution

Services must not contain SQL.

### Repository

Responsible for:

- Persistence
- SQL
- Database queries
- Transactions
- Mapping persistence results

Repositories must not contain HTTP concerns or unrelated business rules.

### Database

PostgreSQL is the persistence layer.

Database constraints may enforce data integrity, but business workflows belong in services.

---

## 4. Module Principle

Modules organize related capabilities.

Examples include:

```text
identity
authorization
tenant
booking
branding
notification
payment
audit
```

A module may contain:

```text
handler/
service/
repository/
model/
...
```

The architecture is still:

```text
Module
 └── Handler → Service → Repository → Database
```

Do not create:

```text
Module → Handler → Service → Repository
```

as though "module" were another runtime layer.

---

## 5. Dependency Inversion

Services should depend on interfaces where that provides real testing and architectural value.

For example:

```go
type UserRepository interface {
    Create(...)
    FindByID(...)
    FindByEmail(...)
}
```

The PostgreSQL implementation satisfies the interface.

This supports:

- Unit testing
- Mocking/fakes
- Separation of business logic from persistence
- Future infrastructure changes
- Potential future service extraction

Do not introduce abstractions merely for abstraction's sake.

---

## 6. Multi-Tenancy

Multi-tenancy is a fundamental security boundary.

A user's identity and tenant access are separate concepts.

```text
User
 ├── Roles
 ├── Permissions
 └── Tenant Memberships
       └── Tenant
```

The system must never assume:

```text
authenticated user = authorized tenant user
```

Authentication answers:

> Who are you?

Authorization answers:

> What are you allowed to do?

Tenant authorization answers:

> Which tenant are you allowed to operate within?

These checks must remain distinct.

---

## 7. Tenant Isolation

Every tenant-scoped operation must establish:

1. Who is making the request?
2. Which tenant context applies?
3. Is the user a member of that tenant?
4. Does the user have the required permission?
5. Does the target resource belong to that tenant?

Conceptually:

```text
Request
 ↓
Authenticate
 ↓
Resolve Tenant Context
 ↓
Verify Membership
 ↓
Check Permission
 ↓
Check Resource Tenant
 ↓
Execute Operation
```

Never rely solely on:

```sql
WHERE id = $1
```

for tenant-owned resources.

Prefer tenant-bounded queries such as:

```sql
SELECT *
FROM bookings
WHERE id = $1
AND tenant_id = $2;
```

A client-supplied tenant ID must never automatically grant tenant access.

---

## 8. Object-Level Authorization / IDOR Protection

Knowing a resource ID must not be sufficient to access a resource.

For example:

```text
GET /bookings/123
```

must verify:

```text
booking 123 belongs to tenant X
AND
authenticated user can access tenant X
AND
user has booking.read permission
```

These checks happen server-side.

Frontend visibility is not authorization.

---

## 9. Roles and Permissions

Initial conceptual roles include:

```text
SUPER_ADMIN
BUSINESS_OWNER
STAFF
CUSTOMER
```

Roles and permissions are backend authorization concepts, not merely frontend UI concepts.

The system must support:

- Role management
- Role assignment
- Permission definitions
- Permission assignment to roles
- Effective-permission resolution
- Permission enforcement

A frontend may hide an unavailable button, but the backend must still reject an unauthorized request.

---

## 10. Error Management Standard

The project uses structured application error codes.

Business logic must never depend on:

```go
if err.Error() == "user already exists" {
    ...
}
```

Use stable, machine-readable string codes.

Examples:

```text
USER_NOT_FOUND
USER_ALREADY_EXISTS
INVALID_CREDENTIALS
ACCOUNT_DISABLED
TENANT_NOT_FOUND
TENANT_ACCESS_DENIED
ROLE_NOT_FOUND
PERMISSION_DENIED
VALIDATION_FAILED
RATE_LIMITED
SERVICE_UNAVAILABLE
INTERNAL_ERROR
```

Do not use arbitrary numeric application codes such as:

```text
1001
1002
1003
```

### Application Error Concept

```go
type ErrorCode string

type AppError struct {
    Code    ErrorCode
    Message string
    Err     error
}
```

The exact implementation may evolve, but these principles are mandatory:

- Code = stable machine-readable identity
- Message = presentation
- Err = optional underlying cause
- Business logic reasons about application error identity
- API handlers decide HTTP representation

---

## 11. Error Wrapping

Errors must preserve their identity through:

```text
Repository
 ↓
Service
 ↓
Handler
 ↓
HTTP response
```

Use Go error wrapping:

```go
fmt.Errorf("creating user: %w", err)
```

Use:

```go
errors.As(...)
```

to recover application-error identity.

Do not parse error strings.

---

## 12. HTTP Status vs Application Error Code

These have different responsibilities.

```text
HTTP status     = transport semantics
Error code      = application semantics
Message         = presentation
```

Typical mapping:

| Application Code | HTTP |
|---|---:|
| VALIDATION_FAILED | 400 |
| INVALID_REQUEST | 400 |
| INVALID_CREDENTIALS | 401 |
| SESSION_EXPIRED | 401 |
| SESSION_REVOKED | 401 |
| PERMISSION_DENIED | 403 |
| TENANT_ACCESS_DENIED | 403 |
| RESOURCE_NOT_FOUND | 404 |
| USER_NOT_FOUND | 404 |
| TENANT_NOT_FOUND | 404 |
| USER_ALREADY_EXISTS | 409 |
| ROLE_ALREADY_EXISTS | 409 |
| RATE_LIMITED | 429 |
| INTERNAL_ERROR | 500 |
| SERVICE_UNAVAILABLE | 503 |

Mapping should be centralized rather than duplicated inconsistently across handlers.

---

## 13. Unexpected Errors

Expected business failures should use stable application error codes.

Unexpected infrastructure/system failures include:

- Database unavailable
- Network failures
- Unexpected nil pointer
- Filesystem failures
- Unknown infrastructure failures

Do not expose raw infrastructure errors, stack traces, SQL details, or secrets to clients.

Map unexpected failures to safe generic API errors such as:

```text
SERVICE_UNAVAILABLE
INTERNAL_ERROR
```

as appropriate.

---

## 14. Authentication Security

Passwords must never be stored as:

- Plaintext
- Encrypted reversible values
- MD5 hashes
- SHA-256 hashes used directly for password storage

Use a well-established password hashing algorithm/library designed for password storage.

Requirements include:

- Reject invalid/empty credentials
- Never return password hashes through APIs
- Never log passwords
- Never put passwords in error messages
- Never put passwords in audit records
- Avoid account enumeration

Authentication failures should use appropriately generic responses.

Avoid responses such as:

```text
Email exists but password is incorrect.
```

Prefer:

```text
Invalid credentials.
```

---

## 15. Sessions / Tokens

The selected authentication mechanism must deliberately document:

- Token/session type
- Lifetime
- Refresh strategy
- Revocation strategy
- Storage strategy
- Logout behavior
- Rotation behavior

Do not casually create long-lived JWTs containing unnecessary sensitive data.

Never place these inside tokens:

- Passwords
- Password hashes
- Secrets
- Sensitive personal data

Refresh tokens must be treated as sensitive credentials.

---

## 16. Authentication Context

After authentication, downstream code must receive trusted identity context.

Conceptually:

```text
Request
 ↓
Authentication Middleware
 ↓
Authenticated Principal
 ↓
Handler
 ↓
Service
```

The principal may contain:

- user_id
- authenticated state
- tenant context
- roles
- permissions

However, tenant context must be resolved/validated against actual membership and authorization.

---

## 17. Security Principles

The system must:

- Never trust frontend authorization
- Never trust an unverified client tenant ID
- Never store plaintext passwords
- Never expose secrets
- Never expose internal errors
- Prevent cross-tenant access
- Protect object-level authorization
- Rate-limit security-sensitive endpoints where appropriate
- Avoid account enumeration
- Audit security-sensitive actions
- Validate authorization server-side

Security must be considered feature-by-feature rather than added at the end.

---

## 18. Auditability

Security-sensitive operations should be auditable.

Examples:

```text
USER_CREATED
USER_LOGIN_SUCCESS
USER_LOGIN_FAILED
USER_LOGOUT
ROLE_ASSIGNED
ROLE_REMOVED
PERMISSION_GRANTED
PERMISSION_REVOKED
TENANT_MEMBERSHIP_CREATED
TENANT_MEMBERSHIP_REMOVED
```

Audit records must not contain secrets or passwords.

---

## 19. Observability

Useful request/log dimensions include:

```text
request_id
tenant_id
user_id
operation
error_code
duration
status
```

Use caution with metric cardinality.

Stable error codes are appropriate low-cardinality metric dimensions.

High-cardinality identifiers such as arbitrary user IDs should generally belong in logs/traces rather than Prometheus labels.

Request IDs should support tracing across the request lifecycle.

---

## 20. Frontend Architecture

The frontend uses:

```text
Next.js
TypeScript
```

The frontend must consume backend authorization information and stable application error codes.

Frontend role/permission visibility is a UX concern.

Backend authorization remains the security authority.

The frontend must not be treated as a trusted security boundary.

---

## 21. Development Method — Pure TDD

Backend development is test-first.

For every feature:

```text
1. Define expected behavior
2. Write failing tests
3. Implement minimum required code
4. Make tests pass
5. Refactor while keeping tests green
6. Add integration/security tests where appropriate
```

Do not generate an entire Epic in one implementation step.

---

## 22. Feature Delivery Model

Each feature should progress through:

```text
Feature Specification
        ↓
Backend API Contract
        ↓
Database Changes
        ↓
Backend TDD
        ↓
Integration Tests
        ↓
Frontend Contract
        ↓
Frontend Implementation
        ↓
End-to-End Verification
```

Do not move to the next feature while the current feature's tests are failing.

---

## 23. Definition of Done

A feature is complete only when applicable items are satisfied:

- [ ] Business behavior documented
- [ ] API contract documented
- [ ] Database requirements documented
- [ ] Unit tests written
- [ ] Tests initially fail where expected
- [ ] Implementation completed
- [ ] Unit tests pass
- [ ] Repository tests pass
- [ ] Handler/API tests pass
- [ ] Security cases tested
- [ ] Error codes tested
- [ ] Tenant isolation tested where relevant
- [ ] No secrets exposed
- [ ] No sensitive information logged
- [ ] Error responses standardized
- [ ] Authorization enforced server-side
- [ ] Handler → Service → Repository separation preserved
- [ ] No business logic in handlers
- [ ] No SQL in services/handlers
- [ ] Documentation updated

---

## 24. Agent Rules

Coding agents must follow these rules:

1. Do not rewrite the architecture without explicit approval.
2. Do not introduce microservices prematurely.
3. Do not create unnecessary abstractions.
4. Do not put business logic in handlers.
5. Do not put HTTP concerns in repositories.
6. Do not put SQL in services.
7. Do not trust frontend authorization.
8. Do not trust client-provided tenant IDs without authorization checks.
9. Do not parse `err.Error()` for business logic.
10. Do not expose internal errors to clients.
11. Do not store plaintext passwords.
12. Do not log secrets.
13. Do not implement an entire Epic as one giant change.
14. Write tests before implementation.
15. Do not silently invent undefined business rules.

If a requirement is ambiguous, stop and identify the ambiguity.

---

## 25. Project Delivery Philosophy

The project should be built incrementally.

The preferred pattern is:

```text
Master Specification
        ↓
Epic Specification
        ↓
Feature Specification
        ↓
Agent Implementation Prompt
        ↓
TDD Implementation
        ↓
Verification
        ↓
Review
        ↓
Next Feature
```

A strong reasoning model may be used to clarify architecture, generate implementation specifications, and review agent output.

A coding agent may then implement the well-defined task.

The coding agent should not be required to rediscover the architecture from scratch.

---

## 26. Future Extensibility

The modular monolith should be designed so that modules can potentially be extracted into services later.

However, the current system must NOT prematurely introduce:

- Message brokers
- Service discovery
- Distributed transactions
- Multiple independent deployments
- Other distributed-system complexity

unless a later requirement explicitly requires them.

The immediate objective is a clean, testable, secure modular monolith.

---

## 27. Conceptual Backend Structure

The structure should emerge feature-by-feature, but conceptually:

```text
backend/
├── cmd/
│   └── api/
├── internal/
│   ├── identity/
│   │   ├── handler/
│   │   ├── service/
│   │   ├── repository/
│   │   └── model/
│   ├── authorization/
│   │   ├── service/
│   │   └── model/
│   ├── tenant/
│   │   ├── handler/
│   │   ├── service/
│   │   ├── repository/
│   │   └── model/
│   ├── audit/
│   │   ├── service/
│   │   ├── repository/
│   │   └── model/
│   └── errors/
│       ├── codes.go
│       └── error.go
├── migrations/
└── ...
```

This is a conceptual target, not an instruction to create every directory immediately.

---

## 28. Epic Roadmap

The exact Epic ordering may evolve as requirements are refined, but the overall project should be decomposed into independently deliverable capabilities.

Epic 1 is:

```text
Identity & Access
```

It establishes the security foundation required by future modules such as:

```text
Tenant Management
Booking
Staff
Customer
Payments
Notifications
Reporting
Branding
```

Future Epics must consume the established identity, tenant, authorization, error, audit, and security infrastructure rather than reinventing them.

---

## 29. Context Restoration

If previous conversation context is unavailable, this document is the starting point.

The implementation agent should understand:

```text
PROJECT:
Multi-Tenant Booking System

STYLE:
Modular monolith

BACKEND:
Go

DATABASE:
PostgreSQL

FRONTEND:
Next.js + TypeScript

ARCHITECTURE:
API → Handler → Service → Repository → Database

DEVELOPMENT:
Pure TDD

SECURITY:
Server-side authorization
Tenant isolation
Object-level authorization
Secure credential handling

ERROR MANAGEMENT:
Stable string error codes
errors.As
%w wrapping
Centralized HTTP mapping

DELIVERY:
Epic → Feature → TDD → Integration → E2E
```

The agent must not invent missing architecture or business rules merely because previous chat context is unavailable.

---

## 30. Master Principle

The system must remain:

**Secure, tenant-isolated, testable, modular, maintainable, and understandable.**

The architecture should optimize for correctness and clear boundaries before premature complexity.

