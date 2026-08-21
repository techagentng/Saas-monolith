# Multi-Tenant Booking System — Epic 01: Identity & Access

**File:** `EPIC-01-IDENTITY-ACCESS.md`  
**Document Type:** Agent Implementation Specification  
**Status:** Authoritative Epic-level implementation context

---

## 1. Epic Overview

**Project:** Multi-Tenant Booking System  
**Epic:** Identity & Access  
**Backend:** Go  
**Frontend:** Next.js + TypeScript  
**Database:** PostgreSQL  
**Architecture:** Modular monolith  
**Request Flow:** API → Handler → Service → Repository → Database  
**Development Method:** Pure TDD / Test-First Development

### Primary Goal

Establish the secure identity, authentication, authorization, role, permission, tenant-access, and audit foundation required by the rest of the platform.

This document is the implementation source of truth for Epic 1.

---

## 2. Purpose

The implementation agent must use this document to reconstruct the intended architecture and development direction if previous conversation context is unavailable.

Before writing implementation code, understand:

1. Business responsibility of this Epic
2. Architectural boundaries
3. Security requirements
4. Data model
5. API contracts
6. Error-code system
7. Testing strategy
8. Implementation sequence
9. Acceptance criteria

Implementation must be incremental and feature-by-feature.

Do not generate the entire Epic in one step.

---

## 3. Architecture

The backend follows:

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

A module is an organizational boundary, not another architectural layer.

Example:

```text
identity/
├── handler/
├── service/
├── repository/
├── model/
└── ...
```

The runtime request still follows:

```text
HTTP API
 ↓
Identity Handler
 ↓
Identity Service
 ↓
Identity Repository
 ↓
PostgreSQL
```

The same principle applies to:

```text
booking
tenant
branding
notification
payment
audit
authorization
```

Do not introduce a "module layer" between Handler and Service.

---

## 4. SOLID Responsibilities

### Handler

- Parse request
- Validate transport-level input
- Obtain authentication context
- Call service
- Map application errors to HTTP responses

### Service

- Business rules
- Authorization decisions
- Orchestration
- Domain validation

### Repository

- SQL
- Persistence
- Database queries
- Transaction interaction

Services should depend on repository interfaces rather than concrete PostgreSQL implementations where appropriate.

---

## 5. Multi-Tenancy Foundation

Tenant isolation is a security boundary.

Conceptually:

```text
User
 ├── Roles
 ├── Permissions
 └── Tenant Memberships
       └── Tenant
```

A user may potentially belong to one or more tenants depending on final product rules.

Never assume:

```text
authenticated user = authorized tenant user
```

Authentication answers:

```text
Who are you?
```

Authorization answers:

```text
What are you allowed to do?
```

Tenant authorization answers:

```text
Which tenant are you allowed to operate within?
```

These are separate checks.

---

## 6. Epic Objective

Epic 1 establishes:

```text
Identity
 ↓
Authentication
 ↓
Tenant Membership
 ↓
Roles
 ↓
Permissions
 ↓
Authorization
 ↓
Auditability
```

Future modules must be able to depend on this foundation.

Expected future consumers include:

- Tenant Management
- Booking
- Staff
- Customer
- Payments
- Notifications
- Reporting
- Branding

---

## 7. Scope

### Identity

- User creation
- User retrieval
- User status
- User identity
- Password credentials
- Email identity

### Authentication

- Login
- Password verification
- Session/token issuance
- Authentication middleware
- Logout/revocation strategy
- Authentication failure handling

### Tenant Membership

- Assign user to tenant
- Remove user from tenant
- Determine tenant access
- Prevent cross-tenant access

### Roles

- Create/manage roles
- Assign roles to users
- Remove roles from users

### Permissions

- Define permissions
- Assign permissions to roles
- Resolve effective permissions
- Enforce permissions

### Authorization

- Authentication checks
- Tenant checks
- Permission checks
- Resource ownership/access checks where required

### Audit

Security-sensitive actions should be auditable.

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

---

## 8. Out of Scope

Do not expand this Epic into unrelated business functionality.

Not part of Epic 1 unless explicitly added later:

- Booking creation
- Calendar management
- Customer booking flow
- Payment processing
- Tenant branding UI
- Notifications
- Reporting
- Marketing
- Staff scheduling
- Resource management

Identity & Access provides the security foundation those modules consume.

---

## 9. Initial Roles

Initial conceptual roles:

```text
SUPER_ADMIN
BUSINESS_OWNER
STAFF
CUSTOMER
```

These roles are backend authorization concepts.

They must not be implemented as frontend-only UI states.

The final permission matrix should be defined explicitly before implementing behavior that depends on an undefined role rule.

---

## 10. Password Security

Use a well-established password hashing library designed for password storage.

Never use:

```text
SHA256(password)
MD5(password)
plaintext password
reversible encrypted password
```

Requirements:

