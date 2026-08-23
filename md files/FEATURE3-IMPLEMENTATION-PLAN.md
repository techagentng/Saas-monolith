# EPIC 02 — FEATURE 3 IMPLEMENTATION PLAN

## Tenant Retrieval & Listing

**Planning Date:** 2026-08-23  
**Status:** PLANNING ONLY — DO NOT IMPLEMENT

---

# 1. Current Repository Findings

### 1.1 Tenant Model & Repository

**Location:** `internal/tenant/model/tenant.go`, `internal/tenant/repository/tenant_repository.go`

**Findings:**
- Tenant model contains: `ID`, `Name`, `Slug`, `Status`, `CreatedAt`, `UpdatedAt`
- Statuses: `StatusActive` ("ACTIVE"), `StatusDisabled` ("DISABLED")
- Repository interface currently provides:
  - `Create(ctx context.Context, tenant *model.Tenant) (*model.Tenant, error)`
  - `FindByID(ctx context.Context, id string) (*model.Tenant, error)`
- PostgreSQL implementation uses `dbtx` pattern supporting both standalone queries and transaction participation
- `FindByID` returns `CodeTenantNotFound` error when tenant not found (wrapped as AppError)
- Slug uniqueness enforced at database level; duplicate slug maps to `CodeTenantSlugTaken` (HTTP 409)

### 1.2 Membership Model & Repository

**Location:** `internal/tenant/model/membership.go`, `internal/tenant/repository/membership_repository.go`

**Findings:**
- TenantMembership model contains: `ID`, `TenantID`, `UserID`, `Status`, `CreatedAt`, `UpdatedAt`
- Statuses: `MembershipStatusActive` ("ACTIVE"), `MembershipStatusDisabled` ("DISABLED")
- Repository interface provides:
  - `Create(ctx context.Context, membership model.TenantMembership) (*model.TenantMembership, error)`
  - `FindByTenantAndUser(ctx context.Context, tenantID, userID string) (*model.TenantMembership, error)` — returns `nil` if not found (no error)
  - `ListByUser(ctx context.Context, userID string) ([]model.TenantMembership, error)` — **returns ALL memberships regardless of status**
  - `Disable(ctx context.Context, tenantID, userID string, now time.Time) error`
- Database schema:
  - Table: `tenant_memberships`
  - Indexes: `tenant_memberships_user_id_idx`, `tenant_memberships_tenant_id_idx`
  - UNIQUE constraint: `(tenant_id, user_id)`
  - Foreign keys to both `tenants` and `users`
- PostgreSQL query for `ListByUser`:
  ```sql
  SELECT id, tenant_id, user_id, status, created_at, updated_at 
  FROM tenant_memberships 
  WHERE user_id = $1 
  ORDER BY created_at, id
  ```
  **Critical observation:** Query does NOT filter by `status = 'ACTIVE'`

### 1.3 Tenant Service

**Location:** `internal/tenant/service/tenant_service.go`

**Findings:**
- Currently only implements `Create(ctx context.Context, input CreateTenantInput) (*model.Tenant, error)`
- Feature 2 responsibility: provisions tenant, ACTIVE membership, and BUSINESS_OWNER role atomically
- Uses transaction pattern for atomicity
- No retrieval/listing service methods exist yet

### 1.4 Membership Service

**Location:** `internal/tenant/service/membership_service.go`

**Findings:**
- Implements:
  - `Create(ctx context.Context, input CreateMembershipInput) (*model.TenantMembership, error)`
  - `Find(ctx context.Context, tenantID, userID string) (*model.TenantMembership, error)`
  - `ListByUser(ctx context.Context, userID string) ([]model.TenantMembership, error)` — **returns ALL memberships, no status filtering**
  - `Revoke(ctx context.Context, tenantID, userID string) error`
- `ListByUser` delegates directly to repository (no filtering)
- Could be reused for Feature 3 tenant retrieval logic

### 1.5 TenantContextService

**Location:** `internal/tenant/service/tenant_context_service.go`

**Findings:**
- Purpose: validates whether an authenticated principal may access a specific tenant
- Method: `Resolve(ctx context.Context, principal auth.Principal, candidateTenantID string) (*TenantContext, error)`
- Validation logic:
  1. Validates UserID and TenantID are valid UUIDs
  2. Retrieves tenant by ID
  3. **Critical: If tenant not found, returns TENANT_ACCESS_DENIED error (not TENANT_NOT_FOUND directly)**
  4. Rejects DISABLED tenants (returns TENANT_ACCESS_DENIED)
  5. Verifies user has membership
  6. Rejects inactive/missing membership (returns TENANT_ACCESS_DENIED)
  7. Returns trusted TenantContext if all checks pass
- **Security decision observed:** Uses 403 (TENANT_ACCESS_DENIED) for both "tenant doesn't exist" and "user has no access" cases (enumeration protection)

### 1.6 Tenant Context Middleware

**Location:** `internal/tenant/middleware.go`

**Findings:**
- Extracts tenantID from route path pattern `/api/v1/tenants/{tenantID}/...`
- Calls TenantContextService.Resolve to validate
- Stores validated TenantContext in request context
- Returns error if:
  - No authentication
  - Invalid tenantID in path
  - Resolve returns error (membership/access failure)
- **Design note:** For routes without tenantID in path (e.g., `GET /api/v1/tenants`), `routeTenantID()` returns empty string, and middleware would fail resolution
- **Implication:** List endpoint cannot use existing tenant middleware; needs custom handling

### 1.7 Authentication & Principal

**Location:** `internal/auth/middleware.go`, `internal/auth/principal.go`

**Findings:**
- Principal contains: `UserID`, `SessionID`
- Auth middleware validates bearer token and populates context with Principal
- All protected routes use `auth.Middleware` first
- No platform identity beyond UserID (no roles in Principal itself)

### 1.8 Authorization Service

**Location:** `internal/authorization/service/authorizer.go`, `internal/authorization/service/permission_resolution_service.go`

**Findings:**
- Authorizer interface provides:
  - `RequireTenantPermission(ctx context.Context, principal, tenantContext, permission string) error`
  - `RequirePlatformPermission(ctx context.Context, principal, permission string) error`
- PermissionResolutionService:
  - `ResolvePlatform(ctx context.Context, userID string) ([]string, error)` — returns platform permissions
  - `ResolveTenant(ctx context.Context, userID, tenantID string) ([]string, error)` — returns tenant permissions
- ResolveTenant validates:
  1. Tenant exists and is ACTIVE
  2. User has ACTIVE membership
  3. Returns list of permission codes (e.g., ["user.create", "tenant.read"])
- **Important:** ResolveTenant also enforces membership check (returns TENANT_ACCESS_DENIED if missing/inactive)

### 1.9 Authorization Middleware

**Location:** `internal/authorization/middleware.go`

**Findings:**
- `TenantPermissionMiddleware` requires both Principal and TenantContext (must run after tenant middleware)
- `PlatformPermissionMiddleware` requires only Principal (no tenant context needed)
- Both patterns available for use

### 1.10 Error Infrastructure

**Location:** `internal/errors/codes.go`, `internal/errors/http.go`

