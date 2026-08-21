Multi-Tenant Booking System
Epic 1 — Feature 2: User Identity

Document Type: Feature Implementation Specification / Context Restoration Document
Project: Multi-Tenant Booking System
Epic: Epic 1 — Identity & Access
Feature: Feature 2 — User Identity
Backend: Go
Database: PostgreSQL
Architecture: Modular Monolith
Request Flow: API → Handler → Service → Repository → Database
Development Method: Pure TDD / Test-First Development

1. Purpose

This document is the authoritative feature-level specification and context-restoration document for Epic 1, Feature 2 — User Identity.

Feature 2 establishes the persistent user identity foundation required by later Identity & Access features.

It builds on:

Master Project Specification.
Epic 1 — Identity & Access specification.
Feature 1 — Application Error Infrastructure specification.

If requirements are ambiguous, the implementation agent must identify the ambiguity rather than silently inventing a business rule.

2. Architecture Context

The backend architecture is:

API → Handler → Service → Repository → Database

A module is an organizational boundary, not another architectural layer.

Feature 2 belongs to the Identity module and follows the same separation:

HTTP/API
    ↓
Identity Handler
    ↓
Identity Service
    ↓
User Repository
    ↓
PostgreSQL
Handler Responsibilities

Handlers are responsible for:

Parsing HTTP requests.
Validating request shape.
Parsing route parameters.
Calling the service layer.
Returning HTTP responses.
Using the centralized error infrastructure from Feature 1.

Handlers must not contain:

SQL.
Password hashing.
Authentication logic.
Authorization decisions.
Business rules that belong in the service.
Service Responsibilities

The service layer is responsible for:

Enforcing user identity business rules.
Coordinating domain-level validation.
Normalizing identity values according to the approved policy.
Coordinating password hashing.
Coordinating repository operations.
Returning stable application errors.

Services must not contain SQL.

Repository Responsibilities

Repositories are responsible for:

Persisting users.
Retrieving users.
Executing parameterized SQL.
Translating expected persistence conditions into stable application errors where appropriate.

Repositories must not:

Make authorization decisions.
Implement HTTP concerns.
Return raw database errors to API consumers.
3. Feature Goal

Implement the minimum secure user identity foundation required before authentication and authorization can be built.

The feature provides:

User Identity
├── Stable User ID
├── Email Identity
├── Password Credential Hash
├── User Status
├── CreatedAt
└── UpdatedAt

The feature must support:

Creating a user.
Looking up a user by ID.
Looking up a user by email internally where required.
Representing user status.
Preventing duplicate email identities.
Securely hashing passwords before persistence.
Ensuring password hashes never appear in public responses.
4. Feature Scope
In Scope

Feature 2 includes:

User domain/model.
users persistence.
User repository.
PostgreSQL repository implementation.
Identity/User service.
User creation.
User lookup by ID.
Internal lookup by email where required.
User status representation.
Password hashing.
Email validation.
Email uniqueness.
Required database migrations.
Handler/API endpoints approved for this feature.
Unit tests.
Repository tests where applicable.
Handler/API tests.
Security tests.
Explicitly Out of Scope

Do not implement:

Login.
Password verification as an authentication flow.
JWT.
Access tokens.
Refresh tokens.
Sessions.
Authentication middleware.
Logout.
Session or token revocation.
Tenant membership.
Tenant authorization.
Roles.
Permissions.
Authorization policies.
Audit infrastructure.
Frontend authentication.

Feature 2 creates identity and stored credentials.

Authentication belongs to a later feature.

5. User Data Model Requirements

The user concept must contain at least:

User
├── ID
├── Email
├── PasswordHash
├── Status
├── CreatedAt
└── UpdatedAt

The exact Go types and SQL types should follow approved project conventions.

5.1 User ID

Use the project's established ID strategy.

Do not introduce a second incompatible identity strategy.

Malformed externally supplied IDs must be rejected at the appropriate boundary.

5.2 Email

Email is the primary identity value for this feature.

Requirements:

Required during user creation.
Validated.
Stored consistently.
Uniqueness enforced by the database.
Duplicate creation maps to USER_ALREADY_EXISTS.