- Reject invalid/empty credentials
- Never store plaintext passwords
- Use a slow password hashing algorithm designed for password storage
- Never return password hashes through APIs
- Never log passwords
- Never put passwords in error messages
- Never put passwords in audit records
- Avoid account enumeration

Avoid:

```text
Email exists but password is incorrect.
```

Prefer a generic authentication failure such as:

```text
Invalid credentials.
```

with an appropriate stable error code.

---

## 11. Authentication Tokens / Sessions

The selected token/session strategy must explicitly document:

- Token type
- Token lifetime
- Refresh strategy
- Revocation strategy
- Storage strategy
- Logout behavior
- Rotation behavior

Do not casually create a long-lived JWT containing sensitive information.

Tokens must not contain:

- Passwords
- Password hashes
- Secrets
- Sensitive personal data

Refresh tokens must be treated as sensitive credentials.

---

## 12. Authentication Context

After successful authentication:

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

```text
user_id
tenant context
authentication state
roles
permissions
```

The service must not blindly trust tenant IDs supplied by the client.

For example, this must never automatically grant access:

```json
{
  "tenant_id": "another-tenant"
}
```

Tenant access must be resolved against authenticated membership and authorization.

---

## 13. Tenant Isolation

Every tenant-scoped operation must establish:

1. Who is making the request?
2. Which tenant are they acting within?
3. Are they a member of that tenant?
4. Do they have the required permission?
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

Prefer:

```sql
SELECT *
FROM bookings
WHERE id = $1
AND tenant_id = $2;
```

---

## 14. IDOR / Object-Level Authorization

The API must prevent insecure direct object reference / broken object-level authorization.

For:

```text
GET /bookings/123
```

the system must verify:

```text
booking 123 belongs to tenant X
AND
authenticated user has access to tenant X
AND
authenticated user has booking.read
```

The check must happen server-side.

---

## 15. Error Management Standard

Epic 1 MUST use the project's error-management convention.

Application logic must not depend on:

```go
if err.Error() == "user already exists" {
    ...
}
```

Use stable application error codes.

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

Use descriptive string codes, not arbitrary numeric codes.

---

## 16. Application Error Model

Conceptually:

```go
type ErrorCode string

type AppError struct {
    Code    ErrorCode
    Message string
    Err     error
}
```

Mandatory principles:

```text
Code    = stable machine-readable identity
Message = presentation
Err     = optional underlying cause
```

Business logic reasons about the application error.

The API layer maps the application error to the final HTTP response.

---

## 17. Error Wrapping

Errors must preserve identity through:

```text
Repository
 ↓
Service
 ↓
Handler
 ↓
HTTP
```

Use:

```go
fmt.Errorf("creating user: %w", err)
```

and recover the underlying application error with:

```go
errors.As(...)
```

Never parse error messages.

---

## 18. Error Categories

### Expected business errors

Examples:

```text
USER_NOT_FOUND
USER_ALREADY_EXISTS
INVALID_CREDENTIALS
ACCOUNT_DISABLED
TENANT_ACCESS_DENIED
PERMISSION_DENIED
ROLE_NOT_FOUND
VALIDATION_FAILED
```

### Unexpected system errors

Examples:

```text
database unavailable
network failure
unexpected nil pointer
filesystem failure
unknown infrastructure failure
```

Do not expose raw infrastructure errors to API clients.

Map unexpected failures to safe generic errors such as:

```text
SERVICE_UNAVAILABLE
INTERNAL_ERROR
```

where appropriate.

---

## 19. API Error Mapping

| Application Code | HTTP | Meaning |
|---|---:|---|
| VALIDATION_FAILED | 400 | Request validation failed |
| INVALID_REQUEST | 400 | Malformed request |
| INVALID_CREDENTIALS | 401 | Authentication failed |
| SESSION_EXPIRED | 401 | Authentication no longer valid |
| SESSION_REVOKED | 401 | Session revoked |
| PERMISSION_DENIED | 403 | Required permission missing |
| TENANT_ACCESS_DENIED | 403 | Tenant cannot be accessed |
| RESOURCE_NOT_FOUND | 404 | Resource does not exist/is not visible |
| USER_NOT_FOUND | 404 | User does not exist |
| TENANT_NOT_FOUND | 404 | Tenant does not exist |
| USER_ALREADY_EXISTS | 409 | Existing identity conflict |
| ROLE_ALREADY_EXISTS | 409 | Role already exists |
| RATE_LIMITED | 429 | Too many requests |
| INTERNAL_ERROR | 500 | Unexpected server failure |
| SERVICE_UNAVAILABLE | 503 | Temporary infrastructure failure |

HTTP mapping should be centralized.

---

## 20. Security Requirements

Epic 1 must:

- Never trust frontend authorization
- Never trust client-supplied tenant context
- Never store plaintext passwords
- Never expose secrets
- Never expose internal errors
- Prevent cross-tenant access
- Protect object-level authorization
- Rate-limit security-sensitive endpoints where appropriate
- Avoid account enumeration
- Audit security-sensitive actions

---

## 21. Observability

Security-sensitive operations should be observable.