**Findings:**
- Error codes already defined:
  - `CodeTenantNotFound` → HTTP 404
  - `CodeTenantAccessDenied` → HTTP 403
  - `CodePermissionDenied` → HTTP 403
  - `CodeInvalidRequest` → HTTP 400
  - `CodeInvalidCredentials` → HTTP 401
- HTTP mapping via `statusByCode` map
- Public messages predefined
- No need for new error codes for basic retrieval
- **Important architectural pattern:** Use AppError type with error wrapping via `%w`; never branch on `err.Error()`

### 1.11 Existing Permissions

**Location:** `migrations/000006_seed_roles_permissions.up.sql`

**Findings:**
- Permission `tenant.read` already defined (ID: `660e8400-e29b-41d4-a716-446655440005`)
- SUPER_ADMIN (PLATFORM scope) assigned all permissions
- BUSINESS_OWNER (TENANT scope) assigned: `user.read`, `user.create`, `user.update`, `user.disable`, `tenant.read`, `tenant.update`, `role.read`, `role.assign`, `permission.read`
- STAFF (TENANT scope) assigned: `tenant.read`, `user.read`, `role.read`, `permission.read`
- **Observation:** `tenant.read` already exists; Feature 3 can reuse it rather than inventing new permission codes

### 1.12 Roles

**Location:** `migrations/000006_seed_roles_permissions.up.sql`

**Findings:**
- Three roles defined:
  1. SUPER_ADMIN (PLATFORM scope) — platform administrator
  2. BUSINESS_OWNER (TENANT scope) — tenant business owner (assigned to Feature 2 creator)
  3. STAFF (TENANT scope) — tenant staff member
- SUPER_ADMIN is platform-level; has all permissions but does not automatically bypass tenant membership validation in ResolveTenant
- **Critical:** Even SUPER_ADMIN must have explicit access path to be retrieved (no special bypass in current architecture)

### 1.13 Current Routes

**Location:** `internal/app/app.go`

**Findings:**
- `POST /api/v1/tenants` — Feature 2 tenant creation (authentication only, no tenant context)
- `POST /api/v1/tenants/{tenantID}/members` — requires auth + tenant context + `user.create` permission
- `DELETE /api/v1/tenants/{tenantID}/members/{userID}` — requires auth + tenant context + `user.disable` permission
- `POST /api/v1/tenants/{tenantID}/role-assignments` — requires auth + tenant context + `role.assign` permission
- `GET /api/v1/tenants/{tenantID}/context` — test/debug endpoint
- **No retrieval endpoints exist yet**

### 1.14 Tenant Handler

**Location:** `internal/tenant/handler/tenant_handler.go`

**Findings:**
- Current handler only implements `Create`
- PublicTenant DTO defined with fields: `ID`, `Name`, `Slug`, `Status`, `CreatedAt`, `UpdatedAt`
- DTO uses JSON snake_case conversion via struct tags
- Handler follows pattern: extract principal → validate input → call service → map result/error → write response

### 1.15 Test Infrastructure

**Location:** `internal/app/tenant_routes_test.go`, `internal/tenant/service/tenant_service_integration_test.go`

**Findings:**
- Integration tests use isolated test database schema
- Test patterns established:
  - Feature 2 route tests verify middleware chain (auth → handler)
  - Service tests verify transaction atomicity
  - Repository tests verify query correctness
- Helper functions: `buildTenantCreateRoute`, `tenantRouteAuthenticatedRequest`

### 1.16 Database Indexes

**Location:** `migrations/000004_create_tenant_memberships.up.sql`

**Findings:**
- Existing indexes:
  - `tenant_memberships_user_id_idx` on `(user_id)` — supports fast lookup by user
  - `tenant_memberships_tenant_id_idx` on `(tenant_id)` — supports fast lookup by tenant
- JOIN strategy for "list my tenants" would be:
  ```sql
  SELECT t.* FROM tenants t
  JOIN tenant_memberships m ON m.tenant_id = t.id
  WHERE m.user_id = $1 AND m.status = 'ACTIVE'
  ORDER BY t.created_at, t.id
  ```
  — index on `tenant_memberships(user_id)` makes this efficient

### 1.17 Disabled Tenant Visibility

**Location:** `internal/tenant/service/tenant_context_service.go`, `internal/authorization/service/permission_resolution_service.go`

**Findings:**
- Both TenantContextService.Resolve and PermissionResolutionService.ResolveTenant reject DISABLED tenants
- Current behavior: DISABLED tenants are not accessible even with valid ACTIVE membership
- This is enforced, not optional
- **Decision for Feature 3:** DISABLED tenants should not appear in listings, following existing precedent

---

# 2. Feature 3 Scope

Feature 3 delivers **secure authenticated tenant retrieval** supporting the following capabilities:

### 2.1 List My Tenants: **YES**

**Why included:**
- Frontend TenantProvider requires user's accessible tenants to:
  - Show onboarding screen (0 tenants)
  - Auto-select single tenant
  - Display tenant selector (2+ tenants)
- Core product requirement, not optional
- Minimal scope: return user's ACTIVE memberships + associated tenant data
- Low complexity: query existing schema efficiently

### 2.2 Get Tenant By ID: **YES**

**Why included:**
- Necessary for loading specific tenant when user navigates within app
- Enforces security: user must have active membership to retrieve
- Follows established authorization patterns from Feature 2 member/role endpoints
- Minimal scope: return single tenant if accessible

### 2.3 Platform Tenant Listing (SUPER_ADMIN global list): **NO (Deferred)**

**Why deferred:**
- Spec explicitly asks to evaluate, not automatically include
- Current architecture does not grant SUPER_ADMIN implicit access to all tenants
- No frontend requirement identified for global listing yet
- Adding SUPER_ADMIN bypass now would require:
  - New authorization logic (platform permission check vs membership check)
  - Pagination strategy for unbounded result set
  - Separate permission code (e.g., `tenant.list.platform`)
  - Additional test coverage
- Recommendation: defer to Feature 5+ when public tenant discovery or admin dashboard justifies the complexity
- **Decision:** Global listing explicitly OUT OF SCOPE for Feature 3

---

# 3. Access-Control Model

### 3.1 Authentication Requirement

**All Feature 3 endpoints require authentication.**
- Must have valid bearer token
- Principal extracted from auth middleware
- Unauthenticated requests return `401 INVALID_CREDENTIALS`

### 3.2 List My Tenants Access Control

**Endpoint:** `GET /api/v1/tenants`

- **Authentication:** REQUIRED
- **Membership requirement:** ACTIVE membership in tenant to be listed
- **Permission requirement:** NONE (membership itself grants visibility)
- **Rationale:** Basic tenant discovery is not a fine-grained permission; membership answers the access question
- **Disabled tenant behavior:** DISABLED tenants excluded from listing (consistent with TenantContextService precedent)
- **Inactive membership behavior:** Memberships with status != ACTIVE excluded from listing
  - This allows removal of access without immediate UI visibility but protects privacy
  - Disabled members do not see the tenant anymore
- **Empty list:** Returns HTTP 200 with `[]` (not 404)
  - User with no memberships gets empty array, enabling onboarding flow

### 3.3 Get Tenant By ID Access Control

**Endpoint:** `GET /api/v1/tenants/{tenantID}`

