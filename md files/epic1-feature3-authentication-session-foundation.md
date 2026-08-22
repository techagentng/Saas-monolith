# Multi-Tenant Booking System
# Epic 1 — Feature 3: Authentication & Session Foundation

**Document Type:** Feature Implementation Specification / Context Restoration Document  
**Project:** Multi-Tenant Booking System  
**Epic:** Epic 1 — Identity & Access  
**Feature:** Feature 3 — Authentication & Session Foundation  
**Backend:** Go  
**Database:** PostgreSQL  
**Architecture:** Modular Monolith  
**Request Flow:** API → Handler → Service → Repository → Database  
**Development Method:** Pure TDD / Test-First Development  

---

## 1. Purpose

This document is the authoritative feature-level specification and context-restoration document for:

**Epic 1, Feature 3 — Authentication & Session Foundation**

Feature 3 builds on the completed foundations from:

1. Master Project Specification.
2. Epic 1 — Identity & Access specification.
3. Feature 1 — Application Error Infrastructure.
4. Feature 2 — User Identity.

Feature 3 answers:

> How does an existing user prove their identity, establish an authenticated session, maintain that session securely, and terminate it?

Feature 3 does **not** answer:

> Which tenant is the user currently acting within?

or:

> What is the authenticated user allowed to do?

Those responsibilities belong to later features:

```text
F3 Authentication & Sessions
        ↓
F4 Tenant Membership & Context
        ↓
F5 Roles & Permissions
        ↓
F6 Authorization & Access Enforcement
```

If a requirement is ambiguous, the implementation agent must identify the ambiguity rather than silently inventing a security or business rule.

---

# 2. Dependency on Previous Features

Feature 3 assumes Feature 1 and Feature 2 are complete.

## Feature 1 provides

- Stable application error codes.
- Typed application errors.
- `%w` wrapping.
- `errors.As` error-chain inspection.
- Centralized HTTP error mapping.
- Sanitized public error responses.

Feature 3 must reuse this infrastructure.

## Feature 2 provides

At minimum:

```text
User
├── ID
├── Email
├── PasswordHash
├── Status
├── CreatedAt
└── UpdatedAt
```

Feature 3 must reuse the existing user identity implementation.

Do not create a second user model, user table, email-normalization strategy, or password-hashing system.

---

# 3. Architecture Context

The backend architecture remains:

```text
API → Handler → Service → Repository → Database
```

Authentication does not create a new architectural layer.

Conceptually:

```text
HTTP Request
     ↓
Authentication Handler
     ↓
Authentication Service
     ↓
User Repository + Session Repository
     ↓
PostgreSQL
```

For authenticated requests:

```text
HTTP Request
     ↓
Authentication Middleware
     ↓
Authenticated Principal / Request Context
     ↓
Handler
     ↓
Service
```

The middleware authenticates the caller.

It must **not** perform tenant authorization, role authorization, or permission checks.

---

# 4. Feature Goal

Implement the minimum secure authentication and session foundation required for later tenant and authorization features.

Feature 3 must support:

1. Login using email and password.
2. Secure password verification.
3. Generic invalid-credential handling.
4. Rejection of disabled users.
5. Creation of authenticated sessions.
6. Issuance of short-lived access credentials.
7. Issuance and secure handling of refresh/session credentials where approved.
8. Session refresh.
9. Session revocation/logout.
10. Authentication middleware.
11. Authenticated principal/request context.
12. Session expiration.
13. Secure token handling.
14. Reuse of Feature 1 error infrastructure.
15. Pure TDD coverage.

---

# 5. Feature Scope

## In Scope

Feature 3 includes:

- Login.
- Password verification.
- Authentication service.
- Session persistence.
- Session repository.
- Access-token issuance.
- Refresh/session-token issuance where approved.
- Refresh/session rotation where approved.
- Logout.
- Session revocation.
- Session expiration.
- Authentication middleware.
- Authenticated principal/request context.
- Disabled-user login rejection.
- Generic credential failures.
- Authentication-related database migration(s).
- Authentication-related handlers.
- Authentication security tests.
- TDD.

---

## Explicitly Out of Scope

Do **not** implement:

- Tenant membership.
- Tenant selection.
- Tenant switching.
- Tenant context resolution.
- Tenant authorization.
- Roles.
- Permissions.
- Role assignment.
- Permission assignment.
- RBAC.
- Authorization middleware.
- Resource ownership authorization.
- Super-admin behavior.
- Password reset.
- Forgot-password workflow.
- Email verification.
- MFA / 2FA.
- OAuth.
- Social login.
- SSO.
- API keys.
- Device-management UI.
- Audit infrastructure unless already required by an approved higher-level specification.
- Frontend authentication UI.
- Account lockout state machine unless explicitly approved.
- Password change workflow unless explicitly approved.

Authentication proves identity.

Authorization decides what that identity may do.

Feature 3 must not mix the two.

---

# 6. Authentication Terminology

Use these concepts consistently.

## User

The persistent identity created in Feature 2.

## Credentials

Information supplied by the caller to prove identity.

For Feature 3:

```text
Email + Password
```

## Session

A server-recognized authenticated login instance.

A user may eventually have multiple sessions across devices.

## Access Token

A short-lived credential used to authenticate normal API requests.

## Refresh Token / Session Credential

A longer-lived credential used to obtain a new access token without requiring the user to enter their password again.

## Authenticated Principal

The minimal identity information placed into trusted request context after successful authentication.

At this stage it should represent identity, not authorization.

Conceptually:

```text
AuthenticatedPrincipal
├── UserID
└── SessionID
```

Do not place roles, permissions, or tenant authorization decisions into the principal during Feature 3.

---

# 7. Authentication Flow

Expected login flow:

```text
Client
   ↓
POST /api/v1/auth/login
   ↓
Authentication Handler
   ↓
Validate Request Shape
   ↓
Authentication Service
   ↓
Normalize Email Using Feature 2 Policy
   ↓
Find User By Email
   ↓
Verify Password
   ↓
Verify User Status
   ↓
Create Session
   ↓
Issue Authentication Credentials
   ↓
Return Safe Authentication Response
```

Authentication failure must not reveal whether:

- The email exists.
- The password was wrong.
- The user record was absent.

Externally these should normally collapse to the same public credential failure.

---

# 8. Login Requirements

The login request contains:

```text
Email
Password
```

The handler must:

- Parse JSON.
- Validate request shape.
- Call the authentication service.
- Never perform password verification itself.
- Never query the database directly.
- Never expose credential values in errors.

The service must:

1. Normalize email according to Feature 2's approved normalization policy.
2. Retrieve the user internally.
3. Verify the password securely.
4. Check whether the user is allowed to authenticate based on user status.
5. Create the session.
6. Issue credentials.
7. Return safe authentication data.

---

# 9. Generic Invalid Credentials

Feature 1 already defines:

```text
INVALID_CREDENTIALS
```

Feature 3 must use it.

The public API must not distinguish between:

```text
Unknown email
Wrong password
```

Both should produce:

```text
INVALID_CREDENTIALS
```

with the centralized HTTP mapping:

```text
HTTP 401 Unauthorized
```

The public message should remain generic.

Example concept:

```json
{
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "Invalid credentials."
  }
}
```

Do not return messages such as:

```text
Email not found.
User does not exist.
Password is incorrect.
```

This helps reduce account-enumeration leakage.

Internal logs/diagnostics may distinguish failures later, but public responses must not.

---

# 10. Password Verification

Feature 3 must reuse Feature 2's password hashing implementation.

Do not create another hashing algorithm or independent password format.

The password component should conceptually support:

```text
Hash(password)
Verify(password, encodedHash)
```

Verification must:

- Use the approved Feature 2 password-hashing implementation.
- Never decrypt passwords.
- Never compare plaintext passwords directly.
- Never expose the password hash.
- Never log the supplied password.
- Never include credentials in errors.

A password mismatch is an expected authentication failure and maps to:

```text
INVALID_CREDENTIALS
```

---

# 11. Disabled User Behavior

Feature 2 established user status.

Feature 3 must reject authentication for users whose status does not permit login.

However, the public response must be designed carefully.

A disabled account should not automatically produce a response that unnecessarily confirms account existence.

The implementation plan must explicitly decide whether disabled-user login returns:

```text
INVALID_CREDENTIALS
```

or a dedicated centrally defined code such as:

```text
USER_DISABLED
```

If `USER_DISABLED` is introduced, the security/account-enumeration implications must be considered before approval.

Do not silently add the code.

---

# 12. Session Model

Feature 3 introduces persistent sessions.

Conceptually:

```text
Session
├── ID
├── UserID
├── RefreshTokenHash / SessionSecretHash
├── CreatedAt
├── ExpiresAt
├── RevokedAt
└── UpdatedAt (if required)
```

Exact fields depend on the approved token/session strategy.

The session must allow the backend to determine:

- Which user owns the session.
- Whether the session exists.
- Whether it has expired.
- Whether it has been revoked.
- Whether a refresh/session credential is valid.

Do not add tenant IDs, roles, or permissions to the session merely for future convenience.

---

# 13. Session Persistence

Sessions must be server-revocable.

Logout and security-sensitive revocation must not depend solely on waiting for a long-lived credential to expire.

The session repository may need capabilities such as:

```text
Create
FindByID
RotateRefreshCredential
Revoke
```

Exact interface methods should be determined by the approved session design.

The repository must:

- Use parameterized SQL.
- Never expose raw database errors publicly.
- Preserve expected application errors.
- Wrap diagnostic context using `%w`.
- Remain independent of HTTP.

---

# 14. Access Token Strategy

Feature 3 may use short-lived signed access tokens if approved.

An access token should contain only the minimum authentication claims required by the backend.

A likely minimal claim set is:

```text
Subject / UserID
SessionID
IssuedAt
ExpiresAt
```

Do not place the following into access tokens during Feature 3:

```text
Tenant permissions
Full permission arrays
Role definitions
Mutable user profile data
Password information
Refresh credentials
Sensitive secrets
```

Tenant and authorization claims must not be prematurely embedded because their semantics belong to later features.

---

# 15. Token Signing

If signed tokens are used:

- Use a mature maintained implementation.
- Use a strong signing strategy.
- Secrets/keys must come from configuration/environment.
- Never hard-code signing secrets.
- Never commit secrets to source control.
- Validate token signature.
- Validate token expiration.
- Validate expected token type/purpose where applicable.
- Reject malformed tokens safely.

The implementation plan must identify the signing algorithm and key-management approach before coding.

Do not implement custom token cryptography.

---

# 16. Access Token Lifetime

Access credentials should be short-lived.

The exact lifetime must be explicitly approved before implementation.

Do not hard-code arbitrary security durations throughout the codebase.

Token/session duration configuration should be centralized.

The implementation plan must propose:

```text
Access token lifetime
Refresh/session lifetime
```

with rationale.

---

# 17. Refresh Credential Security

If refresh tokens are used, they must be treated as high-value secrets.

Requirements:

- Generate using cryptographically secure randomness or an approved token mechanism.
- Never log refresh tokens.
- Never expose refresh-token values through errors.
- Do not persist raw refresh tokens if the approved design supports storing only a secure hash.
- Validate expiration.
- Validate revocation.
- Bind the credential to its server-side session.
- Rotate credentials if rotation is part of the approved design.

A database compromise should not trivially reveal reusable raw refresh credentials.

---

# 18. Refresh Token Rotation

The implementation plan must explicitly decide whether refresh credentials rotate.

Recommended security direction:

```text
Refresh Request
     ↓
Validate Current Refresh Credential
     ↓
Validate Session
     ↓
Invalidate / Replace Existing Refresh Credential
     ↓
Issue New Access Token
     ↓
Issue New Refresh Credential
```

This reduces the usefulness of stolen refresh credentials.

However, rotation semantics must be deliberately designed and tested.

Do not silently introduce complex token-family or reuse-detection infrastructure unless it is approved for Feature 3.

---

# 19. Refresh Flow

Conceptually:

```text
Client
   ↓
Refresh Credential
   ↓
Authentication Handler
   ↓
Authentication Service
   ↓
Validate Credential
   ↓
Load Session
   ↓
Check Revocation
   ↓
Check Expiration
   ↓
Rotate Credential if approved
   ↓
Issue New Access Token
   ↓
Return Updated Authentication Data
```

Expected failures should use Feature 1 error codes.

Relevant existing codes include:

```text
SESSION_EXPIRED
SESSION_REVOKED
INVALID_CREDENTIALS
INVALID_REQUEST
```

Do not expose internal session/token details.

---

# 20. Logout / Revocation

Logout must revoke the corresponding server-side session according to the approved session model.

Conceptually:

```text
Authenticated Client
     ↓
POST /api/v1/auth/logout
     ↓
Authentication Service
     ↓
Revoke Session
```

After revocation:

- Refresh must fail.
- The session must be considered revoked.
- Middleware/session validation behavior must follow the approved access-token strategy.

If access tokens are stateless and short-lived, logout may not instantly invalidate an already-issued access token unless middleware performs session-state validation.

This tradeoff must be explicitly resolved during planning.

Do not silently claim instant access-token revocation unless the implementation actually provides it.

---

# 21. Authentication Middleware

Feature 3 introduces authentication middleware.

Its responsibility is:

> Determine whether an incoming request has valid authentication and establish the authenticated principal.

Conceptually:

```text
Request
   ↓
Extract Access Credential
   ↓
Validate Credential
   ↓
Validate Required Session State
   ↓
Construct AuthenticatedPrincipal
   ↓
Attach Principal to Request Context
   ↓
Next Handler
```

Middleware must not:

- Query tenant membership.
- Check roles.
- Check permissions.
- Decide resource ownership.
- Implement business authorization.

Those belong to later features.

---

# 22. Authenticated Principal

Feature 3 must define a typed authentication context.

Conceptually:

```go
type Principal struct {
    UserID    ...
    SessionID ...
}
```

The exact ID types should reuse the approved Feature 2/session ID strategy.

Provide a safe helper for retrieving the principal from request context.

Avoid:

- Raw string context keys scattered throughout handlers.
- Global authentication state.
- Trusting user IDs supplied directly by client headers.

The principal must only be created after successful authentication.

---

# 23. Authentication Header / Credential Transport

The exact client transport mechanism must be explicitly approved.

For access tokens, a typical API approach is:

```text
Authorization: Bearer <access-token>
```

The implementation must strictly parse the expected scheme.

Malformed or missing authentication should fail safely.

The refresh-token transport strategy must also be explicitly decided.

Possible approaches include:

- Secure HttpOnly cookie.
- Explicit response/request token field for native/mobile clients.

Because this project includes mobile-client use cases, the final design must consider both browser dashboard and mobile application behavior.

Do not silently choose browser-only storage semantics.

---

# 24. Cookie Security

If cookies are used for refresh/session credentials, the implementation plan must define:

- `HttpOnly`
- `Secure`
- `SameSite`
- Cookie path
- Cookie lifetime
- CSRF implications

Do not put access/refresh credentials into insecure cookies.

Do not introduce cookie-based authentication without addressing CSRF behavior.

---

# 25. Authentication Error Contract

Feature 3 must reuse Feature 1.

Relevant existing codes include:

```text
INVALID_REQUEST
VALIDATION_FAILED
INVALID_CREDENTIALS
SESSION_EXPIRED
SESSION_REVOKED
SERVICE_UNAVAILABLE
INTERNAL_ERROR
```

Potential additional code:

```text
USER_DISABLED
```

only if explicitly approved.

Expected conceptual mappings:

```text
Malformed login request
→ INVALID_REQUEST / VALIDATION_FAILED

Unknown email
→ INVALID_CREDENTIALS
→ 401

Wrong password
→ INVALID_CREDENTIALS
→ 401

Invalid access credential
→ INVALID_CREDENTIALS or approved authentication-specific behavior

Expired session
→ SESSION_EXPIRED
→ 401

Revoked session
→ SESSION_REVOKED
→ 401

Temporary dependency failure
→ SERVICE_UNAVAILABLE
→ 503

Unexpected failure
→ INTERNAL_ERROR
→ 500
```

Do not inspect `err.Error()` to classify these failures.

---

# 26. Session Database Requirements

Create only the migration(s) required for authentication/session persistence.

A likely table is:

```text
sessions
```

The exact schema depends on the approved design.

Conceptually it may include:

```text
id
user_id
refresh_token_hash
created_at
expires_at
revoked_at
updated_at
```

Database requirements:

- Primary key.
- Foreign key to users.
- Required expiration.
- Required credential-hash field where applicable.
- Revocation representation.
- Appropriate indexes for lookup.
- Parameterized repository queries.

Do not create:

```text
tenant memberships
roles
permissions
role assignments
permission assignments
audit tables
```

as part of Feature 3.

---

# 27. Transaction Boundaries

Authentication operations involving multiple persistence changes may require transactions.

For example, refresh rotation may involve:

```text
Validate current session
+
Replace refresh credential
```

The implementation plan must identify where atomicity is required.

Do not add a generic transaction framework unless a concrete Feature 3 operation requires it.

Transactions belong around business operations that require atomic persistence, not around every repository method by default.

---

# 28. Rate Limiting and Brute-Force Protection

Authentication endpoints are security-sensitive.