The exact email normalization policy must be explicitly documented before implementation.

At minimum, trivial case variations must not bypass the intended uniqueness rule.

5.3 Password Hash

Passwords must never be stored in plaintext.

Only a secure password hash may be persisted.

The password hash must never:

Appear in public JSON responses.
Be logged.
Be included in error messages.
Be returned in public DTOs.
5.4 User Status

Feature 2 establishes user status because later authentication must be able to account for disabled users.

Before implementation, explicitly document:

Initial allowed status values.
Default status for newly created users.
Whether status mutation belongs to Feature 2.

Do not invent a complex user lifecycle or state machine.

A minimal status model is preferred until future requirements require additional states.

6. Password Security Requirements

Use a well-established, deliberately slow password hashing algorithm designed specifically for password storage.

Do not use:

MD5
SHA256(password)
Reversible encryption
Plaintext storage
Custom cryptography

Requirements:

Reject invalid password input according to approved validation rules.
Hash the password before repository persistence.
Never store plaintext passwords.
Never return password hashes.
Never log passwords.
Never log password hashes.
Never include passwords or hashes in errors.
Do not implement custom cryptography.

A small password-hasher abstraction is acceptable when it provides real value for service testing.

Do not create unnecessary abstractions.

7. Email Uniqueness and Database Invariants

Email uniqueness is a database invariant.

Application-level pre-checks may improve user experience, but they are not sufficient because concurrent requests can bypass them.

The database must enforce the intended uniqueness rule.

Expected behavior:

Create User
    ↓
Duplicate Email
    ↓
Database Uniqueness Constraint
    ↓
Repository
    ↓
USER_ALREADY_EXISTS
    ↓
Feature 1 Error Mapping
    ↓
HTTP 409

Never expose:

PostgreSQL constraint names.
Raw duplicate-key errors.
Database driver messages.

All SQL must be parameterized.

Never construct SQL using string concatenation with user input.

8. Error Contract

Feature 2 must reuse the centralized error infrastructure from Feature 1.

Relevant existing error codes include:

VALIDATION_FAILED
INVALID_REQUEST


USER_NOT_FOUND
USER_ALREADY_EXISTS


SERVICE_UNAVAILABLE
INTERNAL_ERROR

Rules:

Do not create feature-local error string contracts.
Do not branch on err.Error().
Use typed application errors.
Use %w for contextual wrapping.
Use errors.As for error-chain discovery where required.

A new centralized error code may only be added when a genuine expected business failure cannot be represented by the existing shared contract.

9. User Creation Requirements

Expected flow:

Handler
    ↓
Parse Request
    ↓
Identity Service
    ↓
Validate Input
    ↓
Normalize Email
    ↓
Hash Password
    ↓
User Repository
    ↓
Parameterized INSERT
    ↓
PostgreSQL
Valid User Creation

For a valid request:

Password is hashed.
User is persisted.
Plaintext password is never stored.
Password hash is never returned.
Safe user data is returned.
Appropriate HTTP success status is returned.
Invalid Input

Return the appropriate centralized error:

VALIDATION_FAILED

or:

INVALID_REQUEST

depending on the established validation boundary.

Do not expose internal implementation details.

Duplicate Email

Return:

USER_ALREADY_EXISTS

The centralized error infrastructure must map this to:

HTTP 409 Conflict
Unexpected Failure

For unexpected repository or infrastructure failures:

Preserve internal diagnostic context through the error chain.
Do not expose the underlying error publicly.
Allow centralized error handling to return a safe response.
10. User Lookup Requirements

Feature 2 must support lookup by user ID.

Expected flow:

Handler
    ↓
Validate / Parse ID
    ↓
Identity Service
    ↓
User Repository
    ↓
Parameterized SELECT
    ↓
PostgreSQL
User Exists

Return a safe public user representation.

User Does Not Exist

Return:

USER_NOT_FOUND

Feature 1 must map this centrally to:

HTTP 404 Not Found
Malformed ID

Reject malformed IDs using the appropriate validation/request error.

Do not expose database details.

11. Internal Lookup by Email

A FindByEmail capability may exist internally for:

Duplicate handling.
Future authentication.

It must not automatically become a public endpoint.

The system should not unnecessarily expose account existence through a public email lookup API.

12. Public API Scope

The Epic specification may contain conceptual user endpoints, but Feature 2 must not automatically implement every endpoint.

A minimum candidate set is:

POST /users
GET  /users/:id

Before implementation, the planning agent must explicitly identify which endpoints are included.

Do not include:

GET /users/me

because authenticated request context does not yet exist.

Do not add administrative status-changing endpoints unless the actor and authorization rules are explicitly defined.

13. Public Response Security

Public user responses must never expose:

password
password_hash
tokens
session secrets
database errors
internal diagnostics

Use separate public response DTOs when necessary.

Do not rely solely on developer discipline such as:

Remember not to serialize the password hash.

Tests must explicitly verify that sensitive fields are absent.

14. Database Requirements

Create only the database migration(s) required for Feature 2.

The users table must enforce:

A stable primary key.
Required email.
Required password hash.
Required user status.
Required timestamps where established by project conventions.
Email uniqueness.

Do not create tables for:

Tenants
Roles
Permissions
Memberships
Sessions
Tokens
Audit Logs

Those belong to later features.

15. TDD Requirements

Feature 2 must follow pure test-first development.

Recommended sequence:

Inspect the completed Feature 1 error infrastructure.
Inspect existing repository conventions.
Resolve and document Feature 2 ambiguities.
Produce the implementation plan.
Write failing service/domain tests.
Write failing password hashing tests.
Write failing duplicate-email tests.
Write failing user lookup tests.
Implement the minimum production code required.
Add migration and repository implementation.
Add repository tests.
Add handler/API tests.
Add security tests.
Run focused tests.
Run the full repository test suite.
Conduct independent review.

Do not write all production code first and backfill tests afterward.

16. Required Test Coverage
Service Tests

Test at minimum:

Valid user creation succeeds.
Invalid email is rejected.
Required fields are validated.
Invalid password input is rejected according to approved rules.
Password is hashed before persistence.
Plaintext password is not persisted.
Duplicate email returns USER_ALREADY_EXISTS.
Wrapped repository errors preserve application error identity.
User lookup succeeds.
Missing user returns USER_NOT_FOUND.
Unexpected repository failures remain internally wrapped.
Repository Tests

Where applicable:

User can be inserted.
User can be retrieved by ID.
User can be retrieved by email where implemented.
Duplicate email violates the intended uniqueness invariant.
Duplicate condition maps to USER_ALREADY_EXISTS.
Missing user maps to USER_NOT_FOUND.
Password hash, not plaintext password, is persisted.
Handler/API Tests

For every endpoint implemented:

Valid request succeeds.
Invalid JSON is handled safely.
Validation failures use the standardized error contract.
Duplicate email returns HTTP 409.
Missing user returns HTTP 404.
Response JSON excludes passwords and password hashes.
Unexpected errors do not expose internal details.
Security Tests

Explicitly verify:

Plaintext passwords are never persisted.
Password hashes are never returned.
Passwords are never included in error messages.
Password hashes are never included in error messages.
Raw database details do not reach clients.
Email uniqueness follows the approved normalization policy.
17. Scope-Control Rules

Feature 2 must not become an excuse to implement the rest of Epic 1.

Do not add:

Login
JWT
Access Tokens
Refresh Tokens
Sessions
Authentication Middleware
Tenant Membership
Roles
Permissions
Authorization
Audit Logs
Frontend Authentication

Do not implement every conceptual endpoint from the Epic specification.

Implement only behavior explicitly required by Feature 2.

18. Acceptance Criteria