- **Authentication:** REQUIRED
- **Membership requirement:** ACTIVE membership in tenant required
- **Permission requirement:** NONE (membership itself grants visibility)
- **Tenant status requirement:** Tenant must be ACTIVE
- **Cross-tenant denial:**
  - User attempting to retrieve tenant they do not have membership in gets DENIED
  - Decision: Return `403 TENANT_ACCESS_DENIED` (not 404) per existing architecture
  - Rationale: Existing TenantContextService.Resolve returns 403 for both "not found" and "not authorized" cases
  - This choice prevents tenant enumeration attacks
- **Malformed UUID:**
  - Invalid tenantID in path returns `400 INVALID_REQUEST`
  - Validation before database lookup

### 3.4 Disabled Membership Behavior

**Observed precedent:** Both TenantContextService and PermissionResolutionService reject disabled memberships

**Feature 3 behavior:**
- DISABLED membership does not grant access to either endpoint
- GET by ID with disabled membership returns `403 TENANT_ACCESS_DENIED`
- List endpoint excludes disabled memberships

### 3.5 SUPER_ADMIN Behavior

**Decision:** SUPER_ADMIN has no special bypass for Feature 3

**Rationale:**
- Current permission system does not grant SUPER_ADMIN implicit tenant membership
- Adding bypass would require new authorization logic (platform permission check)
- Deferred to future feature when architectural need is clearer
- Current behavior: SUPER_ADMIN follows same membership-based access as ordinary users unless explicitly assigned to tenant

---

# 4. 403 vs 404 Decision

### Decision: Return `403 TENANT_ACCESS_DENIED` for unauthorized cross-tenant retrieval

### Scenario
```text
User A belongs to Tenant A
Tenant B exists
User A requests: GET /api/v1/tenants/{TenantB}
Response: 403 TENANT_ACCESS_DENIED
```

### Rationale

1. **Existing architecture precedent:**
   - TenantContextService.Resolve (line 41) converts TENANT_NOT_FOUND to TENANT_ACCESS_DENIED
   - This is deliberate: both "tenant doesn't exist" and "user has no access" return 403
   - Pattern established and tested

2. **HTTP semantics alignment:**
   - 403 Forbidden: "You are authenticated but lack permission"
   - 404 Not Found: "Resource does not exist"
   - From API consumer perspective, the distinction is not actionable

3. **Security: Enumeration protection**
   - Returning 404 for "tenant doesn't exist" would allow attacker to scan for valid tenant UUIDs
   - Returning 403 for both cases obscures whether tenant exists
   - Even though UUID namespace is large, consistent behavior is safer

4. **Precedent in similar systems:**
   - Common pattern in REST APIs: deny with 403 for authorization failures, protecting information disclosure

### Implementation
- All cases where user lacks access (missing membership, disabled membership, disabled tenant) return same error code: `TENANT_ACCESS_DENIED`
- Malformed UUID returns `INVALID_REQUEST` (400), distinguishing input validation failure from access denial

---

# 5. API Contract

### 5.1 List My Tenants

**Endpoint:** `GET /api/v1/tenants`

| Aspect | Value |
|--------|-------|
| **Method** | GET |
| **Path** | `/api/v1/tenants` |
| **Authentication** | REQUIRED (bearer token) |
| **Tenant Context** | NOT REQUIRED (no tenantID in path) |
| **Permission Check** | NONE (membership-based) |
| **Route Parameters** | None |
| **Query Parameters** | None (pagination deferred) |
| **Request Body** | None |
| **Success Response** | HTTP 200 |
| **Success Content-Type** | `application/json` |
| **Success Body** | Array of PublicTenant objects, ordered by created_at ASC, then id ASC |

**Example success response:**
```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "Acme Salon",
    "slug": "acme-salon",
    "status": "ACTIVE",
    "created_at": "2024-01-15T10:30:00Z",
    "updated_at": "2024-01-15T10:30:00Z"
  },
  {
    "id": "660e8400-e29b-41d4-a716-446655440001",
    "name": "Beauty Studio",
    "slug": "beauty-studio",
    "status": "ACTIVE",
    "created_at": "2024-02-20T14:45:00Z",
    "updated_at": "2024-02-20T14:45:00Z"
  }
]
```

**Empty list (200 OK):**
```json
[]
```

**Expected failures:**

| Scenario | Code | HTTP Status | Body |
|----------|------|-------------|------|
| Unauthenticated (no/invalid token) | INVALID_CREDENTIALS | 401 | error response |
| Tenant membership not found | (empty array) | 200 | `[]` |

### 5.2 Get Tenant By ID

**Endpoint:** `GET /api/v1/tenants/{tenantID}`

| Aspect | Value |
|--------|-------|
| **Method** | GET |
| **Path** | `/api/v1/tenants/{tenantID}` |
| **Authentication** | REQUIRED (bearer token) |
| **Tenant Context** | NOT USED (direct retrieval, not tenant-scoped action) |
| **Permission Check** | NONE (membership-based) |
| **Route Parameters** | `tenantID` (UUID, required) |
| **Query Parameters** | None |
| **Request Body** | None |
| **Success Response** | HTTP 200 |
| **Success Content-Type** | `application/json` |
| **Success Body** | PublicTenant object |

**Example success response:**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Acme Salon",
  "slug": "acme-salon",
  "status": "ACTIVE",
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T10:30:00Z"
}
```

**Expected failures:**

| Scenario | Code | HTTP Status |
|----------|------|-------------|
| Unauthenticated | INVALID_CREDENTIALS | 401 |
| Malformed tenantID (not valid UUID) | INVALID_REQUEST | 400 |
| Tenant does not exist | TENANT_ACCESS_DENIED | 403 |
| User has no membership in tenant | TENANT_ACCESS_DENIED | 403 |
| Membership is DISABLED | TENANT_ACCESS_DENIED | 403 |
| Tenant is DISABLED | TENANT_ACCESS_DENIED | 403 |

---

# 6. Service Design

### 6.1 TenantService Extensions

**Current state:** TenantService only has `Create`

**New methods to add:**

#### 6.1.1 `GetAccessible`
```
Signature: GetAccessible(ctx context.Context, userID, tenantID string) (*model.Tenant, error)

Purpose: Retrieve a single tenant if user has ACTIVE membership

Parameters:
  - userID: authenticated user's ID
  - tenantID: requested tenant ID

Returns:
  - *model.Tenant: populated tenant model if accessible
  - error: TENANT_ACCESS_DENIED if user lacks membership, TENANT_NOT_FOUND if tenant doesn't exist

Validation:
  1. Parse and validate userID, tenantID as UUIDs (return INVALID_REQUEST if invalid)
  2. Retrieve tenant from repository
     - If not found, return TENANT_NOT_FOUND
  3. If tenant.Status != ACTIVE, return TENANT_ACCESS_DENIED
  4. Check membership using MembershipService.Find
     - If membership missing or status != ACTIVE, return TENANT_ACCESS_DENIED
  5. Return tenant