Useful dimensions:

```text
request_id
tenant_id
user_id
operation
error_code
duration
status
```

Do not use arbitrary user IDs as Prometheus labels.

Use stable error codes as low-cardinality metrics dimensions.

Use logs/traces for high-cardinality identifiers.

---

## 22. Feature Delivery Rule

Each feature is delivered independently:

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

## 23. Epic Definition of Done

Epic 1 is complete only when the system can reliably answer:

- Who is this user?
- Is this user authenticated?
- Which tenant(s) can this user access?
- Which role does the user have within the relevant scope?
- Which permissions does the user effectively have?
- Is the user allowed to perform the operation?
- Does the requested resource belong to an authorized tenant?
- Can expected failures be represented using stable error codes?
- Can unexpected errors be safely hidden from clients?
- Can security-sensitive events be audited?
- Can the frontend consume stable authentication/authorization error codes?
- Can all of the above be demonstrated through automated tests?

The core security property is:

```text
Authentication
     ≠
Authorization
     ≠
Tenant Access
     ≠
Frontend Visibility
```

All four must be handled independently and correctly.

---

## 24. Agent Rules

The implementation agent must:

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
13. Do not implement everything in one giant change.
14. Write tests before implementation.
15. Do not silently invent business rules.

If the specification does not define a behavior, identify the ambiguity before implementing it.

---

## 25. Expected Backend Structure

Conceptual structure:

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

This is conceptual.

Do not create every directory simply because it appears in this diagram. The actual structure should emerge as features are implemented.

---

## 26. Module Boundaries

### Identity Module

- User identity
- Credentials
- Authentication

### Authorization Module

- Roles
- Permissions
- Access policies

### Tenant Module

- Tenant membership
- Tenant context
- Tenant access

### Audit Module

- Security-sensitive audit events

These modules still follow:

```text
Handler → Service → Repository
```

internally where applicable.

---

## 27. Future Extensibility

The current implementation should permit eventual module extraction if needed.

Do not prematurely introduce:

- Message brokers
- Service discovery
- Distributed transactions
- Multiple deployments

unless a later requirement explicitly requires them.

---

## 28. Implementation Order

The Epic should be implemented in this order:

```text
1. Error Infrastructure
2. User Identity
3. Tenant Membership
4. Roles & Permissions
5. Authentication
6. Authorization
7. Audit
8. Frontend Authentication
```

This sequence exists because the error contract and identity/security foundations are dependencies for later features.

---

## 29. First Implementation Target

The first implementation target is:

# Feature 1 — Application Error Infrastructure

Do **not** begin with login.

Before implementation, the agent must produce a short plan identifying:

- Files to create
- Interfaces/types required
- Tests required
- Error codes required
- HTTP mapping required
- Dependencies required

Then implement **only Feature 1**.

Do not jump ahead to:

- Users
- Login
- Roles
- Permissions
- Tenant membership

until Feature 1 is complete and its tests pass.

---

## 30. Agent Context Restoration

If previous conversation context is lost, treat this document and the Master Project Specification as authoritative.

```text
PROJECT:
Multi-Tenant Booking System

ARCHITECTURE:
API → Handler → Service → Repository → Database

STYLE:
Modular monolith

BACKEND:
Go

DATABASE:
PostgreSQL

FRONTEND:
Next.js + TypeScript

DEVELOPMENT:
Pure TDD

IDENTITY:
Users + credentials + authentication

AUTHORIZATION:
Roles + permissions

MULTI-TENANCY:
Tenant membership and tenant isolation are security boundaries

ERROR MANAGEMENT:
Stable string application error codes
errors.As
%w wrapping
HTTP status = transport semantics
Error code = application semantics
Message = presentation

SECURITY:
Never trust frontend authorization
Never trust client tenant context
Never store plaintext passwords
Never expose secrets
Never expose internal errors
Prevent cross-tenant access
Protect object-level authorization
Avoid account enumeration
Rate-limit security-sensitive endpoints

OBSERVABILITY:
Request IDs
Stable error codes
Security audit events

IMPLEMENTATION:
One feature at a time
Tests first
No giant changes
No invented business rules
```

---

## 31. Final Architectural Principle

All Identity & Access functionality must preserve:

```text
┌─────────────────────────┐
│ HTTP / API              │
└────────────┬────────────┘
             ↓
┌─────────────────────────┐
│ Handler                 │
│ HTTP concerns only      │
└────────────┬────────────┘
             ↓
┌─────────────────────────┐
│ Service                 │
│ Business rules          │
└────────────┬────────────┘
             ↓
┌─────────────────────────┐
│ Repository              │
│ Persistence only        │
└────────────┬────────────┘
             ↓
┌─────────────────────────┐
│ PostgreSQL              │
└─────────────────────────┘
```

Cross-cutting concerns such as:

- Authentication
- Authorization
- Error handling
- Logging
- Audit
- Request IDs

must be introduced deliberately without destroying this separation.

---

**End of Epic 01 Specification**