Feature 2 is complete when:

 A user identity representation exists.
 Required fields exist: ID, email, password hash, status, timestamps.
 User creation is implemented.
 User lookup by ID is implemented.
 Internal lookup by email exists where required.
 Email validation is implemented.
 Email uniqueness is enforced by the database.
 Duplicate email maps to USER_ALREADY_EXISTS.
 Missing user maps to USER_NOT_FOUND.
 Passwords are hashed before persistence.
 Plaintext passwords are never stored.
 Password hashes never appear in public responses.
 Passwords and hashes never appear in error messages.
 Database errors are not exposed publicly.
 Feature 1 centralized error infrastructure is reused.
 Expected errors survive %w wrapping and remain discoverable.
 SQL is parameterized.
 User status values and default are documented.
 No login, token, session, or authentication middleware functionality is implemented.
 No tenant membership, roles, permissions, or authorization functionality is implemented.
 Tests are written before production implementation.
 Service tests pass.
 Repository tests pass where applicable.
 Handler/API tests pass.
 Security tests pass.
 Full repository test suite passes.
19. Decisions Requiring Explicit Approval Before Coding

The planning agent must not silently invent the following:

Exact user ID strategy if not already established.
Exact email normalization policy.
Exact password validation requirements.
Initial user status values.
Default user status.
Whether status mutation belongs to Feature 2.
Exact public API endpoints included in this feature.

The agent may propose minimal options, but proposals must not automatically become approved business rules.

20. Planning Prompt for Claude
You are preparing to implement Epic 1, Feature 2 — User Identity in the
Feature 1 is complete and its centralized error infrastructure is the
required shared error contract.


Do NOT implement code yet.


Produce a detailed implementation plan for Feature 2 only.


Feature 2 includes:


- Persistent users.
- User identity.
- Email identity.
- Secure password credential storage.
- User creation.
- User lookup by ID.
- Internal lookup by email where required.
- User status.
- Email validation.
- Database-enforced email uniqueness.
- Required database migrations.
- Pure TDD.


Feature 2 explicitly excludes:


- Login.
- JWT/access tokens.
- Refresh tokens.
- Sessions.
- Authentication middleware.
- Logout.
- Tenant membership.
- Roles.
- Permissions.
- Authorization.
- Audit infrastructure.
- Frontend authentication.


Preserve the architecture:


API → Handler → Service → Repository → Database


A module is an organizational boundary, not another layer.


Before proposing implementation details, identify unresolved decisions in
the Feature 2 specification. Do not silently invent business rules.


Your plan must include:


1. Files to create or modify.
2. User/domain model.
3. Database schema and migration plan.
4. Repository responsibilities.
5. Service responsibilities and dependency boundaries.
6. Password hashing approach and justification.
7. Email normalization and uniqueness strategy.
8. User status values and default behavior.
9. Exact API endpoints proposed for Feature 2.
10. Existing application error codes that will be used.
11. Detailed test-first implementation sequence.
12. Service tests.
13. Repository tests.
14. Handler/API tests.
15. Security tests.
16. Scope boundaries and implementation risks.


Do not write production code.
Do not redesign the architecture.
Do not introduce microservices.
Do not add unnecessary abstractions.


End with a section titled:


DECISIONS REQUIRING APPROVAL


List every business or architectural decision that is not already
explicitly defined in the specifications.
21. Implementation Prompt After Plan Approval
The Feature 2 implementation plan has been approved.


Implement only Epic 1, Feature 2 — User Identity according to the
approved Feature 2 specification and implementation plan.


Follow pure TDD:


Define behavior
→ Write a failing test
→ Confirm the test fails
→ Implement the minimum code
→ Make the test pass
→ Refactor only when necessary
→ Add edge cases


Reuse Feature 1's centralized application error infrastructure.


Do not create feature-local error strings.


Do not determine application behavior by parsing err.Error().


Preserve:


API → Handler → Service → Repository → Database


Mandatory security requirements:


- Never store plaintext passwords.
- Use an established password hashing algorithm.
- Never expose password hashes in public responses.
- Never log passwords or password hashes.
- Never include passwords or hashes in error messages.
- Enforce email uniqueness in the database.
- Use parameterized SQL.
- Map duplicate users to USER_ALREADY_EXISTS.
- Map missing users to USER_NOT_FOUND.
- Preserve expected application errors through wrapping.


Implement only Feature 2.


Do not implement:


- Login.
- JWT or other access tokens.
- Refresh tokens.
- Sessions.
- Authentication middleware.
- Logout.
- Tenant membership.
- Roles.
- Permissions.
- Authorization.
- Audit infrastructure.
- Frontend authentication.