Error wrapping: Preserve repository errors via %w; map database failures to INTERNAL_ERROR
```

**Why this method:**
- Encapsulates access control logic
- Reuses existing Tenant/Membership repositories
- Matches service responsibility pattern (business logic, not transport)
- Separates retrieval from list operation

#### 6.1.2 `ListAccessible`
```
Signature: ListAccessible(ctx context.Context, userID string) ([]*model.Tenant, error)

Purpose: Retrieve all tenants accessible to user via ACTIVE memberships

Parameters:
  - userID: authenticated user's ID

Returns:
  - []*model.Tenant: array of accessible tenants, ordered by created_at, id
  - error: database/validation error

Validation:
  1. Parse and validate userID as UUID (return INVALID_REQUEST if invalid)
  2. Call MembershipRepository.ListByUser to get all memberships
  3. Filter memberships where status = ACTIVE
  4. For each ACTIVE membership:
     - Retrieve tenant by ID
     - Filter: include only if tenant.Status == ACTIVE
     - Append to result array
  5. Sort result by created_at ASC, then id ASC
  6. Return result (empty array if no memberships or all excluded)

Performance:
  - N+1 query concern: may query tenant for each membership
  - Acceptable because:
    - Most users belong to few tenants (< 10)
    - Pagination not required (Feature 3 scope)
    - Alternative (single JOIN query) deferred to optimization pass
  - If N becomes problematic, optimize via batch query or left join

Error wrapping: Database errors via %w; no permission errors for this operation
```

**Why this method:**
- Encapsulates list business logic
- Applies consistent ACTIVE filtering
- Supports frontend TenantProvider workflow
- Follows service layer convention

### 6.2 Service Responsibility Summary

- **Does NOT:** make HTTP decisions, understand routes, return HTTP status codes, manipulate JSON
- **Does:** validate input UUIDs, query repositories, apply business rules (ACTIVE status, membership check), return domain errors
- **Delegates to handlers:** HTTP status code mapping, JSON serialization, content negotiation

---

# 7. Repository Design

### 7.1 TenantRepository Extensions

**Current interface:**
```go
interface TenantRepository {
    Create(ctx context.Context, tenant *model.Tenant) (*model.Tenant, error)
    FindByID(ctx context.Context, id string) (*model.Tenant, error)
}
```

**Decision:** NO NEW METHODS on TenantRepository

**Rationale:**
- `FindByID` already exists and is efficient
- Service layer (not repository) handles filtering by status
- Repository remains persistence-focused; doesn't know about business logic
- Filtering at service level is more readable and maintainable

### 7.2 MembershipRepository Extensions

**Current interface:**
```go
interface MembershipRepository {
    Create(ctx context.Context, membership model.TenantMembership) (*model.TenantMembership, error)
    FindByTenantAndUser(ctx context.Context, tenantID, userID string) (*model.TenantMembership, error)
    ListByUser(ctx context.Context, userID string) ([]model.TenantMembership, error)
    Disable(ctx context.Context, tenantID, userID string, now time.Time) error
}
```

**Decision:** NO NEW METHODS on MembershipRepository

**Rationale:**
- `ListByUser` already exists and returns all memberships
- Service layer filters by status as needed
- Adding `ListByUserActive` would be premature specialization
- Current query is simple and testable

### 7.3 Repository Summary

**No new repository methods needed.** Feature 3 uses existing:
- `TenantRepository.FindByID` — retrieve single tenant
- `MembershipRepository.ListByUser` — get all user's memberships
- `MembershipRepository.FindByTenantAndUser` — verify specific membership

---

# 8. PostgreSQL Query Strategy

### 8.1 List My Tenants: Query Approach

**High-level operation:**
```
Find all ACTIVE memberships for user
  ↓
For each membership, retrieve associated tenant
  ↓
Filter: include only ACTIVE tenants
  ↓
Sort by created_at, id
  ↓
Return tenant array
```

**SQL for membership retrieval (existing query):**
```sql
SELECT id, tenant_id, user_id, status, created_at, updated_at
FROM tenant_memberships
WHERE user_id = $1
ORDER BY created_at, id
```

**Index utilization:**
- `tenant_memberships_user_id_idx` supports WHERE clause efficiently
- ORDER BY uses indexed columns (or can be done in-memory for small result sets)

**N+1 consideration:**
```
findMemberships(userID)           — 1 query
for each membership:
  findTenantByID(membership.tenantID)  — N queries
```

**Acceptable because:**
- Most users have few tenants (< 10)
- Indexes make each tenant lookup efficient
- Total query time negligible for normal user
- Optimization deferred: could use `LEFT JOIN tenants` if needed later

**Example query plan (deferred optimization, not required for Feature 3):**
```sql
SELECT DISTINCT t.id, t.name, t.slug, t.status, t.created_at, t.updated_at
FROM tenants t
JOIN tenant_memberships m ON t.id = m.tenant_id
WHERE m.user_id = $1 
  AND m.status = 'ACTIVE'
  AND t.status = 'ACTIVE'