The implementation plan must address brute-force protection.

At minimum, the plan must state whether rate limiting is:

1. Implemented within Feature 3, or
2. Explicitly deferred to shared HTTP/security infrastructure.

Do not silently implement a distributed rate-limiting platform as part of Feature 3.

If rate limiting is deferred, the security limitation must be documented.

Feature 1 already contains:

```text
RATE_LIMITED
```

for future centralized handling.

---

# 29. Timing and Enumeration Considerations

Login behavior should avoid unnecessary account-enumeration differences.

The implementation should consider whether unknown-user authentication performs a safe password-verification-equivalent operation to reduce obvious timing differences.

The exact mitigation should be proposed in the implementation plan.

Do not implement unsafe shortcuts merely because the user does not exist.

Do not claim perfect timing resistance; the goal is to avoid obvious distinguishers.

---

# 30. Secrets and Configuration

Authentication secrets and security parameters must not be scattered through code.

Configuration may include:

```text
Token signing secret/key
Access token lifetime
Session/refresh lifetime
Password verification configuration if needed
Cookie settings if used
```

Requirements:

- No hard-coded production secrets.
- No secrets committed to source control.
- Validate required security configuration at startup when integration exists.
- Tests should use dedicated test configuration.

Feature 3 must not create an unnecessarily large configuration framework if one does not yet exist.

---

# 31. Logging Safety

Feature 3 must never log:

```text
Plaintext password
Password hash
Access token
Refresh token
Session secret
Signing secret/private key
```

If logging infrastructure does not yet exist, do not introduce a full logging platform merely for Feature 3.

Tests/review should still ensure authentication code does not intentionally expose secrets.

---

# 32. Public Authentication Response

The exact response shape must be approved based on the credential transport strategy.

Conceptually, a successful login may return safe information such as:

```json
{
  "user": {
    "id": "...",
    "email": "...",
    "status": "ACTIVE"
  },
  "access_token": "...",
  "expires_in": 900
}
```

A refresh credential may be:

- Returned explicitly for mobile clients, or
- Set through a secure cookie for browser clients,

depending on the approved strategy.

Never return:

```text
password
password_hash
refresh_token_hash
signing keys
database details
internal session state
```

Do not finalize the response contract until the browser/mobile token transport decision is approved.

---

# 33. Proposed API Endpoints

The minimum candidate authentication endpoints are:

```text
POST /api/v1/auth/login
POST /api/v1/auth/refresh
POST /api/v1/auth/logout
```

Feature 3 may also introduce middleware-protected testable behavior, but it must not create unrelated user-management endpoints.

No endpoint should be added for:

```text
tenant switching
role assignment
permission management
password reset
email verification
MFA
```

---

# 34. TDD Requirements

Feature 3 must follow pure test-first development.

Recommended sequence:

1. Inspect completed Feature 1 and Feature 2 implementations.
2. Resolve all Feature 3 security decisions.
3. Produce an approved implementation plan.
4. Write failing password-verification tests.
5. Write failing login-service tests.
6. Write failing generic-invalid-credential tests.
7. Write failing disabled-user tests.
8. Write failing session-model/repository tests.
9. Write failing access-token tests.
10. Write failing refresh/session credential tests.
11. Write failing refresh-rotation tests if approved.
12. Write failing logout/revocation tests.
13. Write failing authentication middleware tests.
14. Write failing authenticated-principal/context tests.
15. Write failing handler/API tests.
16. Write security-focused tests.
17. Implement the minimum production code required to satisfy each failing test.
18. Add migration/repository implementation.
19. Run focused Feature 3 tests.
20. Run PostgreSQL integration tests where required.
21. Run the full repository test suite.
22. Conduct independent security/specification review.
23. Stop before tenant membership or authorization implementation begins.

Do not write all authentication production code first and backfill tests.

---

# 35. Required Service Tests

Test at minimum:

## Successful login

- Existing active user can authenticate with the correct password.
- Feature 2 email normalization is reused.
- Password verification is invoked.
- A session is created.
- Authentication credentials are issued.
- Safe user information is returned.

## Invalid credentials

- Unknown email returns `INVALID_CREDENTIALS`.
- Wrong password returns `INVALID_CREDENTIALS`.
- Public behavior does not reveal which condition occurred.
- No session is created after credential failure.
- No access token is issued after credential failure.
- No refresh credential is issued after credential failure.

## Disabled user

- Disabled user cannot authenticate.
- Public error follows the approved disabled-user policy.
- No session is created.
- No credentials are issued.

## Repository failures

- Expected application errors remain discoverable through wrapping.
- Unexpected errors are wrapped using `%w`.
- Database details do not leak.

## Session creation

- Correct user ID is associated with the session.
- Expiration follows centralized configuration.
- Secret/hash handling follows the approved design.

---

# 36. Required Access Token Tests

If signed access tokens are approved, test:

- Valid token can be created.
- Valid token can be parsed.
- Signature is verified.
- User ID is preserved.
- Session ID is preserved.
- Issued-at value is correct.
- Expiration is correct.
- Expired token is rejected.
- Malformed token is rejected.
- Tampered token is rejected.
- Wrong signing key is rejected.
- Missing required claims are rejected.
- Unexpected token type/purpose is rejected where applicable.
- No password or sensitive credential information appears in claims.

Do not merely test that a token string was generated.

---

# 37. Required Refresh Tests

Test:

- Valid refresh credential succeeds.
- Expired session fails with `SESSION_EXPIRED`.
- Revoked session fails with `SESSION_REVOKED`.
- Invalid refresh credential fails safely.
- Refresh credential is bound to the correct session.
- Raw stored refresh credentials are not persisted if hashing is approved.
- Rotation occurs correctly if approved.
- Old credential behavior after rotation matches the approved policy.
- New access token has the expected lifetime.
- Refresh does not require the user's password.
- Refresh does not introduce roles/permissions/tenant context.

