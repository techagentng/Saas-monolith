# Multi-Tenant Booking System
# Epic 1 — Feature 1: Application Error Infrastructure

**Document Type:** Feature Implementation Specification / Context Restoration Document  
**Project:** Multi-Tenant Booking System  
**Epic:** Epic 1 — Identity & Access  
**Feature:** Feature 1 — Application Error Infrastructure  
**Backend:** Go  
**Architecture:** Modular Monolith  
**Development Method:** Pure TDD / Test-First Development  

---

## 1. Purpose

This document is the authoritative feature-level context for implementing and reviewing **Epic 1, Feature 1 — Application Error Infrastructure**.

It supplements:

1. The project Master Specification.
2. The Epic 1 — Identity & Access specification.
3. The project's `error management.docx` concept.

The feature establishes reusable, centralized error infrastructure. It must not implement Identity, Authentication, Tenant, Booking, Role, Permission, or other business functionality.

---

# 2. Architecture Context

The backend architecture is:

```text
API → Handler → Service → Repository → Database
```

A module is an organizational boundary, **not another architectural layer**.

Feature 1 is cross-cutting infrastructure. It provides a shared error contract that future handlers, services, and repositories will consume.

It must not introduce a new architectural layer.

---

# 3. Feature Goal

Build centralized, type-safe application error infrastructure providing:

- Stable string-based application error codes.
- Typed application errors.
- Optional underlying causes.
- Error wrapping using Go `%w`.
- Error-chain detection using `errors.As`.
- Centralized application-error-to-HTTP mapping.
- Safe handling of unexpected errors.
- Consistent JSON API error responses.
- A public API boundary that never exposes internal diagnostic details.

Out of scope:

- Users.
- Authentication.
- Sessions.
- Tenants.
- Roles.
- Permissions.
- Authorization.
- Bookings.
- Audit workflows.
- Database migrations.
- Frontend implementation.

---

# 4. Error Management Principles

The project uses structured application error codes because error strings are not stable contracts.

```text
Error Code
    = stable machine-readable application identity

HTTP Status
    = transport-level semantics

Public Message
    = safe client-facing presentation

Underlying Cause
    = internal diagnostic information
```

Never write application logic based on:

```go
err.Error()
```

Instead, use typed application errors and stable codes.

---

# 5. Application Error Model

Conceptually:

```go
type ErrorCode string

type AppError struct {
    Code    ErrorCode
    Message string
    Err     error
}
```

Responsibilities:

- `Code` identifies the stable application failure.
- `Message` is optional safe presentation context.
- `Err` preserves the underlying internal cause.

The type must:

- Implement Go's `error` interface.
- Support an optional underlying cause.
- Implement `Unwrap()` where appropriate.
- Remain discoverable with `errors.As`.
- Preserve identity through multiple `%w` wrapping layers.

Internal causes must never be exposed through public API responses.

---

# 6. Error Wrapping Strategy

Expected application errors should be created where their semantic meaning is known.

Higher layers may add diagnostic context using:

```go
fmt.Errorf("context: %w", err)
```

The flow may be:

```text
Repository
    ↓ returns typed application error
Service
    ↓ wraps with context using %w
Handler / HTTP Mapper
    ↓ uses errors.As
```

The HTTP mapping layer must inspect the complete error chain, not only the outermost error.

Conceptually:

```go
var appErr *AppError

if errors.As(err, &appErr) {
    // map appErr.Code
}
```

Never compare business errors using error text.

---

# 7. Initial Error Codes

Feature 1 establishes the following initial shared codes:

```text
VALIDATION_FAILED
INVALID_REQUEST

INVALID_CREDENTIALS
SESSION_EXPIRED
SESSION_REVOKED

PERMISSION_DENIED
TENANT_ACCESS_DENIED

RESOURCE_NOT_FOUND
USER_NOT_FOUND
TENANT_NOT_FOUND

USER_ALREADY_EXISTS
ROLE_ALREADY_EXISTS

RATE_LIMITED

SERVICE_UNAVAILABLE
INTERNAL_ERROR
```

## Extensibility Rule