ORDER BY t.created_at, t.id
```

### 8.2 Get Tenant By ID: Query Approach

**Simple retrieval:**
```sql
SELECT id, name, slug, status, created_at, updated_at
FROM tenants
WHERE id = $1
```

**Index utilization:**
- Primary key `tenants(id)` supports this efficiently

**Membership validation (existing query):**
```sql
SELECT id, tenant_id, user_id, status, created_at, updated_at
FROM tenant_memberships
WHERE tenant_id = $1 AND user_id = $2
```

**Index utilization:**
- `tenant_memberships_tenant_id_idx` and `tenant_memberships_user_id_idx`
- Alternatively, unique constraint on `(tenant_id, user_id)` ensures at most one row

### 8.3 Ordering

**Deterministic ordering:** Required for consistency and pagination-readiness

**Chosen order:** `created_at ASC, id ASC`
- `created_at` is primary sort (chronological)
- `id` is tiebreaker (stable, prevents undefined ordering if timestamps collide)
- Both columns indexed in existing schema
- Matches existing `ListByUser` query behavior

### 8.4 Indexes Assessment

**Existing indexes support Feature 3:**
- `tenant_memberships_user_id_idx` → fast lookup by user
- `tenant_memberships_tenant_id_idx` → fast lookup by tenant (for membership validation)
- Primary keys on both tables → efficient direct retrieval

**New indexes needed:** NONE

**Reasoning:**
- Current indexes support all queries
- No complex joins or range scans introduced
- Query volume not known to be high enough to justify new indexes

---

# 9. Pagination Decision

### Decision: NO PAGINATION for Feature 3

### Rationale

1. **Scope of List My Tenants:**
   - Typical user belongs to 1–3 tenants (business model assumption)
   - Even power users rarely belong to 20+ tenants
   - Response size negligible: ~200 bytes per tenant × 10 tenants = ~2 KB

2. **Frontend requirement:**
   - TenantProvider just needs to populate selector/onboarding
   - No "show more" interaction expected
   - Infinite scroll not part of product design

3. **API complexity trade-off:**
   - Pagination adds:
     - Query parameters (`limit`, `offset` or `cursor`)
     - Response wrapper (e.g., `{data: [...], total: N, hasMore: bool}`)
     - Cursor encoding/decoding if cursor-based
     - Tests for cursor correctness
   - Benefit: not justified for small result sets

4. **Future flexibility:**
   - If (when) pagination is needed, adding it is backwards-compatible:
     - Existing clients get all results (still works)
     - New clients can use pagination parameters (old behavior if omitted)

### Implementation
- Return all accessible tenants in single response
- Empty array `[]` if no tenants (not paginated)
- Do not include pagination fields in response

---

# 10. Error Contract

### Error Definitions for Feature 3

| Code | Scenario | Originating Layer | HTTP Status | Classification | Wrapping |
|------|----------|-------------------|-------------|-----------------|----------|
| `INVALID_REQUEST` | Malformed tenant UUID in route parameter | Handler/Service | 400 | Client error | Via `%w` from uuid.Parse |
| `INVALID_CREDENTIALS` | Missing or invalid bearer token | Auth middleware | 401 | Client error | Direct from auth middleware |
| `TENANT_NOT_FOUND` | Tenant record does not exist | Repository | 404 | Client error | Via `%w` from sql.ErrNoRows |
| `TENANT_ACCESS_DENIED` | User has no ACTIVE membership / membership is DISABLED / tenant is DISABLED | Service/Authorization | 403 | Client error | Explicit AppError construction |
| `INTERNAL_ERROR` | Database query failure (unexpected error) | Repository | 500 | System error | Via `%w` from database error |

### 10.1 List My Tenants Errors

**Unauthenticated request:**
- Code: `INVALID_CREDENTIALS`
- Status: 401
- Source: auth middleware (before handler)
- No service method invoked

**User has no memberships:**
- Returns HTTP 200 with `[]`
- Not an error; expected state

**Database failure during membership query:**
- Code: `INTERNAL_ERROR`
- Status: 500
- Wrapped from repository error

### 10.2 Get Tenant By ID Errors

**Unauthenticated:**
- Code: `INVALID_CREDENTIALS`
- Status: 401
- Source: auth middleware

**Malformed tenantID:**
- Code: `INVALID_REQUEST`
- Status: 400
- Source: Service.GetAccessible calls uuid.Parse(tenantID)

**Tenant does not exist:**
- Code: `TENANT_NOT_FOUND`
- Status: 404
- Source: Repository.FindByID returns sql.ErrNoRows wrapped as AppError

**Membership missing / Disabled membership / Disabled tenant:**
- Code: `TENANT_ACCESS_DENIED`
- Status: 403
- Source: Service.GetAccessible membership/status checks

**Database failure (unexpected):**
- Code: `INTERNAL_ERROR`
- Status: 500
- Wrapped from repository or service error

### 10.3 Error Mapping Implementation

No new error codes required. Reuse existing:
- `CodeInvalidRequest` (400)
- `CodeInvalidCredentials` (401)
- `CodeTenantNotFound` (404)
- `CodeTenantAccessDenied` (403)
- `CodeInternalError` (500)

All mappings already exist in `internal/errors/http.go`.

### 10.4 Error Response Format

Uses existing format (per `HTTPError.WriteJSON`):
```json
{
  "error": {
    "code": "TENANT_ACCESS_DENIED",
    "message": "You do not have access to this tenant."
  }
}
```

---

# 11. Security Requirements

### 11.1 Core Invariants

1. **No implicit access via UUID knowledge**
   - Knowing tenant UUID does NOT grant access
   - Membership check is mandatory for both endpoints
   - Verified by: service layer membership validation + integration tests

2. **Membership must be ACTIVE**
   - DISABLED or missing membership denies access
   - Even if tenant exists and is ACTIVE
   - Verified by: MembershipService.Find status check + integration tests

3. **Tenant must be ACTIVE**
   - DISABLED tenants not listed
   - DISABLED tenants return access denied if retrieved directly
   - Follows existing TenantContextService precedent
   - Verified by: service tenant status check + integration tests

4. **Enumeration protection (403 vs 404)**
   - "Tenant doesn't exist" and "user has no access" both return 403
   - Prevents scanning for valid tenant UUIDs
   - Verified by: error handling tests + manual security review

5. **User identity comes from Principal, not request**
   - Userıd obtained from authenticated Principal
   - Request body/parameters CANNOT specify another user's ID
   - List endpoint always returns authenticated user's tenants, not arbitrary user's
   - Verified by: auth tests + service signature (userID parameter required)

6. **Repository filtering cannot leak tenant data**
   - MembershipRepository.ListByUser WHERE clause includes user_id
   - SQL constraint prevents cross-user leakage
   - Service layer enforces membership status filter
   - Verified by: SQL review + integration tests with multiple users

7. **Inactive/revoked membership does not grant access**
   - User whose membership is disabled loses visibility
   - Cannot see tenant in list or retrieve by ID
   - Enables safe off-boarding
   - Verified by: integration tests for disabled membership scenarios

### 11.2 Test-Verified Scenarios

- [Test] User A cannot retrieve Tenant B (different membership)
- [Test] User A's tenant list never includes Tenant B if no membership
- [Test] Request parameters cannot substitute another user's identity
- [Test] Inactive membership does not grant list visibility
- [Test] Inactive membership denies direct retrieval
- [Test] Repository query filters by correct user_id
- [Test] SQL constraints prevent table design bypass

---

# 12. TDD Test Matrix

### Test Format
| Test Name | Layer | Scenario | Expected Result |

---

## 12.1 Repository / Integration Tests

| Test Name | Layer | Scenario | Expected Result |
|-----------|-------|----------|-----------------|
| `TestListByUserReturnsActiveAndDisabledMemberships` | Repository (MembershipRepository) | User has ACTIVE and DISABLED memberships | Returns all memberships regardless of status (current behavior) |
| `TestListByUserReturnsDeterministicOrdering` | Repository | Multiple memberships created at different times | Returns ordered by created_at ASC, id ASC |
| `TestListByUserReturnsEmptyForUserWithNoMemberships` | Repository | User exists, has no memberships | Returns empty slice, no error |
| `TestListByUserFiltersToSingleUser` | Repository | Multiple users with memberships in same tenant | Returns only queried user's memberships, not other users' |
| `TestFindByIDReturnsActiveTenant` | Repository (TenantRepository) | Tenant ID exists, tenant is ACTIVE | Returns tenant, no error |
| `TestFindByIDReturnsErrorForNonexistent` | Repository | Tenant ID does not exist | Returns TENANT_NOT_FOUND error |
| `TestFindByIDReturnsErrorForDisabledTenant` | Repository | Tenant ID exists, tenant is DISABLED | Returns tenant (repository doesn't filter status) |
| `TestMembershipFindByTenantAndUserReturnsActiveMembership` | Repository | User has ACTIVE membership | Returns membership, no error |
| `TestMembershipFindByTenantAndUserReturnsDisabledMembership` | Repository | User has DISABLED membership | Returns membership (repository doesn't filter) |
| `TestMembershipFindByTenantAndUserReturnsNilForMissing` | Repository | No membership exists for user/tenant pair | Returns nil, no error |

---

## 12.2 Service Layer Tests

| Test Name | Layer | Scenario | Expected Result |
|-----------|-------|----------|-----------------|
| `TestListAccessibleIncludesOnlyActiveMemberships` | Service (TenantService) | User has ACTIVE and DISABLED memberships | Returns only tenants associated with ACTIVE memberships |
| `TestListAccessibleExcludesDisabledTenants` | Service | ACTIVE membership but tenant is DISABLED | Tenant excluded from result list |
| `TestListAccessibleOrderedByCreatedAtThenID` | Service | Multiple accessible tenants | Results sorted by created_at ASC, id ASC |
| `TestListAccessibleReturnsEmptyForNoMemberships` | Service | User exists, no memberships | Returns empty slice, no error |
| `TestListAccessiblePropagatesDatabaseError` | Service | Membership query fails | Returns error wrapped with context |
| `TestListAccessibleInvalidUserID` | Service | UserID is not valid UUID | Returns INVALID_REQUEST error |
| `TestGetAccessibleSucceedsForAuthorizedUser` | Service | User has ACTIVE membership in ACTIVE tenant | Returns tenant, no error |
| `TestGetAccessibleDeniesDisabledMembership` | Service | User has DISABLED membership | Returns TENANT_ACCESS_DENIED |
| `TestGetAccessibleDeniesDisabledTenant` | Service | Tenant is DISABLED despite ACTIVE membership | Returns TENANT_ACCESS_DENIED |
| `TestGetAccessibleDeniesMissingMembership` | Service | User has no membership (different tenant) | Returns TENANT_ACCESS_DENIED |
| `TestGetAccessibleInvalidUserID` | Service | UserID not valid UUID | Returns INVALID_REQUEST |
| `TestGetAccessibleInvalidTenantID` | Service | TenantID not valid UUID | Returns INVALID_REQUEST |
| `TestGetAccessibleTenantNotFound` | Service | Tenant record does not exist | Returns TENANT_NOT_FOUND error |

---

## 12.3 Handler / Route Tests

| Test Name | Layer | Scenario | Expected Result |
|-----------|-------|----------|-----------------|
| `TestListTenantsRequiresAuthentication` | Handler | GET /api/v1/tenants without token | 401 INVALID_CREDENTIALS |
| `TestListTenantsSucceedsForAnyAuthenticatedUser` | Handler | GET /api/v1/tenants with valid token | 200 OK, JSON array of PublicTenant |
| `TestListTenantsReturnsEmptyForNoMemberships` | Handler | User has no memberships | 200 OK, empty JSON array `[]` |
| `TestListTenantsPropagatesServiceError` | Handler | Service returns database error | 500 INTERNAL_ERROR |
| `TestGetTenantRequiresAuthentication` | Handler | GET /api/v1/tenants/{id} without token | 401 INVALID_CREDENTIALS |
| `TestGetTenantMalformedIDReturns400` | Handler | GET with invalid UUID in path | 400 INVALID_REQUEST |
| `TestGetTenantSucceedsForAuthorizedUser` | Handler | User has ACTIVE membership | 200 OK, JSON PublicTenant |
| `TestGetTenantDeniesUnauthorizedUser` | Handler | User has no membership | 403 TENANT_ACCESS_DENIED |
| `TestGetTenantNotFoundReturns403` | Handler | Tenant doesn't exist | 403 TENANT_ACCESS_DENIED (not 404) |
| `TestGetTenantErrorPropagation` | Handler | Service returns database error | 500 INTERNAL_ERROR |
| `TestListTenantsReturnsPublicTenantDTO` | Handler | Verify response structure | Response includes id, name, slug, status, created_at, updated_at |
| `TestGetTenantReturnsPublicTenantDTO` | Handler | Verify response structure | Single PublicTenant object with all fields |

---

## 12.4 Route & Middleware Chain Tests

| Test Name | Layer | Scenario | Expected Result |
|-----------|-------|----------|-----------------|
| `TestListTenantsRouteChain` | Route/App | Verify auth middleware applied | Unauthenticated request returns 401 |
| `TestGetTenantRouteChain` | Route/App | Verify auth middleware applied | Unauthenticated request returns 401 |
| `TestGetTenantRouteExtractsPathParameter` | Route/App | Verify tenantID parsed correctly | Handler receives correct ID from path |

---

## 12.5 Security Tests

| Test Name | Layer | Scenario | Expected Result |
|-----------|-------|----------|-----------------|
| `TestUserACannotRetrieveTenantB` | Integration (Security) | User A belongs to Tenant A, Tenant B exists but User A has no membership | GET /api/v1/tenants/{B} returns 403 TENANT_ACCESS_DENIED |
| `TestListTenantsNeverIncludeUnauthorizedTenant` | Integration (Security) | User A has membership in A, Tenant B exists | GET /api/v1/tenants never includes Tenant B |
| `TestListExcludesDisabledMembership` | Integration (Security) | User's membership is DISABLED | List does not include that tenant |
| `TestGetExcludesDisabledMembership` | Integration (Security) | User's membership is DISABLED | Direct retrieval returns 403 |
| `TestRepositoryFilteringPreventsCrossTenantLeakage` | Integration (Security) | Membership query scoped to user_id | SQL WHERE clause prevents other users' memberships in result |
| `TestDisabledTenantNotListed` | Integration (Security) | Tenant is DISABLED, user has ACTIVE membership | List excludes tenant |
| `TestDisabledTenantNotRetrievable` | Integration (Security) | Tenant is DISABLED, user has ACTIVE membership | Direct GET returns 403 |

---

## 12.6 Regression Tests

| Test Name | Layer | Scenario | Expected Result |
|-----------|-------|----------|-----------------|
| `TestFeature2CreationUnchanged` | Feature compatibility | Feature 2 tenant creation still works | POST /api/v1/tenants succeeds atomically |
| `TestFeature2TenantSlugUniquenessUnchanged` | Feature compatibility | Slug uniqueness still enforced | Duplicate slug returns 409 TENANT_SLUG_TAKEN |
| `TestFeature2BusinessOwnerAssignmentUnchanged` | Feature compatibility | Creator still assigned BUSINESS_OWNER role | Tenant creator has BUSINESS_OWNER role |
| `TestEpic01AuthenticationUnchanged` | Feature compatibility | Authentication still required | Unauthenticated requests return 401 |
| `TestEpic01AuthorizationUnchanged` | Feature compatibility | Existing permission checks still work | Role assignment still enforces permissions |
| `TestEpic01MembershipUnchanged` | Feature compatibility | Membership model unchanged | Existing membership queries still work |

---

# 13. Files Expected To Change

### 13.1 NEW FILES

```
internal/tenant/handler/retrieval_handler.go        (List + Get endpoints)
internal/tenant/handler/retrieval_handler_test.go   (Handler tests)
internal/tenant/service/retrieval_service.go        (Service methods for retrieval)
internal/tenant/service/retrieval_service_test.go   (Service tests)
```

**Alternative organization:** Could extend existing tenant_handler.go/tenant_service.go instead of separate files. Recommend separate files for clarity (creation vs retrieval are distinct concerns).

### 13.2 MODIFY

| File | Changes |
|------|---------|
| `internal/app/app.go` | Add two route registrations (List + Get endpoints with auth middleware) |
| `internal/tenant/service/tenant_service.go` | Extend TenantService interface to include GetAccessible, ListAccessible (or create new service) |

### 13.3 UNCHANGED (Protected)

```
internal/tenant/model/tenant.go
internal/tenant/model/membership.go
internal/tenant/repository/tenant_repository.go
internal/tenant/repository/membership_repository.go
internal/tenant/repository/postgres_tenant_repository.go
internal/tenant/repository/postgres_membership_repository.go
internal/tenant/middleware.go
internal/tenant/context.go
internal/tenant/service/membership_service.go
internal/tenant/service/tenant_context_service.go
internal/auth/middleware.go
internal/auth/principal.go
internal/authorization/service/authorizer.go
internal/authorization/middleware.go
internal/errors/codes.go
internal/errors/http.go
migrations/ (all existing migrations)
```

---

# 14. Migration Assessment

### Decision: NO NEW MIGRATION REQUIRED

### Justification

1. **Existing schema fully supports Feature 3:**
   - `tenants` table has all required fields (id, name, slug, status, created_at, updated_at)
   - `tenant_memberships` table has required fields (tenant_id, user_id, status)
   - Indexes exist: `tenant_memberships_user_id_idx`, `tenant_memberships_tenant_id_idx`

2. **Queries do not require new schema:**
   - List query: `SELECT * FROM tenant_memberships WHERE user_id = $1`
   - Retrieval: `SELECT * FROM tenants WHERE id = $1` + membership lookup
   - Both use existing columns and indexes

3. **No new constraints needed:**
   - Uniqueness constraints already prevent dual memberships
   - Status checks are application logic, not schema constraints

4. **Immutability of existing migrations:**
   - Migrations 000001–000007 remain unchanged
   - No backfill logic needed
   - No schema evolution required

### Alternative: If optimization is later needed

Should a performance issue arise (e.g., thousands of memberships per user), a future migration might add:
```sql
CREATE INDEX tenant_memberships_user_id_status_idx 
ON tenant_memberships(user_id, status, created_at)
```

But this is premature and deferred.

---

# 15. Implementation Order

### Step-by-Step Sequence (TDD-First)

1. **Service Layer: List Accessible**
   - Write failing tests (service tests for ListAccessible)
   - Implement TenantService.ListAccessible method
   - Verify: tests pass, handles ACTIVE/DISABLED filtering, returns sorted order

2. **Service Layer: Get Accessible**
   - Write failing tests (service tests for GetAccessible)
   - Implement TenantService.GetAccessible method
   - Verify: tests pass, membership check enforced, 403 behavior correct

3. **Handler: List Endpoint**
   - Write failing handler tests (mock service)
   - Implement handler method for GET /api/v1/tenants
   - Verify: tests pass, returns PublicTenant array, handles errors

4. **Handler: Get Endpoint**
   - Write failing handler tests (mock service)
   - Implement handler method for GET /api/v1/tenants/{tenantID}
   - Verify: tests pass, returns single PublicTenant, handles errors

5. **Route Registration**
   - Write failing route tests (verify middleware chain)
   - Register routes in app.go: GET /api/v1/tenants, GET /api/v1/tenants/{tenantID}
   - Apply auth middleware (no tenant context middleware for list; optional for get)

6. **Integration Tests**
   - Write security tests (cross-tenant retrieval denied, disabled memberships, etc.)
   - Create real database scenario with multiple users/tenants
   - Verify: no cross-user leakage, correct access control

7. **Regression Tests**
   - Run all Epic 01 tests (auth, authorization, membership)
   - Run all Feature 1 tests (tenant persistence)
   - Run all Feature 2 tests (tenant creation)
   - Verify: no breakage

8. **Code Quality**
   - Run `go vet ./...`
   - Run `go fmt -l .` (format check)
   - Review diff: ensure no scope creep, no commented code, no debug statements

---

# 16. Risks / Architectural Concerns

### 16.1 Cross-Tenant Data Leakage

**Risk:** Repository query returns membership/tenant for wrong user

**Mitigation:**
- MembershipRepository.ListByUser has `WHERE user_id = $1` — SQL constraint prevents leakage
- Service layer does not bypass repository filtering
- Integration test verifies: multiple users, verify only queried user's memberships returned
- Code review: inspect all queries for WHERE clause correctness

### 16.2 N+1 Tenant Queries

**Risk:** List endpoint issues one membership query + N tenant queries (one per membership)

**Mitigation:**
- Acceptable for Feature 3 scope: typical user has < 10 memberships
- Each tenant query uses indexed lookup (fast)
- Documented in comment: "N+1 acceptable for current scale"
- Optimization path identified: future join-based query if needed

### 16.3 Duplicated Membership Checks

**Risk:** Membership checked in both service and potential middleware

**Mitigation:**
- List endpoint: does NOT use tenant middleware (no tenantID in path), membership checked in service
- Get endpoint: does NOT use tenant middleware, membership checked in service directly
- Do not apply tenant middleware to retrieval endpoints (would be redundant and circular)
- Clear design decision documented

### 16.4 Duplicated Authorization Logic

**Risk:** Permission logic repeated across service/handler/middleware

**Mitigation:**
- Feature 3 does NOT use permission codes for basic retrieval
- Membership itself grants access (no `tenant.read` permission check needed)
- Simple rule: ACTIVE membership = ACTIVE tenant = access granted
- Authorization layer (permission codes) reserved for specific actions (e.g., user.create, role.assign)

### 16.5 SUPER_ADMIN Implicit Access Risk

**Risk:** SUPER_ADMIN user expects to see all tenants but gets 403

**Mitigation:**
- Explicitly deferred from Feature 3
- Current architecture does not grant SUPER_ADMIN implicit membership to all tenants
- If needed, future feature (Feature 5+) adds:
  - Platform permission check (e.g., `tenant.list.platform`)
  - Alternative path: check for PLATFORM role
  - Separate endpoint or merged endpoint with permission fork
- Clear decision documented: Feature 3 does not implement SUPER_ADMIN bypass

### 16.6 Tenant Enumeration Attack

**Risk:** Attacker learns valid tenant UUIDs by scanning 404 responses

**Mitigation:**
- Return 403 (not 404) for "tenant doesn't exist" scenario
- Same behavior for "user has no access"
- Attacker cannot distinguish, cannot enumerate
- Existing precedent: TenantContextService.Resolve already uses this pattern
- Tests verify: both scenarios return 403 (403 vs 404 tests)

### 16.7 Inactive Membership Visibility

**Risk:** User with disabled membership can still see tenant, creating confusion

**Mitigation:**
- DISABLED membership completely hides tenant from both endpoints
- Clean cut: disabled = no visibility
- Supports safe off-boarding (remove membership access immediately)
- Test: verify disabled membership excludes tenant from list

### 16.8 Disabled Tenant Visibility

**Risk:** User with ACTIVE membership to DISABLED tenant expects to see it (to understand why they can't book)

**Mitigation:**
- Feature 3 excludes DISABLED tenants from list (business decision)
- Follows existing TenantContextService precedent
- If product later needs visibility, Feature 8 handles lifecycle transitions and policies
- Documented as deliberate choice, not oversight
- Test: verify DISABLED tenant excluded

### 16.9 Pagination Overengineering

**Risk:** Pagination added now becomes tech debt when rarely used

**Mitigation:**
- Explicitly not included in Feature 3
- Documented rationale: typical user has few tenants
- Backwards-compatible to add later (existing clients get all results, new clients can paginate)
- No tech debt; clean removal of scope

### 16.10 Feature 4/5/7 Scope Creep

**Risk:** Feature 3 accidentally implements tenant settings, public slug lookup, staff assignment

**Mitigation:**
- Strict scope: retrieval only (no mutations)
- No new DTOs beyond PublicTenant (existing DTO reused)
- No new permissions beyond existing `tenant.read`
- Code review checklist: verify no create/update/delete, no new fields

---

# 17. Acceptance Criteria

### 17.1 Functional Criteria

**List My Tenants:**
1. ✓ GET /api/v1/tenants returns HTTP 200 with JSON array of PublicTenant objects
2. ✓ Array includes only tenants where user has ACTIVE membership
3. ✓ Array excludes tenants where user has DISABLED membership
4. ✓ Array excludes DISABLED tenants (even with ACTIVE membership)
5. ✓ Array is sorted by `created_at ASC, id ASC`
6. ✓ Empty array (not 404) when user has no memberships
7. ✓ Unauthenticated request returns 401 INVALID_CREDENTIALS

**Get Tenant By ID:**
1. ✓ GET /api/v1/tenants/{id} returns HTTP 200 with single PublicTenant object
2. ✓ Only accessible if user has ACTIVE membership
3. ✓ Returns 403 TENANT_ACCESS_DENIED if user has DISABLED membership
4. ✓ Returns 403 TENANT_ACCESS_DENIED if user has no membership (not 404)
5. ✓ Returns 403 TENANT_ACCESS_DENIED if tenant is DISABLED (even with ACTIVE membership)
6. ✓ Returns 400 INVALID_REQUEST if tenantID is not valid UUID
7. ✓ Unauthenticated request returns 401 INVALID_CREDENTIALS

### 17.2 Security Criteria

1. ✓ User A cannot retrieve Tenant B (no membership)
2. ✓ User A's list never includes Tenant B
3. ✓ Request body/parameters cannot substitute another user's identity
4. ✓ Disabled membership completely denies access (both endpoints)
5. ✓ Database query filters by authenticated user_id (prevents cross-user leakage)
6. ✓ Enumeration protected: both "not found" and "no access" return same status (403)

### 17.3 API Contract Criteria

1. ✓ List response is JSON array (not wrapped object)
2. ✓ Get response is single JSON object (not array)
3. ✓ PublicTenant DTO includes: id, name, slug, status, created_at, updated_at
4. ✓ Error response format: `{ "error": { "code": "...", "message": "..." } }`
5. ✓ All timestamp fields are RFC3339 format (UTC)

### 17.4 Regression Criteria

1. ✓ Feature 2 (POST /api/v1/tenants) still works
2. ✓ Feature 2 slug uniqueness still enforced
3. ✓ Feature 2 atomic provisioning still works
4. ✓ Epic 01 authentication still required
5. ✓ Epic 01 authorization patterns still work
6. ✓ Epic 01 membership model unchanged
7. ✓ All existing tests pass (go test ./...)

### 17.5 Code Quality Criteria

1. ✓ go vet ./... passes
2. ✓ gofmt -l . shows no files needing format
3. ✓ No commented code
4. ✓ No debug statements (log.Print, fmt.Println)
5. ✓ No scope creep (only retrieval, no mutations)
6. ✓ Service layer does not know about HTTP
7. ✓ Handler does not repeat service validation

---

# 18. Explicit Non-Changes

### 18.1 Epic 01 — Protected

The following **must NOT be modified** during Feature 3 implementation:

```
Tenant membership model and repository
Authentication middleware
Authorization service and middleware
Role assignment logic
Permission catalog
SUPER_ADMIN semantics
Session management
User authentication
```

### 18.2 Feature 1 — Protected

The following **must NOT be modified**:

```
Tenant model (ID, Name, Slug, Status, CreatedAt, UpdatedAt)
Tenant repository interface (Create, FindByID)
Tenant status enum (ACTIVE, DISABLED)
Schema: tenants table
```

### 18.3 Feature 2 — Protected

The following **must NOT be modified**:

```
POST /api/v1/tenants endpoint
Tenant creation transaction (atomicity)
BUSINESS_OWNER role assignment
Membership creation during provisioning
Slug uniqueness enforcement
Feature 2 error codes (TENANT_SLUG_TAKEN, etc.)
Creator identity from Principal (not request body)
```

### 18.4 Migrations — Protected

The following **must NOT be modified**:

```
migrations/000001_create_users.up.sql
migrations/000002_create_sessions.up.sql
migrations/000003_create_tenants.up.sql
migrations/000004_create_tenant_memberships.up.sql
migrations/000005_create_roles_permissions.up.sql
migrations/000006_seed_roles_permissions.up.sql
migrations/000007_add_slug_to_tenants.up.sql
(no rollback of existing migrations)
```

### 18.5 Future Features — Out of Scope

The following **must NOT be started** during Feature 3:

```
Feature 4: Tenant Context Isolation
Feature 5: Tenant Branding & Slug Resolution
Feature 6: Role Assignment (already exists, don't redesign)
Feature 7: Tenant-Wide Resource Isolation
Feature 8: Tenant Lifecycle Management
Tenant settings
Tenant updates/mutations
Tenant deletion
Staff scheduling
Booking management
Payment processing
Subscriptions/Billing
```

---

# 19. Summary & Key Decisions

| Decision | Rationale | Impact |
|----------|-----------|--------|
| List My Tenants: YES | Frontend TenantProvider needs user's tenants | Core Feature 3 |
| Get Tenant By ID: YES | Necessary for tenant details retrieval | Core Feature 3 |
| SUPER_ADMIN global listing: NO | Deferred; adds architectural complexity not yet justified | Feature 3 remains simple |
| 403 vs 404: 403 TENANT_ACCESS_DENIED | Enumeration protection; consistent with existing precedent | Security-first, harder to enumerate |
| Membership filtering: ACTIVE only | Disabled members lose visibility; enforces access control | Clean off-boarding |
| Pagination: NO | Most users have few tenants; backwards-compatible to add later | Simpler API, no tech debt |
| Permission requirement: NONE | Membership itself grants access; reuse existing `tenant.read` if needed | Simpler authorization |
| New repository methods: NONE | Existing methods sufficient; filtering in service layer | Narrow, focused repository |
| New migration: NO | Existing schema and indexes sufficient | No database changes |
| Disabled tenant behavior: EXCLUDE | Follows TenantContextService precedent | Consistent, not surprising |

---

# END OF PLAN

**This plan is complete and ready for implementation review.**

- Next step: Implementation agent inspects this plan, asks clarifying questions if any, then proceeds TDD-first
- Do NOT implement until plan is reviewed and approved
- Do NOT modify this plan without explicit request; it is the source of truth

---

## QUICK REFERENCE: 403 vs 404 DECISION

When `GET /api/v1/tenants/{tenantID}` fails:
- **Both** "tenant doesn't exist" AND "user has no access" return **403 TENANT_ACCESS_DENIED**
- Protects against enumeration attacks
- Consistent with existing TenantContextService.Resolve behavior
- Tested explicitly in test matrix