---

# 38. Required Logout Tests

Test:

- Valid logout revokes the session.
- Revoked session cannot refresh.
- Repeated logout behavior is explicitly defined and tested.
- Logout does not expose session internals.
- Logout cannot revoke an arbitrary session merely from untrusted client-supplied identity data.
- Session ownership/authentication requirements are enforced according to the approved design.

---

# 39. Required Middleware Tests

Test:

- Valid access credential establishes the principal.
- Principal contains correct user ID.
- Principal contains correct session ID where applicable.
- Missing credential is rejected for protected endpoints.
- Malformed authorization header is rejected.
- Wrong authentication scheme is rejected.
- Expired access credential is rejected.
- Tampered access credential is rejected.
- Invalid signature is rejected.
- Revoked-session behavior follows the approved middleware strategy.
- Handler cannot spoof principal using a client-controlled header.
- Middleware does not perform role or tenant authorization.

---

# 40. Required Handler/API Tests

Test:

## Login

- Valid request succeeds.
- Malformed JSON returns `INVALID_REQUEST`.
- Missing email/password returns validation failure.
- Invalid credentials return HTTP 401.
- Unknown email and wrong password produce equivalent public errors.
- Password never appears in the response.
- Password hash never appears in the response.

## Refresh

- Valid refresh succeeds.
- Invalid refresh fails safely.
- Expired session returns HTTP 401.
- Revoked session returns HTTP 401.
- Sensitive session information is absent.

## Logout

- Valid logout succeeds according to approved status semantics.
- Session is revoked.
- Invalid authentication fails safely.

## General

- Responses use JSON where applicable.
- Feature 1 error envelope is preserved.
- Unexpected errors return generic `INTERNAL_ERROR`.
- Temporary dependency failures return `SERVICE_UNAVAILABLE`.
- Internal diagnostics are absent.

---

# 41. Required Security Tests

Explicitly verify:

- Passwords are never logged or returned.
- Password hashes are never returned.
- Access tokens are not logged.
- Refresh credentials are not logged.
- Raw refresh credentials are not persisted if the approved design hashes them.
- Signing secrets are not embedded in source constants.
- Tampered tokens are rejected.
- Expired tokens are rejected.
- Revoked sessions cannot refresh.
- Generic login errors reduce account enumeration.
- No session is created after failed authentication.
- Client input cannot choose token signing algorithms.
- Client input cannot choose token expiration.
- Client input cannot supply arbitrary authenticated user IDs.
- Authentication middleware does not trust spoofed headers.
- SQL remains parameterized.
- Raw database errors never reach clients.
- Tenant IDs, roles, and permissions are not prematurely trusted from client credentials.
- Sensitive token/session fields are absent from public JSON.

---

# 42. Repository Tests

Session repository tests should cover:

- Session creation.
- Lookup.
- Expiration data.
- Revocation.
- Refresh-credential hash persistence where applicable.
- Rotation/update where applicable.
- Foreign-key behavior with users.
- Missing-session behavior.
- Database error wrapping.
- Parameterized SQL.
- Correct timestamp persistence.

PostgreSQL integration tests should verify database constraints and behavior that mocks cannot prove.

---

# 43. Migration Tests

Where migration testing infrastructure exists or is approved, verify:

- Session table can be created.
- Required constraints exist.
- Foreign key to users exists.
- Required indexes exist.
- Required fields cannot be null.
- Expiration data is required.
- Revocation representation works.
- Rollback works if rollback migrations are part of the approved migration approach.

Do not add tenant/role/permission schema.

---

# 44. Performance Considerations

Authentication code should be secure before being optimized.

However:

- Password verification is intentionally expensive.
- Database access should use appropriate indexed session/user lookups.
- Token validation should not perform unnecessary work.
- Do not introduce caching for credentials or password hashes in Feature 3 without a concrete requirement.
- Do not weaken password hashing to improve test or login speed.

Tests may use controlled test hashing configuration where the approved password implementation safely supports it.

---

# 45. Concurrency Considerations

Session refresh and rotation may receive concurrent requests.

If refresh-token rotation is approved, the implementation plan must consider what happens when the same refresh credential is submitted concurrently.

The database must remain the final authority for state transitions that require atomicity.

Do not assume:

```text
check then update
```

is safe under concurrency without appropriate persistence guarantees.

The implementation plan must identify the required transaction/atomic-update strategy.

---

# 46. Scope-Control Rules

Feature 3 must not become the entire Identity & Access system.

Do not implement:

```text
Tenant Membership
Tenant Context
Tenant Switching
Roles
Permissions
RBAC
Authorization Middleware
Resource Authorization
Super Admin Authorization
Password Reset
Email Verification
MFA
OAuth
SSO
Audit Platform
Frontend Authentication UI
```

Do not add speculative claims to tokens for features that do not yet exist.

Keep authentication concerned with:

> Who is this caller?

Later features answer:

> Which tenant are they acting within?

and:

> What are they allowed to do?

---

# 47. Acceptance Criteria

Feature 3 is complete when:

- [ ] Login is implemented.
- [ ] Feature 2 email normalization is reused.
- [ ] Feature 2 password verification implementation is reused.
- [ ] Unknown email and wrong password return generic `INVALID_CREDENTIALS`.
- [ ] Disabled-user behavior follows the approved policy.
- [ ] Failed authentication never creates a session.
- [ ] Failed authentication never issues credentials.
- [ ] Persistent session representation exists.
- [ ] Session repository exists.
- [ ] Required session migration exists.
- [ ] Session expiration is implemented.
- [ ] Session revocation is implemented.
- [ ] Short-lived access credentials are implemented according to the approved strategy.
- [ ] Refresh/session credentials are implemented according to the approved strategy.
- [ ] Raw refresh credentials are not persisted if hashing is part of the approved design.
- [ ] Refresh expiration is enforced.
- [ ] Revoked sessions cannot refresh.
- [ ] Refresh rotation works if approved.
- [ ] Logout/revocation is implemented.
- [ ] Authentication middleware is implemented.
- [ ] Authenticated principal/context is implemented.
- [ ] Middleware does not perform tenant/role/permission authorization.
- [ ] Feature 1 centralized errors are reused.
- [ ] Application behavior does not depend on `err.Error()`.
- [ ] Expected errors survive `%w` wrapping.
- [ ] SQL is parameterized.
- [ ] Authentication secrets are not hard-coded.
- [ ] Passwords are never logged or returned.
- [ ] Password hashes are never returned.
- [ ] Access/refresh credentials are not leaked through errors.
- [ ] Raw database errors are not exposed.
- [ ] Login enumeration risk is considered and tested.
- [ ] Required service tests pass.
- [ ] Required repository tests pass.
- [ ] Required handler/API tests pass.
- [ ] Required middleware tests pass.
- [ ] Required token/session tests pass.
- [ ] Required security tests pass.
- [ ] PostgreSQL integration tests pass where applicable.
- [ ] Full repository test suite passes.
- [ ] No tenant membership functionality is implemented.
- [ ] No roles or permissions are implemented.
- [ ] No authorization enforcement is implemented.

---

# 48. Decisions Requiring Explicit Approval Before Coding

The implementation agent must not silently invent the following:

1. Authentication/session architecture.
2. Whether short-lived JWT access tokens are used.
3. Access-token signing algorithm.
4. Signing key/secret strategy.
5. Access-token lifetime.
6. Refresh/session credential design.
7. Refresh/session lifetime.
8. Whether raw refresh credentials are stored or only hashes.
9. Refresh-token generation strategy.
10. Refresh-token rotation policy.
11. Behavior of old refresh credentials after rotation.
12. Concurrent refresh behavior.
13. Whether refresh-token reuse detection is part of Feature 3.
14. Session ID strategy.
15. Session database fields.
16. Session revocation representation.
17. Logout semantics.
18. Whether logout instantly invalidates already-issued access tokens.
19. Whether middleware validates server-side session state on every request.
20. Disabled-user public error behavior.
21. Whether `USER_DISABLED` is required.
22. Access-token transport mechanism.
23. Refresh-token transport for mobile clients.
24. Refresh-token transport for browser/dashboard clients.
25. Whether secure HttpOnly cookies are used for browser refresh credentials.
26. Cookie `SameSite` policy if cookies are used.
27. CSRF strategy if cookie credentials are used.
28. Successful login response shape.
29. Successful refresh response shape.
30. Successful logout HTTP status.
31. Rate-limiting implementation or explicit deferral.
32. Login timing/enumeration mitigation.
33. Session cleanup/expired-session deletion strategy.
34. Token configuration/environment variable names.
35. Whether multiple simultaneous sessions per user are allowed.
36. Whether logout revokes one session or all user sessions.
37. Whether device metadata is deliberately excluded.
38. Confirmation that password reset is excluded.
39. Confirmation that email verification is excluded.
40. Confirmation that MFA/OAuth/SSO are excluded.

No production code should be generated until the security-sensitive decisions are approved.

---

# 49. Recommended Security Direction for the Planning Agent

The implementation agent may propose alternatives, but the preferred starting direction is:

```text
Authentication:
Email + password

Access credential:
Short-lived signed access token

Session:
Persistent server-side session

Refresh credential:
High-entropy opaque random token

Refresh storage:
Store only a secure hash of the refresh credential

Refresh behavior:
Rotate on successful refresh

Logout:
Revoke the server-side session

Principal:
UserID + SessionID

Authorization:
Deferred

Tenant context:
Deferred
```

This section is a **direction for planning**, not automatic approval of every implementation detail.

The planning agent must still justify concrete algorithms, lifetimes, storage, rotation, and transport choices.

---

# 50. Planning Prompt for Claude

```text
You are preparing the implementation plan for Epic 1, Feature 3 —
Authentication & Session Foundation in the Multi-Tenant Booking System.

First inspect and read:

1. Master Specification.
2. Epic 1 — Identity & Access specification.
3. Feature 1 — Application Error Infrastructure specification and implementation.
4. Feature 2 — User Identity specification and implementation.
5. Feature 3 — Authentication & Session Foundation specification.

Feature 1 and Feature 2 are complete.

Do NOT implement production code yet.

Your task is to produce a detailed implementation plan for Feature 3 only.

FEATURE 3 PURPOSE:

Authenticate an existing user, establish and maintain a secure server-recognized
session, issue short-lived access credentials, support secure refresh, support
logout/revocation, and establish authenticated request context.

PRESERVE THE ARCHITECTURE:

API → Handler → Service → Repository → Database

Authentication middleware may establish authenticated request context but is
not an authorization layer.

FEATURE 3 INCLUDES:

- Login.
- Password verification using Feature 2.
- Generic invalid-credential behavior.
- Disabled-user authentication behavior.
- Persistent sessions.
- Session repository.
- Access-token issuance.
- Refresh/session credentials.
- Refresh.
- Refresh rotation if approved.
- Session expiration.
- Logout/revocation.
- Authentication middleware.
- Authenticated principal/context.
- Required migrations.
- Security configuration.
- Pure TDD.

FEATURE 3 EXCLUDES:

- Tenant membership.
- Tenant context.
- Tenant switching.
- Roles.
- Permissions.
- RBAC.
- Authorization.
- Password reset.
- Email verification.
- MFA.
- OAuth.
- SSO.
- Audit platform.
- Frontend authentication UI.

CRITICAL RULE:

Do not silently make security-sensitive decisions.

Before proposing implementation files, inspect the existing Feature 2
password implementation, ID strategy, email normalization, repository style,
database driver, migration strategy, and project conventions.

Your plan must explicitly address:

1. Files to create or modify.
2. Authentication service responsibilities.
3. Session domain model.
4. Session database schema.
5. Session repository interface and PostgreSQL implementation.
6. Access-token strategy.
7. Signing algorithm and key strategy.
8. Access-token claims.
9. Access-token lifetime.
10. Refresh credential generation.
11. Refresh credential persistence.
12. Whether raw refresh credentials are ever stored.
13. Refresh lifetime.
14. Refresh rotation.
15. Concurrent refresh behavior.
16. Session revocation.
17. Logout semantics.
18. Whether already-issued access tokens remain valid after logout.
19. Middleware session-validation strategy.
20. Authenticated principal/context design.
21. Mobile token transport.
22. Browser/dashboard token transport.
23. Cookie and CSRF implications if cookies are proposed.
24. Generic invalid-credential behavior.
25. Disabled-user public behavior.
26. Login timing/account-enumeration mitigation.
27. Rate limiting or explicit deferral.
28. Secret/configuration management.
29. Required Feature 1 error codes.
30. Whether any new error code is genuinely required.
31. Transaction boundaries.
32. Concurrency risks.
33. Exact API endpoints.
34. Success response/status contracts.
35. Detailed TDD sequence.
36. Service tests.
37. Token tests.
38. Refresh tests.
39. Repository/PostgreSQL tests.
40. Handler/API tests.
41. Middleware/context tests.
42. Security tests.
43. Scope boundaries.

Do not write production code.

Do not redesign the project.

Do not introduce microservices.

Do not introduce tenant, role, permission, or authorization logic.

Do not implement speculative authentication features.

Prefer the minimum secure implementation that satisfies Feature 3.

End with:

DECISIONS REQUIRING APPROVAL

List every unresolved security, API, persistence, configuration, or business
decision that must be approved before implementation begins.
```