Do not make unrelated refactors.


After implementation:


1. Run focused Feature 2 tests.
2. Run required repository/integration tests.
3. Run the complete repository test suite.
4. Report:
   - Files changed.
   - Files created.
   - Migrations created.
   - Tests added.
   - Focused test results.
   - Full test results.
   - Any deviations from the approved plan.
22. Review Prompt for Claude Haiku
Review only. Do not modify any files.


Review the completed implementation of Epic 1, Feature 2 — User Identity.


Use these documents as the authoritative requirements:


1. Master Specification.
2. Epic 1 — Identity & Access specification.
3. Feature 1 — Application Error Infrastructure specification.
4. Feature 2 — User Identity specification.
5. The approved Feature 2 implementation plan.


Verify the following:


ARCHITECTURE
- Correct API → Handler → Service → Repository separation.
- No SQL in handlers or services.
- No password hashing in handlers or repositories.


USER IDENTITY
- User creation is correct.
- Lookup by ID is correct.
- Email validation is correct.
- Email normalization follows the approved policy.
- User status follows the approved model.


DATABASE
- Email uniqueness is enforced by the database.
- Duplicate database conditions map safely to USER_ALREADY_EXISTS.
- Missing users map to USER_NOT_FOUND.
- SQL is parameterized.


SECURITY
- Plaintext passwords are never persisted.
- Password hashes are never returned publicly.
- Passwords and hashes are absent from error responses.
- Internal database errors are not exposed.
- No unnecessary security-sensitive data is logged.


ERROR HANDLING
- Feature 1 infrastructure is reused.
- Expected errors remain discoverable through %w wrapping.
- Centralized error mapping is preserved.
- No application logic depends on err.Error().


TESTING
- TDD coverage exists for user creation.
- Password hashing is tested.
- Duplicate email handling is tested.
- User lookup is tested.
- Not-found behavior is tested.
- Handler/API behavior is tested.
- Sensitive response fields are tested for absence.
- Required edge cases are covered.


SCOPE
Confirm that the implementation does NOT introduce:


- Login.
- Tokens.
- Sessions.
- Authentication middleware.
- Tenant membership.
- Roles.
- Permissions.
- Authorization.
- Audit infrastructure.


Classify findings as only:


CRITICAL
IMPORTANT
MINOR


Distinguish genuine specification violations from stylistic preferences.


If required tests are missing, state clearly that the feature is not
specification-complete until those tests are added.


Do not recommend unnecessary abstractions, architectural redesign,
microservices, or unrelated improvements.


Do not modify files.
23. Context Restoration Summary
PROJECT:
Multi-Tenant Booking System


EPIC:
Epic 1 — Identity & Access


FEATURE:
Feature 2 — User Identity


ARCHITECTURE:
API → Handler → Service → Repository → Database


STYLE:
Modular Monolith


FEATURE PURPOSE:
Create the secure persistent user identity foundation.


IN SCOPE:
- Users.
- Email identity.
- Password hashing.
- User creation.
- User lookup.
- User status.
- Email uniqueness.
- Database migration.
- TDD.


OUT OF SCOPE:
- Login.
- Tokens.
- Sessions.
- Authentication middleware.
- Tenant membership.
- Roles.
- Permissions.
- Authorization.
- Audit infrastructure.
- Frontend authentication.


SECURITY:
- Never store plaintext passwords.
- Never expose password hashes.
- Never log passwords or hashes.
- Use parameterized SQL.
- Database enforces email uniqueness.
- Never expose database internals.


ERRORS:
Reuse Feature 1 centralized infrastructure.


Use:
- Typed application errors.
- %w for wrapping.
- errors.As for error-chain inspection.


Never parse err.Error() for application behavior.


IMPORTANT UNRESOLVED DECISIONS:
- User ID strategy if not already established.
- Email normalization policy.
- Password validation rules.
- Initial user status vocabulary.
- Default user status.
- Whether status mutation belongs to Feature 2.
- Exact public endpoint scope.


DO NOT:
Silently invent those decisions.
Implement the rest of Epic 1.
Redesign the architecture.
Introduce microservices.