The initial list is **not permanently exhaustive**.

Future features may add centrally defined error codes when they introduce legitimate new expected business failures.

However:

- Existing error-code string values are stable contracts.
- Existing codes must not be renamed casually.
- Codes must not be duplicated in feature-local packages.
- New codes should be centrally defined in the shared error infrastructure.
- Do not add speculative codes merely because they might be useful later.

---

# 8. HTTP Status Mapping

| Application Code | HTTP Status |
|---|---:|
| `VALIDATION_FAILED` | 400 |
| `INVALID_REQUEST` | 400 |
| `INVALID_CREDENTIALS` | 401 |
| `SESSION_EXPIRED` | 401 |
| `SESSION_REVOKED` | 401 |
| `PERMISSION_DENIED` | 403 |
| `TENANT_ACCESS_DENIED` | 403 |
| `RESOURCE_NOT_FOUND` | 404 |
| `USER_NOT_FOUND` | 404 |
| `TENANT_NOT_FOUND` | 404 |
| `USER_ALREADY_EXISTS` | 409 |
| `ROLE_ALREADY_EXISTS` | 409 |
| `RATE_LIMITED` | 429 |
| `INTERNAL_ERROR` | 500 |
| `SERVICE_UNAVAILABLE` | 503 |

The mapping must be centralized.

Handlers must not independently recreate application-error mapping logic.

---

# 9. INTERNAL_ERROR vs SERVICE_UNAVAILABLE

## INTERNAL_ERROR

Use for unexpected application/programming failures.

```text
Unexpected failure
→ INTERNAL_ERROR
→ HTTP 500
```

The public response must be generic and safe.

## SERVICE_UNAVAILABLE

Use for intentionally classified temporary infrastructure or dependency failures.

```text
Known temporary dependency failure
→ SERVICE_UNAVAILABLE
→ HTTP 503
```

Do not infer `SERVICE_UNAVAILABLE` merely by parsing arbitrary database or network error text.

If an error is not explicitly classified, the safe centralized fallback should be `INTERNAL_ERROR`.

---

# 10. Unexpected Error Handling

The mapper should:

1. Inspect the error chain for a known `AppError`.
2. If found, resolve the known code through centralized mapping.
3. If the code is unknown, use the safe fallback.
4. If no application error is found, classify it as unexpected.
5. Return a generic public response.
6. Never serialize the original error to the client.

Default unexpected behavior:

```text
HTTP 500
Code: INTERNAL_ERROR
Message: generic safe message
```

Never expose:

- Stack traces.
- SQL statements.
- Database driver messages.
- Internal hostnames.
- Filesystem paths.
- Credentials.
- Tokens.
- Secrets.
- Raw wrapped error text.

---

# 11. Public Message Safety

Application messages are public API data.

An `AppError` may carry an optional safe message, but arbitrary internal messages must not automatically become public API responses.

The public error mapper/serializer is the final security boundary.

```text
Known application error
    ↓
Stable code
+
Safe public message

Unknown/unexpected error
    ↓
INTERNAL_ERROR
+
Generic safe public message
```

The underlying `Err` must never be serialized.

This design allows future centralized messages and localization.

---

# 12. Public JSON Error Contract

Use one consistent response shape:

```json
{
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "Invalid credentials."
  }
}
```

Requirements:

- Top-level `error` object.
- Stable `code`.
- Safe `message`.
- Correct HTTP status.
- JSON content type.
- No internal diagnostic fields.

Serialize sanitized public data rather than arbitrary Go error objects.

Do not invent request-ID infrastructure for Feature 1.

---

# 13. Security Requirements

The implementation must ensure:

- Underlying causes are never automatically exposed.
- SQL details never reach API responses.
- Stack traces never reach API responses.
- Credentials and secrets never reach API responses.
- Error mapping does not depend on message text.
- Unknown errors fail closed to a generic safe response.
- Stable codes do not contain sensitive user-specific data.
- No high-cardinality dynamic values are embedded in error-code constants.

Feature 1 must not introduce logging or observability infrastructure.

---

# 14. Observability Context

Stable error codes are suitable for low-cardinality metrics:

```text
errors_total{code="INVALID_CREDENTIALS"}
errors_total{code="PERMISSION_DENIED"}
errors_total{code="SERVICE_UNAVAILABLE"}
```

Do not use arbitrary error strings as metric labels.

Do not place user IDs, request IDs, or timestamps inside error codes or metric labels.

Actual logging/metrics infrastructure is outside Feature 1.

---

# 15. Scope Boundaries

## Feature 1 MAY create

The shared error package and its tests, likely under:

```text
internal/errors/
```

Likely concerns:

- Error codes.
- Typed application errors.
- HTTP mapping.
- Public response serialization.

The exact number of files is not prescribed. File boundaries should follow cohesion.

## Feature 1 MUST NOT implement

- User identity.
- User repository.
- Authentication.
- Password hashing.
- Sessions.
- JWT/token issuance.
- Tenant membership.
- Roles.
- Permissions.
- Authorization rules.
- Booking logic.
- Database migrations.
- PostgreSQL configuration.
- Frontend implementation.
- Audit implementation.
- Logging/metrics systems.

Do not modify unrelated files merely to force integration.

If there is no HTTP server yet, `main.go` does not need modification for Feature 1.

---

# 16. Package Design Guidance

The error infrastructure should be stateless.

It should not require:

- Database dependencies.
- Repository dependencies.
- Service dependencies.
- Configuration.
- A logger.
- A global mutable registry.

Do not create unnecessary service or repository interfaces.

The Go standard library should be sufficient.

Likely packages:

```text
errors
fmt
encoding/json
net/http
```

No third-party error package is required.

---

# 17. TDD Requirements

Implement using test-first development.

Recommended sequence:

1. Write failing tests for initial error-code values.
2. Write failing tests for application error construction.
3. Write failing tests for `error` interface behavior.
4. Write failing tests for optional causes.
5. Write failing tests for `Unwrap()`.
6. Write failing tests for `%w` wrapping.
7. Write failing tests for `errors.As`.
8. Write failing tests for centralized HTTP mapping.
9. Write failing tests for unknown-error fallback.
10. Write failing tests for public-response sanitization.
11. Implement the minimum code required.
12. Run focused package tests.
13. Fix only Feature 1 failures.
14. Run the full repository test suite.
15. Review scope, architecture, and security.

Do not proceed to User Identity merely because the package compiles.

---

# 18. Required Test Coverage

## Error Code Tests

Verify that initial documented constants have correct stable string values.

Do not create brittle tests asserting that the package can never gain new codes.

The contract is:

> Existing code values remain stable.

## Application Error Tests

Test:

- Construction with code and safe message.
- Construction with an underlying cause.
- Construction without a cause.
- Implementation of the standard `error` interface.
- No panic when cause is absent.
- Defined behavior for invalid construction inputs.

## Wrapping Tests

Test direct errors, one `%w` wrapping layer, and multiple wrapping layers.

Verify `errors.As` can locate the typed application error and the stable code remains accessible.

## HTTP Mapping Tests

Test every initial documented code.

Also test:

- Unknown application code.
- Plain standard-library error.
- Application error wrapped with contextual `%w`.
- Multiple wrapping layers.
- Explicit `SERVICE_UNAVAILABLE`.
- Safe fallback behavior.

## Serialization Tests

Verify:

- Correct top-level JSON shape.
- Correct code.
- Correct safe message.
- Valid JSON.
- Underlying causes are absent.
- SQL-like diagnostic text is absent.
- Network/host diagnostic text is absent.
- Filesystem paths are absent.
- Secret-like values are absent.
- Stack traces and diagnostic context are absent.

## Edge Cases

Define and test behavior for:

- Nil error passed to mapping/serialization APIs.
- Unknown application code.
- Empty safe message.
- Missing underlying cause.
- Multiple wrapping layers.
- Unrelated standard errors.

The implementation must not panic on unexpected input.

---

# 19. Acceptance Criteria

Feature 1 is complete when:

- [ ] Initial shared error codes exist centrally.
- [ ] Existing code values are treated as stable contracts.
- [ ] Future features can extend the central code set deliberately.
- [ ] Typed application errors support optional underlying causes.
- [ ] The type satisfies Go's `error` interface.
- [ ] Wrapping with `%w` preserves application-error identity.
- [ ] `errors.As` can detect application errors through multiple wrapping layers.
- [ ] HTTP mapping is centralized.
- [ ] Every initial documented code maps to the required HTTP status.
- [ ] `INTERNAL_ERROR` maps to HTTP 500.
- [ ] `SERVICE_UNAVAILABLE` maps to HTTP 503.
- [ ] Unknown and unexpected errors fall back safely.
- [ ] Public responses use one consistent JSON structure.
- [ ] Underlying causes never appear in public JSON.
- [ ] Sensitive/internal diagnostic information never appears in public responses.
- [ ] No business module is implemented as part of Feature 1.
- [ ] No database or external-service dependency is introduced.
- [ ] No unnecessary abstraction is introduced.
- [ ] Tests are written before implementation.
- [ ] Focused package tests pass.
- [ ] The full repository test suite passes.

---

# 20. Agent Review Rules

When reviewing this feature:

1. Review against this specification, the Epic 1 specification, and the project's error-management concept.
2. Do not redesign the project architecture.
3. Do not suggest unrelated features.
4. Do not recommend microservices.
5. Do not add authentication or business logic.
6. Do not require modification of `main.go` merely for integration.
7. Prioritize correctness, security, simplicity, and testability.
8. Classify findings as:
   - Critical
   - Important
   - Minor
9. Distinguish genuine requirement violations from optional stylistic preferences.
10. Do not modify files during a review unless explicitly instructed.

Specifically verify:

- Stable string error codes.
- Typed application errors.
- `%w` wrapping.
- `errors.As` behavior.
- Error-chain inspection.
- Centralized HTTP mapping.
- Correct HTTP statuses.
- Correct `INTERNAL_ERROR` vs `SERVICE_UNAVAILABLE` semantics.
- Safe unknown-error fallback.
- Sanitized JSON responses.
- No sensitive information leakage.
- No scope creep.
- Adequate TDD coverage.

---

# 21. Context Restoration Summary

```text
PROJECT:
Multi-Tenant Booking System

EPIC:
Epic 1 — Identity & Access

FEATURE:
Feature 1 — Application Error Infrastructure

ARCHITECTURE:
API → Handler → Service → Repository → Database

STYLE:
Modular Monolith

IMPORTANT:
A module is an organizational boundary, not another architectural layer.

DEVELOPMENT:
Pure TDD / tests first.

FEATURE PURPOSE:
Create centralized reusable error infrastructure before business functionality.

CORE ERROR MODEL:
ErrorCode = stable machine-readable identity
HTTP status = transport semantics
Public message = safe presentation
Underlying cause = internal diagnostics only

GO ERROR HANDLING:
Use typed AppError.
Use %w for contextual wrapping.
Use errors.As to discover AppError through the error chain.
Never branch application logic on err.Error().

UNEXPECTED FAILURES:
Unexpected application/programming failure
→ INTERNAL_ERROR
→ HTTP 500

Known intentionally classified temporary dependency failure
→ SERVICE_UNAVAILABLE
→ HTTP 503

PUBLIC API:
Use one consistent JSON error envelope:
{
  "error": {
    "code": "...",
    "message": "..."
  }
}

SECURITY:
Never expose Err.Error() automatically.
Never expose stack traces, SQL, paths, credentials, tokens, or secrets.
Unknown errors receive a generic safe response.

SCOPE:
Do not implement users, authentication, sessions, tenants, roles,
permissions, authorization, booking, database migrations, or frontend.

PACKAGE:
Likely internal/errors.
Keep it stateless.
No third-party dependency required.
No unnecessary interfaces.

TESTING:
TDD.
Test constants, construction, optional causes, wrapping, errors.As,
HTTP mapping, unknown fallback, and serialization sanitization.

REVIEW:
Review only against authoritative specifications.
Classify findings as Critical, Important, or Minor.
Do not redesign the architecture or introduce unrelated features.
```