---

# 51. Implementation Prompt After Plan Approval

```text
The Epic 1, Feature 3 — Authentication & Session Foundation implementation
plan and its security decisions have been approved.

Implement only Feature 3 according to:

1. Master Specification.
2. Epic 1 specification.
3. Feature 1 specification and existing implementation.
4. Feature 2 specification and existing implementation.
5. Feature 3 specification.
6. The approved Feature 3 implementation plan.
7. The approved Feature 3 decision addendum.

Follow pure TDD:

Define behavior
→ Write failing test
→ Confirm failure
→ Implement minimum production code
→ Pass test
→ Refactor only when justified
→ Add required edge/security cases

Preserve:

API → Handler → Service → Repository → Database

Reuse Feature 1's centralized error infrastructure.

Reuse Feature 2's:
- User model.
- User repository behavior where applicable.
- Email normalization.
- Password hashing/verification implementation.
- User status model.

Do not create duplicate identity or password infrastructure.

SECURITY REQUIREMENTS ARE MANDATORY:

- Never log passwords.
- Never log password hashes.
- Never log access tokens.
- Never log refresh credentials.
- Never expose secrets through errors.
- Never store plaintext passwords.
- Never expose password hashes.
- Never hard-code production signing secrets.
- Never trust user IDs from arbitrary client headers.
- Validate token signature and expiration.
- Enforce session expiration.
- Enforce session revocation.
- Store refresh credentials according to the approved secure strategy.
- Use cryptographically secure token generation where required.
- Use parameterized SQL.
- Preserve generic invalid-credential behavior.
- Preserve Feature 1 safe error responses.
- Use %w and errors.As-compatible wrapping.
- Do not classify errors by parsing err.Error().

Implement only the approved Feature 3 scope.

DO NOT IMPLEMENT:

- Tenant membership.
- Tenant context.
- Tenant switching.
- Roles.
- Permissions.
- RBAC.
- Authorization.
- Password reset.
- Email verification.
- MFA.
- OAuth.
- SSO.
- Audit platform.
- Frontend authentication UI.

Do not make unrelated refactors.

After implementation:

1. Run focused authentication tests.
2. Run session repository/PostgreSQL integration tests.
3. Run middleware tests.
4. Run security tests.
5. Run the full repository test suite with:

   go test ./...

6. Report:
   - Files created.
   - Files modified.
   - Migrations created.
   - Dependencies added.
   - Tests added.
   - Focused test results.
   - Integration test results.
   - Full test results.
   - Any deviations from the approved plan.
   - Any security limitation intentionally deferred.

Stop after Feature 3 is complete.
Do not begin Feature 4.
```

---

# 52. Review Prompt for Claude Haiku

```text
Review only. Do not modify files.

Review the completed Epic 1, Feature 3 — Authentication & Session Foundation
implementation.

Use as authoritative requirements:

1. Master Specification.
2. Epic 1 — Identity & Access specification.
3. Feature 1 — Application Error Infrastructure specification.
4. Feature 2 — User Identity specification.
5. Feature 3 — Authentication & Session Foundation specification.
6. Approved Feature 3 implementation plan.
7. Approved Feature 3 security/decision addendum.

Inspect the actual implementation and tests.

ARCHITECTURE

Verify:

- Correct Handler → Service → Repository separation.
- Authentication middleware is authentication only.
- No tenant authorization exists.
- No role/permission authorization exists.
- No SQL in handlers/services.
- No HTTP behavior in repositories.
- Feature 2 identity/password infrastructure is reused rather than duplicated.

LOGIN

Verify:

- Correct email normalization.
- Correct password verification.
- Unknown email and wrong password use generic INVALID_CREDENTIALS behavior.
- Failed login never creates a session.
- Failed login never issues credentials.
- Disabled-user behavior matches the approved policy.
- Account-enumeration mitigations match the approved plan.

SESSION SECURITY

Verify:

- Session persistence matches the approved schema.
- Session expiration is enforced.
- Session revocation is enforced.
- Logout semantics match the approved design.
- Multiple-session behavior matches the approved design.
- Session ownership cannot be spoofed by client input.

ACCESS TOKENS

Verify:

- Approved signing algorithm is used.
- Signing secrets/keys are not hard-coded.
- Signature validation occurs.
- Expiration validation occurs.
- Required claims are validated.
- Claims contain only approved authentication data.
- Roles/permissions/tenant authorization are not prematurely embedded.
- Tampered tokens are rejected.

REFRESH SECURITY

Verify:

- Refresh credentials are securely generated.
- Persistence follows the approved hash/storage strategy.
- Raw refresh credentials are not persisted if prohibited by the approved design.
- Refresh expiration is enforced.
- Revoked sessions cannot refresh.
- Rotation matches the approved plan.
- Old-token behavior matches the approved plan.
- Concurrent refresh behavior is safe according to the approved design.
- Refresh credentials are not logged or leaked.

MIDDLEWARE

Verify:

- Valid credentials establish a typed principal.
- Principal contains only approved identity/session information.
- Missing/malformed credentials fail safely.
- Client-controlled headers cannot spoof authenticated identity.
- Middleware does not perform tenant/role/permission authorization.
- Revocation behavior matches the approved architecture.

ERROR HANDLING

Verify:

- Feature 1 infrastructure is reused.
- INVALID_CREDENTIALS is used correctly.
- SESSION_EXPIRED is used correctly.
- SESSION_REVOKED is used correctly.
- SERVICE_UNAVAILABLE is only intentionally classified.
- Unexpected errors safely become INTERNAL_ERROR.
- Application logic does not parse err.Error().
- %w wrapping preserves expected application errors.
- No internal diagnostic data reaches public responses.

SECURITY

Verify:

- Passwords are never logged or returned.
- Password hashes are never returned.
- Access tokens are not logged.
- Refresh credentials are not logged.
- Signing secrets are not exposed.
- SQL is parameterized.
- Raw database errors are not exposed.
- Client input cannot select signing algorithms.
- Client input cannot control credential lifetimes.
- Sensitive token/session information is absent from public responses.

TESTING

Verify required coverage for:

- Login.
- Invalid credentials.
- Disabled users.
- Sessions.
- Access tokens.
- Expiration.
- Refresh.
- Rotation where applicable.
- Revocation.
- Logout.
- Middleware.
- Principal/context.
- PostgreSQL behavior.
- Security edge cases.
- Concurrency behavior where required.

SCOPE

Confirm Feature 3 did NOT implement:

- Tenant membership.
- Tenant context.
- Tenant switching.
- Roles.
- Permissions.
- RBAC.
- Authorization.
- Password reset.
- Email verification.
- MFA.
- OAuth.
- SSO.
- Frontend authentication.

CLASSIFY FINDINGS ONLY AS:

CRITICAL
IMPORTANT
MINOR

Distinguish genuine specification/security violations from stylistic
preferences.

Do not recommend architectural redesign merely because another approach
is possible.

Do not recommend unnecessary abstractions.

If behavior works but explicitly required tests are missing, state that
the feature is not specification-complete until those tests exist.

Conclude with one of:

APPROVED
APPROVED AFTER IMPORTANT FIXES
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
Feature 3 — Authentication & Session Foundation

PREVIOUSLY COMPLETED:
F1 — Application Error Infrastructure
F2 — User Identity

ARCHITECTURE:
API → Handler → Service → Repository → Database

STYLE:
Modular Monolith

FEATURE 3 PURPOSE:
Prove user identity and establish/maintain authenticated sessions.

CORE FLOW:
Email + Password
→ Verify User
→ Verify Password
→ Verify Status
→ Create Session
→ Issue Credentials

AUTHENTICATED REQUEST:
Access Credential
→ Authentication Middleware
→ Authenticated Principal
→ Handler

AUTHENTICATED PRINCIPAL:
UserID
SessionID

DO NOT ADD:
Tenant permissions
Roles
Permission arrays
Authorization decisions

SESSION:
Persistent and server-recognized.

ACCESS:
Short-lived credential according to approved strategy.

REFRESH:
Secure longer-lived session credential according to approved strategy.

LOGOUT:
Revoke server-side session according to approved semantics.

ERRORS:
Reuse Feature 1.

Relevant codes:
INVALID_REQUEST
VALIDATION_FAILED
INVALID_CREDENTIALS
SESSION_EXPIRED
SESSION_REVOKED
SERVICE_UNAVAILABLE
INTERNAL_ERROR

Potential:
USER_DISABLED only if explicitly approved.

SECURITY:
Never expose whether email or password caused normal credential failure.
Never log credentials.
Never hard-code signing secrets.
Never store plaintext passwords.
Never expose password hashes.
Never expose raw refresh-token storage values.
Validate token signature and expiration.
Enforce session expiration and revocation.
Use parameterized SQL.
Never trust client-supplied user identity headers.

TDD:
Tests first.

TEST:
Login
Invalid credentials
Disabled users
Session creation
Access credentials
Expiration
Refresh
Rotation
Revocation
Logout
Middleware
Principal/context
Repository
PostgreSQL constraints
Security cases

OUT OF SCOPE:
Tenant membership
Tenant context
Roles
Permissions
Authorization
Password reset
Email verification
MFA
OAuth
SSO
Audit platform
Frontend authentication UI

IMPORTANT:
Do not silently choose security-sensitive token/session decisions.

Planning must resolve:
- Access-token strategy.
- Signing algorithm.
- Token lifetime.
- Refresh design.
- Refresh lifetime.
- Refresh storage.
- Rotation.
- Concurrent refresh.
- Revocation.
- Logout semantics.
- Middleware/session validation.
- Mobile transport.
- Browser transport.
- Cookie/CSRF behavior.
- Disabled-user behavior.
- Rate limiting.
- Enumeration mitigation.
- Multiple-session behavior.

STOP:
After Feature 3 is complete, do not begin Feature 4.
```