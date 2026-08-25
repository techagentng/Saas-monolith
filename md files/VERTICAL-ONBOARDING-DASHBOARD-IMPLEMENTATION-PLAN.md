# Vertical Onboarding & Dashboard Architecture — Implementation Plan

**Status: PLAN ONLY — repository-grounded. No production code, migrations, or tests were written for this pass.**

Source baseline: `Epic_02_Vertical_Onboarding_and_Dashboard_Architecture.docx` (conceptual — see comparison table in §3 for where it was overridden by repository evidence).

**Revision note:** this document was reviewed and amended after a first read-through. Corrections applied: `business_type`/`onboarding_status` are typed Go enums, not bare strings (§6); the DB column nullability strategy for legacy vs. new tenants is now explicit (§8, §22); `POST /tenants`'s contract evolution is called out as intentional rather than silent (§21); `business_type` immutability is stated as non-negotiable, not just a soft default (§7); onboarding completion now requires an explicit prerequisite-validation step, closing a public-launch bypass where a caller could call the completion endpoint immediately after creation with nothing configured (§21, §23, §24, F2); the feature table is renamed/reordered and F8 is relabeled frontend-primary (§26); dependency/implementation order is simplified to a sequential recommendation (§27). No repository re-inspection was performed for this revision — corrections are reasoning-level, applied on top of the same evidence in §2.

Repositories inspected:
- Backend: `Saas-monolith` (Go modular monolith, `main` branch, current HEAD `d4b1af6`)
- Frontend: `Saas-frontend` (Next.js App Router, `main` branch)

---

## 1. Executive Summary

The product direction in the baseline document is sound, but the document underestimates how much of it is **already built**. The backend's Feature 2 (tenant creation) and Feature 5 (slug/public identity) already force "create the tenant early, with a slug, atomically" — there is no draft-tenant concept anywhere, and building one would duplicate work Feature 2 already does. The frontend's onboarding route, `TenantGate`, `TenantProvider`, and permissions stack already implement the skeleton of the exact resume-capable, backend-authoritative flow the document asks for — today it just has one step (business name + slug) instead of many, and no notion of "is onboarding done."

The actual gap is narrow: **two new tenant-scoped columns** (`business_type`, plus a resume pointer distinct from the existing `status` lifecycle field), **one new visibility rule** (public identity must also require onboarding completion, not just `ACTIVE` status), and **a step-framework layer on the frontend** that the existing config-driven nav pattern (`dashboardNavItems`) already models well enough to extend rather than reinvent.

This plan recommends: create the tenant near the start of onboarding (extending the existing Feature 2 transaction, not replacing it); store `business_type` directly on `tenants` as a fourth identity-like column alongside `slug`/`status`; add a workflow-only `onboarding_status`/`onboarding_step` pair that is explicitly decoupled from the existing `ACTIVE`/`DISABLED` lifecycle so its meaning is never disturbed; gate public visibility on both; and build the frontend step framework as a natural extension of the existing `TenantGate`/`TenantProvider`/nav-config architecture. No vertical booking domain (rooms, tables, routes, technicians) is designed in this pass, per the document's own instruction.

---

## 2. Repository State

### Backend (`Saas-monolith`) — verified by reading source, not by re-running the doc's assumptions

| Area | Actual state |
|---|---|
| `tenants` table | `id, name, slug, status, description, contact_email, contact_phone, timezone, created_at, updated_at`. `status CHECK IN ('ACTIVE','DISABLED')` — **binary**, not the doc's assumed `ACTIVE/SUSPENDED/INACTIVE`. No `business_type`, no onboarding columns, no settings table. (migrations 000003, 000007, 000008) |
| Tenant creation (Feature 2) | `tenantService.Create` — one `BeginTx`/`Commit` transaction: insert tenant (name+**slug required**), create `ACTIVE` membership, look up `BUSINESS_OWNER` role, insert `user_roles` row. Creator ID comes only from `auth.Principal`, never the request body. Already atomic; already requires slug up front. |
| Tenant retrieval (Feature 3) | `ListAccessibleByUserID` — `INNER JOIN tenant_memberships ... WHERE tm.status='ACTIVE' AND t.status='ACTIVE'`. No `LIMIT`; already multi-tenant-safe. `GetAccessible` re-derives access from the same list (no separate trust path). |
| Tenant profile (Feature 4) | `UpdateProfile` — partial update of name/description/contact_email/contact_phone/timezone via nullable-pointer diffing. Slug is never editable here. |
| Slug/public identity (Feature 5) | `model.ValidateSlug` — canonical-only (no normalization), reserved-word list (`admin, api, auth, book, dashboard, health, login, logout, public, settings, signup, static, support, tenants, users, www`), DB-enforced uniqueness (`tenants_slug_key`). `PublicTenantService.GetBySlug` hides reserved slugs and non-`ACTIVE` tenants identically as `TENANT_NOT_FOUND`. `PublicTenantIdentity` exposes only `slug, name, description, timezone` — no ID, no status, no contact info. |
| Memberships | `tenant_memberships(tenant_id, user_id, status)`, unique on `(tenant_id, user_id)`. |
| Roles/permissions | Seeded: `SUPER_ADMIN` (PLATFORM, all 13 permissions), `BUSINESS_OWNER` (TENANT, 9 permissions), `STAFF` (TENANT, 4 permissions). `user_roles(user_id, role_id, tenant_id NULL=platform)`. Two partial unique indexes prevent duplicate platform vs. tenant assignments. |
| Authorization | `Authorizer.RequireTenantPermission` / `RequirePlatformPermission` — both **fail closed** on any resolver error or missing principal/tenant context; never cache. |
| Tenant context | `tenant.Middleware` (HTTP) → `TenantContextService.Resolve` — re-derives tenant+membership from DB on every request; a client-supplied `tenantID` is only ever a lookup key, never trusted. |
| Auth/registration | `POST /api/v1/users` public, no tokens returned, no role assignment code path at all. `POST /api/v1/auth/login` returns `{user, access_token, refresh_token, expires_in}`. No session/cookie mechanism anywhere. |
| SUPER_ADMIN reachability | Structurally unreachable from registration: user creation only writes to `users`; every `user_roles` write in the codebase is either the transaction-scoped `BUSINESS_OWNER` assignment in `tenantService.Create` or the `role.assign`-gated `AssignmentService` — neither is reachable from an unauthenticated caller. |
| Routes (`internal/app/app.go`) | `POST /users` (public), `POST/GET /auth/*`, `GET /public/tenants/{slug}` (public), `GET /users/{id}` (self-only), `POST/GET /tenants`, `GET/PATCH /tenants/{tenantID}`, `POST /tenants/{tenantID}/members`, `DELETE .../members/{userID}`, `POST .../role-assignments`, `GET .../permissions`. CORS added this session (`ALLOWED_ORIGINS`). |
| Backend's own Epic 02 roadmap (`md files/EPIC-02-TENANT-MANAGEMENT.md`) | Features 1–5 done. Feature 6 (context resolution) exists in substance already (`tenant.Middleware`), just not under that name. Features 7 (isolation test matrix), 8 (lifecycle: SUSPENDED/INACTIVE), 9 (settings foundation), 10 (branding), 11 (audit), 12 (error hardening) are **not built**. |

### Frontend (`Saas-frontend`) — verified by reading source

| Area | Actual state |
|---|---|
| Auth | `token-store.ts` — **memory-only**, deliberately never persisted (documented rationale: backend has no cookie mechanism at all, so memory-only is the smallest-exposure option; a hard reload signs the user out). `auth-provider.tsx` — `register()` already does `POST /users` then immediately `login()` with the same credentials; this matches the doc's "Register → authenticate" step exactly, no backend auto-login exists or is needed. |
| Route protection | `ProtectedRoute` — client-only gate (no middleware possible, since there's no server-readable session artifact); redirects to `/login?redirect=<path>`. |
| Tenant selection | `TenantProvider` — auto-selects when exactly one tenant, otherwise `null` until an explicit `TenantSelector` pick; selection persisted only as a **tenant ID** in `localStorage` (never trusted as authorization — every request re-verifies). `TenantGate` — redirects to `/onboarding` whenever `availableTenants.length === 0`. Its own code comment already anticipates this needing revision once tenant listing is "really" wired up. |
| Onboarding | `app/onboarding/page.tsx` renders exactly one component, `CreateTenantForm` — **name + slug only**, calls `POST /v1/tenants`, then `router.push('/dashboard')`. No business type, no multi-step, no resume concept, no draft state anywhere. |
| Permissions | `PermissionsProvider` sources `GET /v1/tenants/{id}/permissions`, scoped to `currentTenant`, fails closed to an empty `Set` on any unresolved state. `Can`/`useCan`/`can` explicitly documented as UX-only, never a security boundary — and explicitly documented as **tenant-scoped only**, not to be reused for `/admin`. |
| Dashboard | `DashboardShell` — explicitly structural-only per its own comment ("No business-feature UI lives here"). `Sidebar` reads a config array (`dashboardNavItems`) and filters by `permission`. `dashboard/page.tsx` (today's F13) renders literally "Welcome back, {email}" and nothing else — no fabricated metrics. |
| Admin | `/admin` layout has **zero role/permission gating today** — explicitly called out in its own comment as "out of scope for this phase." |
| API/error layer | `apiClient` — single-flight refresh-and-retry on 401, `ApiError` normalizes `{code, status, message}`, callers branch on `code` only. `types/tenant.ts`, `types/auth.ts`, `types/permission.ts` all carry comments cross-referencing the exact Go source they mirror. |
| Marketing copy | `components/marketing/BusinessTypes.tsx` lists a **different, generic** set of business categories (Salons, Wellness, Photography, Consulting, Tutoring, Repair Services) — unrelated to the four verticals in this plan. Cosmetic, not a product conflict; flagged in §28. |

Both repos are unusually well-documented in-line (comments cite the exact feature/decision that produced each piece of code), which is why this plan can cite specific files/lines rather than inferring behavior.

---

## 3. Baseline vs. Repository Comparison

| Document Assumption | Repository Reality | Decision |
|---|---|---|
| Registration may need to "invent" auto-authentication | `register()` already chains `POST /users` → `login()` client-side; backend was never expected to auto-login | **No change.** Already correct, see §4. |
| Tenant creation timing is an "open decision," Option A vs B | Feature 2 already requires `slug` (not just `name`) inside one atomic transaction; there is no draft persistence anywhere to build Option A on top of | **Option B, extended** — see §5. |
| `tenants.status` lifecycle is `ACTIVE / SUSPENDED / INACTIVE` | Actual DB constraint is `ACTIVE / DISABLED` only (Epic 02 Feature 8, lifecycle management, was never built) | Do not touch `status` at all for onboarding. Use a **separate** column — see §8. |
| Onboarding state "possibly" belongs on tenants/settings/dedicated resource | No settings table exists (Feature 9 not built); tenants already carries scalar identity fields (`slug`, soon `business_type`) queried on every fetch | **On `tenants` directly**, workflow-state columns only — see §8. |
| `/dashboard` needs "common vs. vertical" module architecture designed from scratch | `Sidebar` already reads a filtered config array (`dashboardNavItems`); the filter predicate (`!item.permission || can(...)`) is trivially extensible to a second predicate | **Extend the existing config pattern** — see §17. |
| Public tenant route "must resolve business_type to the right vertical" | `PublicTenantIdentity` already exists, already ACTIVE-gated, already hides reserved/disabled tenants identically; it just doesn't carry `business_type` yet | **Additive field**, same visibility function, plus an onboarding-completion gate — see §19. |
| "Must never assign SUPER_ADMIN from registration" | Already structurally impossible — no code path from `POST /users` reaches `user_roles` | **No change needed; documented as verified**, not re-implemented. |
| Marketing page should presumably reflect the four verticals | `BusinessTypes.tsx` shows six unrelated generic categories | **Flagged as a later cosmetic follow-up, not part of this architecture pass.** |

No conflict required silently changing established backend behavior. The one place this plan **does** change existing behavior is public visibility (§8, §19) — called out explicitly, not silent.

---

## 4. Registration Flow Decision

**Repository fact:** `POST /api/v1/users` returns only `PublicUser` — no tokens (confirmed in `user_handler.go` and `authentication_handler.go`; only `Login`/`Refresh` call `writeAuthenticationResult`). The frontend's `AuthProvider.register()` already compensates by chaining `authApi.register()` → `authApi.login()` with the same credentials, storing tokens from the login response.

**Decision: keep this exact flow.** It is already the "cleanest flow consistent with the existing auth architecture" the document asks for. Do not add backend auto-login (would be a real Epic 01 change, explicitly out of scope) and do not duplicate the chaining logic anywhere else.

**What's missing** is only the *next* hop: after `register()` resolves, `RegisterForm` currently does `router.replace(redirectTo ?? '/dashboard')`. Since a brand-new user has zero tenants, `TenantGate` already catches this and bounces to `/onboarding` — so the flow is accidentally correct today, but only because `/onboarding` doesn't yet care about business type. §11 formalizes this as the resume algorithm's first branch rather than an accident of `TenantGate`'s current zero-tenant check.

---

## 5. Tenant Creation Timing Decision

Document Option A (draft → create tenant at the end) vs. Option B (create tenant early, configure after).

**Decision: Option B, and it requires no change to Feature 2's atomicity.** Reasoning from evidence, not preference:

1. Feature 2 already creates tenant + `ACTIVE` membership + `BUSINESS_OWNER` assignment in one transaction, and already requires a **validated, unique slug** before `BeginTx` even opens (`model.ValidateSlug` runs first; a bad slug never touches the DB). Building Option A means re-implementing slug reservation, uniqueness racing, and eventual promotion-to-real-tenant — all of which Feature 2 already solved, just not under the name "draft."
2. A draft-then-promote design would need its own persistence for exactly the same fields `tenants` already has (name, slug, description, contact, timezone) — this is the "JSON dumping ground" anti-pattern the document itself warns against in §9/§19, just relocated to a draft table instead of a JSON blob.
3. Option B needs a **tenant ID immediately**, which every subsequent onboarding step (vertical config, later business-domain tables) will foreign-key against. Option A defers that ID until the end, which is backwards for exactly the multi-step, resumable flow the document wants.

**The only real risk Option B introduces** — an incompletely-configured tenant being just as `ACTIVE` and just as publicly visible as a finished one — is real today even under the *current* one-step onboarding, and is resolved directly in §8, not by avoiding Option B.

**Concretely:** `CreateTenantInput` gains `BusinessType`; `tenantService.Create` sets it plus `onboarding_status = IN_PROGRESS` and an initial `onboarding_step` in the same transaction it already runs. No new transaction, no new atomicity boundary.

---

## 6. Business Type Decision

**Where it belongs: `tenants.business_type`, a fourth identity-like scalar column — not a settings table, not a separate vertical-config table, not JSON.**

Evaluated against the document's own criteria, using repository precedent:

- **Domain meaning / query frequency:** `business_type` is read on nearly every tenant-scoped request that matters for this architecture — dashboard rendering, public tenant resolution, onboarding step routing. `slug` is the closest existing precedent (also read on almost every fetch, also identity-like) and it lives directly on `tenants`, not in a side table. A separate table would force a join on every one of those hot paths for a single scalar value.
- **Mutability:** Low (see §7) — closer to `slug`'s "validated once, not normalized, not casually changed" treatment than to `description`/`contact_email`'s "edit anytime" treatment. Still fits the same table; `tenants` already mixes both mutability profiles (`slug` vs. `description`).
- **Tenant isolation:** No isolation implication either way — it's a scalar attribute of one tenant row, same as everything else already there.
- **Future verticals:** Adding a fifth vertical later is a one-line addition to a validation list (same shape as `reservedSlugs`) plus a widened `CHECK` constraint via a new migration — no schema redesign.
- **Contrast with what does NOT belong here:** vertical-specific *domain data* (a hotel's room types, a nail tenant's service catalog) is not 1:1 scalar tenant metadata — it's a real collection with its own lifecycle, and belongs in dedicated per-vertical tables designed when each vertical's backend work actually starts (§16). Putting `room_type_id`/`table_id`/`vehicle_id`-shaped nullable columns on `tenants` or one shared table is exactly the anti-pattern the document's §19 warns against, and this plan does not do it.

**Canonical values**, following the existing `Status`/`Scope` enum convention (`VARCHAR` + `CHECK`, uppercase, underscore-separated — matching `ACTIVE`/`DISABLED`/`PLATFORM`/`TENANT`, not the dotted lowercase style used for *permission codes*, which are a different kind of value) — **as a Go-typed string enum, not a bare `string` field**, matching how `model.Status` and `authzmodel.Scope` are already declared:

```go
type BusinessType string

const (
    BusinessTypeNailTechnician BusinessType = "NAIL_TECHNICIAN"
    BusinessTypeRestaurant     BusinessType = "RESTAURANT"
    BusinessTypeHotel          BusinessType = "HOTEL"
    BusinessTypeTransport      BusinessType = "TRANSPORT"
)
```

Validate server-side with a dedicated function mirroring `model.ValidateSlug`'s structure (fixed allow-list, not client-driven), living in `internal/tenant/model` alongside it, and returning the typed `BusinessType` rather than leaving callers to compare raw strings.

**`OnboardingStatus` gets the same typed treatment** (defined in full in §8, since it's a workflow-state concept rather than a business-identity one, but declared the same way):

```go
type OnboardingStatus string

const (
    OnboardingStatusInProgress OnboardingStatus = "IN_PROGRESS"
    OnboardingStatusCompleted  OnboardingStatus = "COMPLETED"
)
```

Note there is no `OnboardingStatusNotStarted` constant. Under the Option B model (§5), a tenant row does not exist until `business_type` + name + slug are submitted together in one `POST /tenants` call — so a persisted tenant is *born* `IN_PROGRESS`; a `NOT_STARTED` state would describe a moment that never occurs for any real row. Carrying it in the type (and the DB `CHECK` constraint) would be dead code inviting an unreachable branch somewhere downstream. See §8 for the corresponding migration/`CHECK` constraint.

**`OnboardingStep` stays a plain, service-validated string, not a typed enum** — the valid step sequence differs per `BusinessType` (§11–§16), so a single Go/DB enum can't express "valid value depends on another field" cleanly. It is never trusted as an arbitrary client-supplied value: every write to it is validated server-side against that tenant's vertical step list (§8, §21) before being persisted, the same way `ValidateSlug`'s pattern-and-reserved-word checks gate `slug` without `slug` itself being a Go enum.

---

## 7. Business Type Mutability

**Decision (non-negotiable, not a soft default): `business_type` is chosen once, before/at tenant creation, and is immutable through every ordinary endpoint for the tenant's entire lifetime — not just "until completion."** This is a firmer position than the plan's first draft (which allowed changes while still `IN_PROGRESS`): the moment a tenant row exists at all, its vertical is fixed. Onboarding steps *within* that vertical can still be revised freely (§8's `onboarding_step` bookkeeping), but the vertical itself cannot.

Domain justification, directly from the document's own hotel→restaurant example: once any vertical-specific domain table exists for a tenant (rooms, rates — once they're built), it's keyed to that tenant under the assumption its `business_type` is fixed. Allowing `business_type` to change at any point — mid-onboarding or after — would orphan that data with no defined migration path, and no such migration path is in scope for this plan or the source document (§18's explicit non-goal: don't design the vertical booking engines).

**Enforcement, named explicitly:** `PATCH /api/v1/tenants/{id}` — the existing Feature 4 profile-update endpoint, and the *only* endpoint `TenantHandler.UpdateProfile` backs — must never gain a `business_type` field. `UpdateTenantProfileRequest` simply has no `BusinessType` field today and must not acquire one; there is no code path to change it once past the single endpoint that ever sets it (tenant creation, `POST /tenants`, one time, at F6). No new "is this allowed" branching logic is needed — the absence of the field on that struct *is* the enforcement, the same pattern already used for `slug` immutability. This is restated as an explicit non-change in §30 precisely so a future feature doesn't casually add it to the profile PATCH as a convenience.

If a real business later needs to pivot verticals, that is a deliberate, rare, administratively-gated operation — explicitly **out of scope**, flagged as a risk in §28, not designed here. It would be a dedicated migration workflow (likely support/admin-initiated, re-validating or discarding vertical domain data), never a field on a self-service endpoint.

---

## 8. Onboarding Persistence Decision

**Two new columns on `tenants`, decoupled from the existing `status` column:**

```
onboarding_status  VARCHAR(16) NOT NULL DEFAULT 'COMPLETED'   -- IN_PROGRESS | COMPLETED (no NOT_STARTED — see §6)
onboarding_step    VARCHAR(64) NULL                            -- resume pointer, e.g. "services", "room_types"
business_type      VARCHAR(32) NULL                            -- see §6/§7 — nullable strategy below
```

**Why the migration default is `COMPLETED`, not `IN_PROGRESS`:** every tenant row that exists today predates onboarding entirely — it was created through the current one-step flow and is a fully real, in-use tenant. Defaulting existing rows to anything other than `COMPLETED` would make them vanish from public visibility the moment §19's new gate ships (see below). Going forward, the *application layer* — not the column default — explicitly sets `onboarding_status = IN_PROGRESS` on every new `Create()` call from the onboarding flow, the same pattern `PostgresTenantRepository.Create` already uses for `status` defaulting (`if status == "" { status = model.StatusActive }`).

**Nullability strategy for `business_type` — stated explicitly so `*string` doesn't become an accidental permanent domain rule:**

- **DB column: nullable.** Every tenant row created before F1 ships has no `business_type` and cannot retroactively be assigned a correct one — there is no data anywhere to infer it from. The column must accept `NULL` for those legacy rows, full stop; a `NOT NULL` migration would either fail outright or require fabricating values for real tenants, which is worse.
- **New tenant creation: required, enforced at the service boundary, not the DB.** From F6 onward, `POST /tenants` requires a valid `business_type` in the request; `tenantService.Create` rejects a missing/invalid one with `VALIDATION_FAILED` *before* `BeginTx` (same pre-transaction pattern already used for slug validation, §5). In practice, then, **every tenant created after F1/F6 ship has a non-null `business_type`** — nullability exists only as a legacy-data accommodation, not as an ongoing product state a new tenant can be created into.
- **Consumers must treat `business_type == nil` as a real, permanent case for pre-existing tenants** — not a transient state that resolves itself, and not something to crash on. See §7 (F13/F7) for how the dashboard renders it, and §19 for how the public route handles it (a `nil` `business_type` on an otherwise-`ACTIVE`+`COMPLETED` legacy tenant must not break public resolution — it renders with no vertical branch, same shape as today's behavior before this plan existed).

**Why this is not the existing `status` column:** `status` (`ACTIVE`/`DISABLED`) is Epic 01/02's tenant *lifecycle* concept — suspension, disabling — and multiple existing call sites already depend on its exact two-value meaning (`ListAccessibleByUserID`'s `WHERE t.status = 'ACTIVE'`, `PublicTenantService`'s ACTIVE-only check, `TenantContextService.Resolve`'s active-tenant check). Overloading it with a third meaning ("mid-onboarding") would require auditing and changing every one of those call sites and risks silently changing what "ACTIVE" has meant since Feature 1. A parallel column costs one migration and changes zero existing query semantics.

**Workflow state vs. domain data**, per the document's own distinction (§9 of the doc):

- **Workflow state** (this plan, now): `business_type`, `onboarding_status`, `onboarding_step` — small scalars, no independent lifecycle, always read together with the tenant row.
- **Business domain data** (later, per-vertical, not this pass): a hotel's room types, a nail tenant's service catalog. Under the Option B model (§5), there is no "draft" version of this data to design a persistence format for — each vertical step, once built, will write directly to its real destination table, the same way the *existing* common onboarding step (today's `CreateTenantForm`) already writes directly to the real `tenants` row rather than a draft. This sidesteps the document's own warning against a single dumping-ground JSON blob entirely, by never introducing one.

**Why `onboarding_step` is a free-form string, not an enum/CHECK:** the valid step sequence differs per `business_type` (§11–§16), so a single DB-level enum would either need one column per vertical or a constraint that can't express "valid step depends on another column's value" cleanly in Postgres without a trigger. The step *values* are validated at the service layer against that tenant's vertical step list (same spirit as `ValidateSlug`'s character-level check being separate from the reserved-word check) — the DB only guarantees a string is present or null.

---

## 9. Resume Algorithm

**Stated explicitly, since it's easy to get backwards: onboarding state is a property of the *tenant*, never of the *user*.** There is no `users.onboarding_step` and none should ever be added — a user can own multiple tenants (§10) at different onboarding stages simultaneously, so "the user's onboarding step" is not a well-formed concept. Every read/write below targets `selectedTenant.onboarding_status` / `.onboarding_step`, never anything hung off the authenticated principal or session.

Runs after every successful authentication (login, register→login, or app boot with a still-valid access token) and on every protected-route entry — it is the same decision `TenantGate` already partially makes, extended:

```
resolve availableTenants (existing: GET /v1/tenants, unchanged)
resolve currentTenant (existing TenantProvider logic, unchanged):
    - persisted tenant id matches an available tenant → that one
    - exactly one available tenant → auto-select it
    - otherwise → null (ambiguous; needs explicit selection)

if availableTenants.length === 0:
    → /onboarding/new                                   (create first tenant)

else if currentTenant === null:
    → render TenantSelector (existing pattern), do not guess
      selector must visually distinguish IN_PROGRESS tenants (§10)

else if currentTenant.onboarding_status !== 'COMPLETED':
    → /onboarding/{currentTenant.id}                    (resume at onboarding_step)

else:
    → proceed to /dashboard (or /admin, unaffected — see §20)
```

**Direct URL navigation:** if a user hits `/dashboard` directly while `currentTenant` is incomplete, `TenantGate` performs the same check inline (it already sits in the layout tree above `DashboardShell`) and redirects — no separate handling needed, this is exactly what `TenantGate` already does for the zero-tenant case today, extended with one more condition.

**Revoked membership / disabled tenant:** already handled — `ListAccessibleByUserID` filters both out at the SQL level, so such a tenant simply never appears in `availableTenants`; no new logic needed.

**SUPER_ADMIN:** entirely orthogonal — a platform role carries no tenant membership and this algorithm only runs for tenant-scoped routes. `/admin` boundary is §20.

---

## 10. Multiple-Tenant Behavior

The system already supports N tenants per user with no schema changes (`tenant_memberships` has no cardinality constraint per user; `ListAccessibleByUserID` has no `LIMIT`). Because `onboarding_status`/`business_type` live on the **tenant row**, not the user or session, a user can simultaneously own a `COMPLETED` `NAIL_TECHNICIAN` tenant and an `IN_PROGRESS` `HOTEL` tenant with zero conflict — completion state travels with the tenant, never with the user.

**Deterministic rules:**

- **Creating an additional tenant:** an existing `BUSINESS_OWNER` navigates to `/onboarding/new` same as a first-time user (no special-casing needed — `POST /v1/tenants` already has no "one tenant per user" constraint anywhere in the schema or service).
- **Existing owners starting a second onboarding while the first is incomplete:** allowed. Nothing today prevents multiple simultaneous `IN_PROGRESS` tenants for one user, and there's no product reason in the source document to block it.
- **Incomplete Tenant B does not affect completed Tenant A:** guaranteed structurally — every query and permission check in the codebase is already tenant-scoped by `tenant_id`; nothing aggregates across tenants.
- **Login should not auto-resume an incomplete tenant when a completed one already exists:** per §9's algorithm, if `currentTenant` resolves to the completed tenant (via persisted selection or single-tenant auto-select), the user lands on `/dashboard`, not forced into resuming Tenant B. Resuming Tenant B only happens if the user explicitly selects it via `TenantSelector` — auto-selection only ever fires when there is exactly **one** available tenant total, an existing `TenantProvider` rule this plan does not change.
- **`TenantSelector` representation of incomplete tenants:** extend the existing `<option>` rendering to show a lightweight "Draft" or "Setup incomplete" suffix/badge when `tenant.onboarding_status !== 'COMPLETED'`, so the owner can tell them apart before picking. This is a presentational change only to an already-existing component — no new component needed.

**Recorded product rule, not a backend restriction:** allowing unlimited `IN_PROGRESS` tenants per user (Tenant A, B, C, ... all incomplete) is structurally fine and this plan does not add a cardinality limit. But left unchecked it can produce abandoned-workspace clutter. No backend enforcement is proposed now; the rule to carry forward is a UX one — the "Draft" labeling above, plus (at F5/F6 implementation time) framing `/onboarding/new`'s entry point as "resume an existing draft" first when incomplete tenants exist, rather than defaulting straight to "create another." This is a note for future refinement, not a gate.

---

## 11. Shared Onboarding Architecture

**Folder boundaries**, extending existing conventions rather than inventing new ones (compare to the existing `dashboardNavItems` config-array pattern and the existing `modules/tenant/{api,queries,keys}.ts` triad):

```
src/app/onboarding/
  layout.tsx                    # ProtectedRoute wrapper — unchanged
  page.tsx                      # NEW routing-only: 0 tenants → /new; incomplete → /{tenantId}; else /dashboard
  new/page.tsx                  # Phase 1 (F6): business type + name + slug → POST /tenants → tenant created IN_PROGRESS
  [tenantId]/
    layout.tsx                  # OnboardingShell: resolves tenant, guards ownership, renders step chrome
    business-profile/page.tsx   # Phase 2 (F6): description/contact/timezone → existing PATCH /tenants/{id} + save step
    [step]/page.tsx             # dynamic step router for later vertical-specific steps — looks up the step component from the vertical's config

src/lib/onboarding/
  steps.ts                      # per-business_type ordered step list — same idiom as lib/navigation/dashboard-nav.ts

src/components/onboarding/
  onboarding-shell.tsx          # step indicator + Back/Continue chrome
  step-indicator.tsx

src/modules/onboarding/
  api.ts                        # PATCH .../onboarding, POST .../onboarding/complete (see §21)
  queries.ts
  keys.ts
```

**Why this avoids "four duplicate onboarding applications":** exactly one shell, one step-router page, and a data-driven step list per vertical (a plain array/object keyed by `business_type`, the same shape as `dashboardNavItems`). A vertical adds a row to `steps.ts` and however many step components it needs — never a parallel copy of the shell, routing, save/resume logic, or validation boundary.

**Save strategy (§10/§9 of the doc's numbering):** save on **step completion** (Continue/Back), not on every keystroke and not only at a final review screen. Concretely: `PATCH /tenants/{id}/onboarding` fires once when the user advances past a step, carrying that step's already-validated field values (routed through the *existing* `UpdateProfile`-style endpoint for common fields, and future vertical endpoints for vertical fields) plus the new `current_step` pointer. This balances the document's stated goals: bounded API traffic (one call per step, not per keystroke), a natural validation boundary (the step's own form validates before the call fires), and real recoverability (a browser crash mid-step loses at most that one unsaved step, never the whole flow). **On save failure:** surface inline, keep the user on the current step, do not advance `current_step` or navigate — retry is just resubmitting the same step.

---

## 12–15. Vertical Baselines

These identify **domain boundaries only**, per the document's explicit instruction not to design every table yet. No backend tables are proposed here; each vertical's real backend domain work is a separate future planning pass (flagged in §26/§28).

### 12. Nail Technician
Onboarding: business profile → location/service model → services → staff/technicians → working hours → booking rules → branding → review.
Customer flow: service → technician → date → time → customer details → confirmation.
Core resources (future): services (duration-bearing), staff/technicians (availability-bearing), appointments. Availability is a *function of* staff × service duration, not a standalone inventory the way hotel rooms are.

### 13. Restaurant
Onboarding: business profile → location → opening hours → dining configuration → tables/capacity → reservation rules → menu/public info → branding → review.
Customer flow: date → time → party size → availability → customer details → confirmation.
Core resources (future): tables/capacity (a count/layout, not individually bookable the way a hotel room is), opening hours, reservations. No per-unit resource identity is required the way a hotel room or transport seat needs one — capacity is aggregate.

### 14. Hotel
Onboarding: property → location → amenities → room types → room inventory → occupancy/rates → check-in/out rules → policies → branding → review.
Customer flow: check-in → check-out → guests → available rooms → room selection → guest details → reservation.
Core resources (future): room types (a category), rooms (individual inventory units under a type), rates (date-range-dependent pricing), stays (date-range bookings against a specific room). This is the only vertical with true date-range inventory (occupancy across a span), distinct from a single-slot appointment or reservation.

### 15. Transport
Onboarding: transport company → operating locations → routes → vehicles → seat/capacity configuration → schedules → pricing → booking rules → branding → review.
Customer flow: origin → destination → travel date → available trips → select trip → seat/passenger → details → booking.
Core resources (future): routes (origin/destination pairs), vehicles (capacity-bearing), scheduled trips (a vehicle × route × time instance), seats (individually assignable, closer to hotel rooms than to restaurant tables).

**Cross-vertical observation feeding §16:** three distinct availability shapes already emerge — *staff-duration-based* (nail), *aggregate-capacity* (restaurant), and *individually-assigned-unit* (hotel rooms, transport seats). Forcing one generic "resource" table across all three would need nullable `room_id`/`table_id`/`vehicle_id`/`technician_id` columns on every booking row — the exact anti-pattern §16 (and the source document's §19) rejects.

---

## 16. Booking-Domain Boundary Analysis

**Genuinely shared, safe to design once (when the time comes):**
- **Tenant** — already exists, already the isolation boundary.
- **Customer** — end-user identity for a booking, likely tenant-scoped, not yet modeled anywhere.
- **Booking identity/status** — every vertical produces *something* with an ID and a lifecycle (`PENDING → CONFIRMED → CANCELLED`, or similar); the *status vocabulary* and *identity concept* can be shared even if the row shape can't.
- **Money** — pricing/currency representation, if/when payments enter scope (currently explicitly out of scope, per both this document and the backend's Epic 02 non-goals list).
- **Contact** — customer contact fields, likely reusable across verticals.
- **Time** — date/time/timezone handling conventions (the tenant already carries `timezone`).
- **Policies** — cancellation/booking-rule text, structurally similar across verticals even if the rules differ.

**Not shared — must stay vertical-specific:**
- The bookable **resource** itself (technician, table, room, vehicle/seat) — different identity, different availability computation, different cardinality.
- Availability computation logic — staff-schedule math, capacity math, and date-range inventory math are three different algorithms, not one parameterized one.

**Explicit rejection, stated for the record:** no shared `bookings` table with nullable `room_id / table_id / vehicle_id / technician_id` columns. If a shared `Booking` concept is designed later, it should carry a `resource_type` + a foreign key into exactly one vertical-specific resource table (or be split per vertical outright) — a decision for that future pass, not this one.

---

## 17. Dashboard Architecture

**Decision: extend the existing config-driven `Sidebar`/`dashboardNavItems` pattern; do not build four dashboard shells.**

`Sidebar` already does `dashboardNavItems.filter(item => !item.permission || can(permissions, item.permission))`. Extend `NavItem` with an optional `businessTypes?: BusinessType[]` field and extend the filter to `(!item.permission || can(...)) && (!item.businessTypes || item.businessTypes.includes(currentTenant.business_type))`. Each vertical's dashboard module (appointments/services/staff for nail; reservations/capacity for restaurant; etc.) is then just additional rows in the same config array plus its own route under `/dashboard/*` — `DashboardShell` itself (sidebar chrome, header, tenant selector, account menu) never changes per vertical, matching its own "structural shell only" charter.

This is not designed further here — no vertical dashboard *pages* are specified — because no vertical backend domain exists yet to render (§16). The architecture only needs to prove it *can* branch without duplicating the shell, which the existing filter pattern already does for permissions today.

---

## 18. Revised F13 Scope

**Old F13** (today's `dashboard/page.tsx`): "Welcome back, {email}." Deliberately minimal, and correctly so — no fabricated data exists to show.

**Revised F13 — "Business Owner Dashboard Foundation":**
- Tenant name and a human label for `business_type` (e.g. "Hotel" from `HOTEL`) — both real fields, already available the moment §6 ships.
- If `onboarding_status !== 'COMPLETED'`: a visible "Finish setting up your workspace" banner linking to `/onboarding/{tenantId}` — real, derivable state, not fabricated.
- Nothing else. No booking counts, no revenue, no occupancy, no charts — none of that data exists in the backend yet, and inventing it would violate the document's explicit non-goal.

**Three render rules, stated explicitly so they aren't left to implementation-time guessing:**
1. `onboarding_status === 'COMPLETED'` → **no** setup banner, ever — a completed tenant's dashboard looks exactly like today's minimal dashboard, plus the business-type label.
2. `onboarding_status === 'IN_PROGRESS'` → setup banner shown, linking to resume.
3. `business_type === null` (a legacy, pre-F1 tenant — see §8's nullability strategy) → render a safe generic label (e.g. no vertical badge, or "—") instead of the human-readable type name, and **must not throw/crash** on the missing value. This is the one case implementation must not skip: every pre-existing tenant in the database hits this path the day F1 ships, so it is not an edge case, it is the majority case until real vertical adoption grows.

This is a small, honest addition — not a redesign — and is the last item in this plan's feature breakdown that touches the dashboard page itself (§26, F7).

---

## 19. Public Customer Architecture

**Backend (this is F3 — already fully specified there, restated here as the formula it resolves to):**

```
publicly visible  ⟺  tenant.Status == ACTIVE  AND  tenant.OnboardingStatus == COMPLETED
```

`PublicTenantIdentity` gains `BusinessType *string` (additive, matches the existing optional-field style already used for `Description`/`Timezone`). `PublicTenantService.GetBySlug` gains one more condition:

```
if tenant.Status != ACTIVE            → TENANT_NOT_FOUND   (existing)
if tenant.OnboardingStatus != COMPLETED → TENANT_NOT_FOUND  (NEW)
```

Both collapse to the same response shape/code, preserving the existing "disabled and nonexistent are indistinguishable" privacy property for "still onboarding and nonexistent" too — no new information leaks about which slugs are reserved-but-pending versus genuinely free. This is the precise sense in which `ACTIVE` is *necessary but not sufficient* for public launch: `ACTIVE` has always meant (and continues to mean) "not disabled/revoked"; `COMPLETED` is the new, separate condition meaning "business setup is ready for public exposure." Conflating the two — or launching F1/F6 (which can produce `IN_PROGRESS` tenants) without F3 already live — would reopen exactly the gap this formula closes; see §28.

**Frontend (this is F8, and it is frontend work almost entirely — the backend side is done once F3 ships `business_type` on the public response):** one dynamic route, `app/(public)/book/[slug]/page.tsx` — note `book` is already in the backend's `reservedSlugs` list, so this exact path prefix was already anticipated. It fetches public identity once, then branches its render on `business_type` to mount the corresponding vertical experience component. One route, four render branches — not four routes. SEO/mobile-responsiveness considerations are unaffected by this choice (a single dynamic route with server-rendered metadata per slug is standard Next.js App Router capability, not a new pattern). Native/mobile-app reuse is naturally supported later since the branch point is a single API response field, not client-side routing structure.

No vertical experience component is built in this pass — only the routing/branch point, since no vertical backend domain exists to render yet (§16).

---

## 20. Super Admin Boundary

**Not designed in this pass**, per the document's explicit instruction — only the boundary needed so this architecture doesn't conflict with it later:

- `/admin` and `/dashboard` remain fully separate route groups (already true — `(platform)/admin` vs. `(dashboard)/dashboard`).
- Nothing in this plan gives `/admin` any onboarding/business_type awareness — it has none today and needs none for tenant onboarding to work.
- When `/admin` eventually gets real gating, it must use `Authorizer.RequirePlatformPermission` (already exists, already fail-closed) — never the tenant-scoped `Can`/`useCan`/`permissions-provider.tsx`, which is already explicitly documented as tenant-scoped only.
- Verified, not re-verified by new code: public registration cannot reach `SUPER_ADMIN` (§2 table, §3 table) — this remains true after every change in this plan, since none of them touch `user_roles` or the registration handler.

---

## 21. API Contract Plan

Extends existing endpoints wherever semantically correct; two new endpoints only where a field-level PATCH doesn't fit.

**Extend `POST /api/v1/tenants`** (Feature 2) — **this is an intentional, controlled evolution of an existing public API contract, not a silent addition, and F1's plan entry says so explicitly:**
```
Request (today):  { "name": "...", "slug": "..." }
Request (F6+):     { "name": "...", "slug": "...", "business_type": "HOTEL" }   -- now required
Response: PublicTenant + { "business_type": "HOTEL", "onboarding_status": "IN_PROGRESS", "onboarding_step": "<first step>" }
```
`business_type` becomes a required request field from F6 onward (§8's service-boundary enforcement). This changes what a valid request body looks like — existing Postman collections, integration test fixtures, and any external API consumer sending only `{name, slug}` will need to add it. That is the point of the change, not an accident of it; see F1's acceptance criteria for how this is reflected in test expectations rather than papered over.

**Extend `GET /api/v1/tenants`, `GET /api/v1/tenants/{id}`** (Feature 3, additive fields only):
```
Response: existing PublicTenant fields + business_type, onboarding_status, onboarding_step
```
This means `TenantProvider`'s existing `GET /v1/tenants` call already carries everything §9's resume algorithm needs — no second network call required for the common case.

**New — `PATCH /api/v1/tenants/{id}/onboarding`** (cheap, frequent — fired on every step Continue; **flexible by design**):
```
Request:  { "current_step": "room_types" }
Response: 200, updated tenant (same shape as above)
```
`current_step` is validated against a fixed allow-list of that tenant's `business_type`'s known steps (rejecting an unrecognized value with `VALIDATION_FAILED`) but does **not** enforce sequential ordering — saving progress can move forward, backward, or re-save the same step freely. Reuses the existing `TenantPermissionMiddleware{tenant.update}` authorization already wired for `PATCH /tenants/{id}` (Feature 4) — no new permission code needed.

**New — `POST /api/v1/tenants/{id}/onboarding/complete`** (one-way state transition, not a field write; **strict by design — this is the corrected, mandatory behavior**):
```
Request:  {} 
Response: 200 with onboarding_status: COMPLETED, on success
          422/400 VALIDATION_FAILED (exact status TBD at implementation), on a tenant that hasn't satisfied completion prerequisites
```
This endpoint must **not** be a bare flag flip. It calls a service-owned check — conceptually `ValidateOnboardingCompletion(tenant)` / `CanCompleteOnboarding(tenant)`, living in `internal/tenant/service` alongside `tenantService`, never in the handler and never trusted from the frontend — before transitioning `onboarding_status`. **This check is what stands between a caller and turning `IN_PROGRESS` straight into publicly-launched (§19's formula) with nothing configured; without it, F3's public-visibility gate is trivially bypassable by calling this endpoint immediately after `POST /tenants`.** Modeled as an action endpoint rather than folding into the PATCH above precisely because it is a *validated* irreversible-through-this-endpoint transition, not an arbitrary partial-field write. See §23/§24 for the error contract and security framing, and F2 in §26 for what "prerequisites" means at this stage (the common baseline only — no vertical requirements exist yet).

**Extend `GET /api/v1/public/tenants/{slug}`** (Feature 5, additive field, see §19):
```
Response: existing fields + business_type
```

No REST endpoint explosion: four touch points total, two of them additive fields on endpoints that already exist.

---

## 22. Data/Migration Assessment

**Required for the first onboarding foundation (this plan's scope):**
- One migration: `ADD COLUMN business_type VARCHAR(32) NULL` (nullable — legacy-row accommodation, required at the service boundary for new rows, see §8), `ADD COLUMN onboarding_status VARCHAR(16) NOT NULL DEFAULT 'COMPLETED'`, `ADD COLUMN onboarding_step VARCHAR(64) NULL`, plus a `CHECK` constraint on `onboarding_status IN ('IN_PROGRESS','COMPLETED')` — no `NOT_STARTED` value (§6/§8). (A `CHECK` on `business_type`'s four values is optional — application-layer validation, matching `ValidateSlug`'s own precedent of not relying solely on a DB constraint for a value set likely to grow, is sufficient; a `CHECK` can be added if the team prefers defense-in-depth, at the cost of a migration every time a vertical is added.)
- Corresponding `.down.sql` dropping the three columns.

**Likely later, per vertical (not this pass, not designed here):**
- `nail_technician_services`, `nail_technician_staff`, staff availability
- `restaurant_tables`, capacity/opening-hours config
- `hotel_room_types`, `hotel_rooms`, rates
- `transport_routes`, `transport_vehicles`, schedules
- A `customers` table and a shared booking-identity concept (§16), once a real vertical needs it

**Not required:**
- Any generic `bookings` table (§16)
- Any `tenant_settings` table (Feature 9 of the backend's own roadmap remains genuinely out of scope — nothing in this plan needs configurable settings beyond the three new scalar columns)
- Any audit table (Feature 11 of the backend's own roadmap, unrelated to onboarding)

---

## 23. Error Contract

Reusing existing codes wherever the semantics already match, per the source document's explicit instruction:

| Situation | Code | Why reuse |
|---|---|---|
| Tenant doesn't exist / caller lacks access, during onboarding save/complete | `TENANT_NOT_FOUND` / `TENANT_ACCESS_DENIED` (existing) | Onboarding state *is* the tenant row — there is no separate "onboarding resource" to 404 independently. |
| Invalid `business_type` value | `VALIDATION_FAILED` (existing) | Same category as any other malformed request-body field; not identity/uniqueness-shaped the way slug conflicts are. |
| Invalid/out-of-sequence `current_step` for that tenant's vertical | `VALIDATION_FAILED` (existing) | A business-rule input validation failure, not a new category of error. |
| Calling `.../onboarding/complete` before required steps are satisfied | `VALIDATION_FAILED` (existing) — **the validation itself is mandatory in F2, not optional** (corrected from this plan's first draft; see §21, §24, §28); reusing the existing code is a judgment call for F2's implementation, and a dedicated code may turn out to read better for the frontend to branch on distinctly from generic field validation — that specific choice is left to F2's implementation, the *requirement that some rejection occurs* is not | Same reasoning; this is a request precondition failure, not a new domain concept — but skipping the check entirely is not an option (§21). |
| Attempting to change `business_type` after completion | No new code needed — the field is simply absent from any post-completion-capable request struct (§7), so this can't occur as a distinct error case | Enforcement by omission, matching slug's existing precedent. |

**No new error codes are proposed in this plan.** If implementation reveals a genuine gap (e.g., completion-precondition failures need to be distinguishable from generic validation failures in the frontend), that is a one-line addition to `internal/errors/codes.go` at implementation time — deliberately not pre-invented here per the document's instruction not to create codes during planning.

---

## 24. Security Invariants

Restated against this plan's specific additions, not just the general list:

- **Public registration cannot assign roles or business_type at signup** — `business_type` is only ever set via the authenticated `POST /tenants` flow, never `POST /users`. Verified structurally in §2/§3, unchanged by this plan.
- **Tenant ownership is derived from the principal** — unchanged; `CreatorUserID` still never comes from the request body (§5).
- **Onboarding state is tenant-scoped and inaccessible cross-tenant** — `PATCH .../onboarding` and `POST .../onboarding/complete` both route through the existing `TenantPermissionMiddleware{tenant.update}` chain (Authentication → Tenant Context → Authorization → Handler), the same chain Feature 4 already proved denies cross-tenant access.
- **`business_type` is server-validated** — a fixed allow-list check in `internal/tenant/model`, never trusting the client value beyond membership in that list (§6).
- **Frontend routing is not the security boundary** — `TenantGate`'s new onboarding-completion check (§9) is UX-only, exactly like every existing frontend gate; the backend's `PublicTenantService` gate (§19) is the actual enforcement point for public visibility, and every tenant-scoped write still re-verifies membership server-side regardless of what the frontend believes the onboarding state is.
- **Tenant switching cannot leak vertical configuration** — `PermissionsProvider`'s existing "fails closed on tenant switch, new query key per tenant" behavior already prevents Tenant A's resolved state (permissions today; onboarding state under the same `GET /tenants/{id}` call tomorrow) from bleeding into Tenant B's render; no new leak surface is introduced since this plan adds fields to an already tenant-scoped, already-correctly-invalidated query.
- **Public tenant endpoint exposes only approved public data** — `business_type` is being *added* to that allow-list deliberately (§19); no other new field crosses the public boundary. `onboarding_status`/`onboarding_step` are explicitly **not** added to `PublicTenantIdentity` — they are workflow internals, not public identity.
- **Unfinished onboarding must not accidentally expose private configuration publicly** — this is precisely what §19's new gate exists to prevent; it is the one place this plan changes existing observable behavior, and it's called out three times in this document (§3, §8, §19) rather than buried.
- **Public launch cannot be achieved merely by calling the completion endpoint** — this is the corrected invariant from this plan's revision: `POST /tenants/{id}/onboarding/complete` (§21, F2 in §26) must validate real completion prerequisites server-side before flipping `onboarding_status`. Without this check, §19's `Status == ACTIVE AND OnboardingStatus == COMPLETED` visibility formula is trivially satisfiable by an authenticated owner calling `POST /tenants` then immediately `POST /tenants/{id}/onboarding/complete`, with nothing actually configured — turning the entire onboarding-completion gate into a formality rather than a real launch check. This is treated as a security/business-integrity invariant, not a UX nicety, precisely because F3's public-visibility promise depends on `COMPLETED` meaning something real.

---

## 25. TDD Strategy

**Backend:**
- *Repository/integration:* `business_type`/`onboarding_status`/`onboarding_step` round-trip through `Create`/`FindByID`/`FindBySlug`/`ListAccessibleByUserID`/`UpdateProfile`(unchanged — confirm it does *not* accept `business_type`); migration up/down against the disposable test DB pattern already established this session (`docker-compose.test.yml`).
- *Service:* `tenantService.Create` sets `onboarding_status=IN_PROGRESS` and rejects invalid/missing `business_type` before `BeginTx` (mirroring the existing pre-transaction slug validation test); the step-save service validates `current_step` against that tenant's vertical allow-list (rejecting unrecognized values) without requiring sequential order (§21). **Mandatory:** `ValidateOnboardingCompletion`/`CanCompleteOnboarding` (§21, §24) rejects completion for a tenant that hasn't satisfied the current common-baseline prerequisites, and a positive test proving a tenant that *has* satisfied them completes successfully — both directions must be covered, not just the happy path.
- *Handler/route:* full middleware chain tests for the two new endpoints, following the exact pattern already used for `PATCH /tenants/{id}` (auth required → tenant context → `tenant.update` permission → cross-tenant denial → success), reusing the existing fake-repository test harness style seen in `user_route_test.go`/`tenant_profile_route_test.go`.
- *Security regression:* extend `postgres_tenant_repository_integration_test.go`-style coverage to prove an `IN_PROGRESS` tenant's public slug 404s identically to a nonexistent one (§19); prove `business_type` is absent from any successful `UpdateProfile` round-trip payload (§7's "enforcement by omission").
- *Full-suite regression:* existing Feature 1–5 tests' **behavioral contracts** must remain green (§21, §29) — a fixture that only ever posted `{name, slug}` to `POST /tenants` is expected to need a mechanical `business_type` addition once that field becomes required (F1), since that's this plan's one intentional API-contract change; no test exercising unrelated behavior (slug validation, atomicity, membership creation, cross-tenant denial) should need any change. The public-visibility gate (F3) is new behavior and gets its own new test rather than an edited existing one.

**Frontend:**
- *State transitions:* `TenantProvider`'s `currentTenant` resolution logic unit-tested for the existing cases (zero/one/many tenants, persisted-id match/miss) plus the new incomplete-tenant labeling.
- *Routing:* `TenantGate` tested for all five branches in §9's algorithm, including direct-URL navigation to `/dashboard` while incomplete.
- *Query behavior:* onboarding step-save mutation follows the same optimistic-cache-then-invalidate pattern already proven in `useCreateTenant`/`useUpdateTenantProfile` (`modules/tenant/queries.ts`) — reuse that pattern's test shape.
- *Resume behavior:* a simulated "return after dropoff" test — save step 2, remount the provider tree (simulating a fresh load/different tab), confirm resume lands on step 2 via the server response, not any local cache.
- *Tenant switching:* confirm switching `currentTenant` from a completed to an incomplete tenant re-triggers the resume redirect (not just a stale render).
- *Vertical selection:* config-driven step list resolves the correct step sequence per `business_type`, with an unknown/legacy-null `business_type` failing closed (no steps silently defaulting to one vertical).
- *Fail-closed behavior:* onboarding query errors (network failure, 403) must not be interpreted as "onboarding complete" — mirror `PermissionsProvider`'s existing "empty set on any unresolved state" pattern rather than defaulting optimistically.
- *Legacy null `business_type` rendering:* `TenantSelector`, the dashboard (F7/§18), and any `business_type`-branching UI must render a safe generic state and must not throw when `business_type === null` — this is the majority case for every tenant that exists before F1 ships (§8), not a rare edge case, and needs an explicit test rather than incidental coverage.

---

## 26. Detailed Feature Breakdown

Each feature is independently implementable and testable by a single agent. Names/order revised from this plan's first draft per review — see the summary table below, then full specs.

| Feature | Name | Main purpose |
|---|---|---|
| F1 | Tenant Business Type & Onboarding State Foundation | Persist controlled business type + onboarding state |
| F2 | Tenant Onboarding Progress API | Secure save/resume/**validated** completion state |
| F3 | Onboarding-Aware Public Visibility | Public tenant requires `ACTIVE` + `COMPLETED` |
| F4 | Resume-Aware Tenant Workspace Routing | `TenantGate`/selector understand incomplete tenants (tenant-scoped, never user-scoped) |
| F5 | Shared Onboarding Framework | Reusable shell, steps, save/resume |
| F6 | Tenant Creation & Common Business Setup | Business type/name/slug → create; then common profile (two phases) |
| F7 | Business Owner Dashboard Foundation | Real tenant/type/setup state; vertical-aware nav foundation |
| F8 | Public Tenant Vertical Router Foundation (frontend-primary) | Slug → business type → future vertical experience |

Backend features (F1, F2, F3) precede the frontend features that consume their response fields (F4–F8).

### F1 — Backend: Tenant Business Type & Onboarding State Foundation
- **Goal:** Add the three new columns and wire them through Feature 2's creation transaction and Feature 3's retrieval responses.
- **Backend scope:** Migration (§22, nullable `business_type`, no `NOT_STARTED` value — §8); `model.Tenant` gains `BusinessType *BusinessType, OnboardingStatus OnboardingStatus, OnboardingStep *string` using the **typed enums from §6**, not bare `string`/`*string`; `model.ValidateBusinessType`; `CreateTenantInput.BusinessType` (**required** from this feature onward — §21's contract evolution); `tenantService.Create` rejects a missing/invalid `business_type` before `BeginTx` and sets `OnboardingStatus = OnboardingStatusInProgress`; `PostgresTenantRepository` scan/insert updates across `Create`/`FindByID`/`FindBySlug`/`ListAccessibleByUserID`; `TenantHandler`'s four `PublicTenant`-construction call sites gain the new fields.
- **Frontend scope:** none.
- **Dependencies:** none.
- **Acceptance:** existing Feature 1–5 **behavioral contracts** remain green — a request/response fixture that only ever sent `{name, slug}` to `POST /tenants` is *expected* to need mechanical updating to include `business_type`, since that field becoming required is this feature's explicit, intentional change (§21); a test asserting unrelated behavior (slug validation, membership creation, atomicity) must not need to change. New tests cover: rejected creation on missing/invalid `business_type`; default `onboarding_status` on new vs. legacy rows; migration up/down against the disposable test DB; `business_type == nil` round-tripping correctly for a row inserted before this migration (simulated via direct insert bypassing the now-required application check).
- **Non-goals:** no new endpoints yet (that's F2); no public-identity change yet (that's F3).

### F2 — Backend: Tenant Onboarding Progress API
- **Goal:** `PATCH /tenants/{id}/onboarding` (flexible step-save) and `POST /tenants/{id}/onboarding/complete` (**strict, validated** completion) — §21.
- **Backend scope:** two handler methods + service methods on `TenantService` (or a small new `OnboardingService` if that separation reads more cleanly at implementation time — a call for the implementing agent, not pre-decided here); route wiring reusing the existing `TenantPermissionMiddleware{tenant.update}` chain. **Mandatory for this feature, not deferrable:** a service-owned `ValidateOnboardingCompletion`/`CanCompleteOnboarding` check gating the completion endpoint (§21, §24). At this stage — before any vertical-specific requirements exist (F9+) — "prerequisites" means the current common baseline only: at minimum, that the tenant has a `business_type` and a name (both already guaranteed by F1's creation requirement) and has been through the common business-profile step (F6) rather than being completed the instant it's created. The exact minimum bar is an implementation-time decision within that constraint, but *some* real check must exist — completion must not be reachable by calling the endpoint alone with nothing else having happened.
- **Frontend scope:** none.
- **Dependencies:** F1.
- **Acceptance:** full middleware-chain tests (auth → tenant context → permission → cross-tenant denial → success), matching the existing `PATCH /tenants/{id}` test shape, **plus** a test proving completion is rejected for a freshly-created tenant that hasn't completed the common baseline, and a positive test proving completion succeeds once it has.
- **Non-goals:** step-*order* enforcement on the save endpoint is still not required (saving remains flexible, §21) — only the completion endpoint is strict. No vertical-specific completion requirements yet (F9+).

### F3 — Backend: Onboarding-Aware Public Visibility
- **Goal:** `PublicTenantService.GetBySlug` denies incomplete tenants identically to nonexistent ones, per §19's formula (`ACTIVE AND COMPLETED`); `PublicTenantIdentity` gains `business_type`.
- **Backend scope:** one new condition in `GetBySlug`; one new struct field; handler wiring.
- **Frontend scope:** none.
- **Dependencies:** F1.
- **Acceptance:** new test proves an `IN_PROGRESS` tenant's slug 404s with `TENANT_NOT_FOUND`; existing Feature 5 tests for `ACTIVE`/disabled/reserved-slug behavior remain green unmodified; a legacy tenant with `business_type == nil` but `ACTIVE`+`COMPLETED` still resolves successfully (with a null `business_type` in the response) — this is not a regression case, it's the expected shape for every pre-existing tenant.
- **Non-goals:** no change to owner-facing (`ListAccessibleByUserID`) visibility. **Deployment note carried from §28:** F1 and F3 should not ship independently with a gap between them — an `IN_PROGRESS` tenant is publicly visible until F3 lands.

### F4 — Frontend: Resume-Aware Tenant Workspace Routing
- **Goal:** `TenantGate`/`TenantProvider`/`TenantSelector` become `onboarding_status`-aware per §9/§10 — **strictly tenant-scoped state, never modeled on the user** (§9's explicit statement).
- **Frontend scope:** `types/tenant.ts` gains the new fields; `TenantGate` implements the full branch table in §9; `TenantSelector` labels incomplete tenants.
- **Backend scope:** none (consumes F1's response fields).
- **Dependencies:** F1.
- **Acceptance:** all five §9 branches covered by tests, including direct-URL navigation and the "completed tenant selected, incomplete one exists elsewhere" non-hijack case (§10); a test confirms no onboarding field is ever read from or written to anything user-scoped (auth state, session) — only from the selected tenant object.
- **Non-goals:** no onboarding step UI yet.

### F5 — Frontend: Shared Onboarding Framework
- **Goal:** `OnboardingShell`, step indicator, Back/Continue, `lib/onboarding/steps.ts`, `modules/onboarding/*` wired to F2's endpoints.
- **Frontend scope:** folder restructure per §11's tree; step content can be placeholder/empty at this stage — this feature proves the chrome and persistence work, not the vertical forms. **Correction from this plan's first draft:** a placeholder step screen must not be able to trigger `POST .../onboarding/complete` successfully — since F2 now enforces real prerequisites, clicking through placeholder screens in a tenant that hasn't done the common baseline (F6) will correctly be rejected by the backend; the shell's "Finish"/"Complete" action should surface that rejection inline rather than assuming success, and should not be exposed at all in a build where the common baseline step doesn't exist yet (i.e., F5 alone, before F6 ships, has no working "Complete" button — only Back/Continue/save).
- **Backend scope:** none.
- **Dependencies:** F1, F2, F4.
- **Acceptance:** step indicator renders from config; Continue calls F2's save endpoint and advances; a simulated remount resumes at the persisted step (§25's resume-behavior test); a placeholder-only flow cannot reach `COMPLETED` (asserted against F2's real validation, not mocked away).
- **Non-goals:** no real vertical field forms.

### F6 — Frontend + Backend: Tenant Creation & Common Business Setup
- **Goal:** Two explicit phases, not one blended step (correction from this plan's first draft):
  1. **`/onboarding/new`** — business type + business name + public slug → `POST /tenants` → tenant created `IN_PROGRESS` (today's `CreateTenantForm`, extended with the business-type field).
  2. **`/onboarding/{tenantId}/business-profile`** — description, contact email, contact phone, timezone → the **existing** `PATCH /tenants/{tenantId}` (Feature 4's `UpdateProfile`, unchanged, reused as-is) → then a save-step call to F2 marking this step complete.
- **Backend scope:** likely none beyond F1 — confirm at implementation time whether `UpdateProfile` needs any adjustment for onboarding-context calls (expected: no, and it must **not** gain a `business_type` field regardless — §7).
- **Frontend scope:** `/onboarding/new` page (phase 1); `/onboarding/{tenantId}/business-profile` page (phase 2), reusing the existing `useUpdateTenantProfile` mutation unchanged.
- **Dependencies:** F1, F5.
- **Acceptance:** creating a tenant sets `business_type` + `IN_PROGRESS`; the profile step saves via the existing `useUpdateTenantProfile` mutation with zero changes to that mutation; step completion recorded via F2; **this feature is what makes F2's completion validation satisfiable for the first time** — completing phase 1 + phase 2 is the minimum common baseline F2 checks for.
- **Non-goals:** vertical-specific fields.

### F7 — Frontend: Business Owner Dashboard Foundation
- **Goal:** `NavItem` gains `businessTypes?: BusinessType[]`; `Sidebar` filter extended (§17); `dashboard/page.tsx` becomes the F13 revision in §18, **including its three explicit render rules** (completed → no banner; in-progress → banner; `business_type === null` → safe generic label, never a crash).
- **Frontend scope:** as above.
- **Backend scope:** none (consumes F1's fields).
- **Dependencies:** F1, F4.
- **Acceptance:** dashboard shows tenant name + business-type label + completion banner **only** when incomplete (never for a completed tenant); a legacy tenant with `business_type == nil` renders without throwing; no fabricated metrics anywhere; sidebar filter unit-tested for both permission and business-type predicates independently and combined.

### F8 — Frontend: Public Tenant Vertical Router Foundation
*(Renamed from "F8 — Backend: Public Customer Route Branch Point" — the backend side of this is F3, already fully specified there and complete once F3 ships; this feature is frontend work almost in its entirety.)*
- **Goal:** `app/(public)/book/[slug]/page.tsx` fetches public identity (F3's response, already carrying `business_type`) and branches its render on `business_type`.
- **Frontend scope:** the dynamic route + branch. Given the actual vertical customer flows haven't been planned yet (F9+), the branch should render **at most** a thin, distinct shell per vertical (e.g. "Nail public shell" / "Restaurant public shell" / "Hotel public shell" / "Transport public shell") with no booking form invented — or, equally acceptable, defer even that distinction until F9+'s vertical planning and ship F8 as a single generic "here's the business" placeholder for all four values. Either is consistent with this plan; the one thing F8 must not do is fabricate a booking UI ahead of the domain planning that's supposed to define it.
- **Backend scope:** none — already delivered by F3.
- **Dependencies:** F3.
- **Acceptance:** correct branch (or correct generic placeholder) mounts per `business_type`; a `null` `business_type` (legacy tenant) renders a safe default, not a crash; unknown slug and incomplete-onboarding tenant both render the same "not found" experience (mirroring F3's backend behavior).

### Beyond F8 — deliberately unnumbered, separate future epics
Per review, F9/F10 are dropped as plan entries here — they are not part of this architecture foundation and forcing them into this sequence implied a false continuity with F1–F8. Recorded instead as separate future work:
- **Vertical baselines (Nail / Restaurant / Hotel / Transport):** each needs its own backend domain design (resource tables, availability model — §12–§16) that this plan deliberately does not produce. Suggested order once that planning happens: Nail Technician first (simplest availability shape — staff-duration), then Restaurant (aggregate capacity), then Hotel and Transport together (both individually-assigned-unit inventory, structurally similar). Each vertical will get its own F1-style feature breakdown from its own planning pass.
- **Super Admin workspace:** not designed in this pass, per §20 — whenever `/admin` gains real access control, it is a platform-permission feature entirely independent of everything above, reusing `Authorizer.RequirePlatformPermission` unchanged. Recommended as its own epic, not a numbered continuation of this one.

---

## 27. Implementation Order

**Dependency graph** (what genuinely requires what):

```
F1
 ↓
├──── F2 → F5 → F6
├──── F3 → F8
└──── F4 → F7
```

**Recommended delivery order: strictly sequential, not parallelized across agents, for now:**

```
F1 → F2 → F3 → F4 → F5 → F6 → F7 → F8
```

This is a deliberate simplification over the dependency graph's theoretical parallelism (F2/F3/F4 could technically proceed independently once F1 lands). At this stage — a first pass through a brand-new area of both repositories, implemented and reviewed one feature at a time the same way the backend's Epic 01/02 work already has been — sequential delivery makes regression review tractable: each feature lands, gets tested, gets reviewed, and only then does the next one start, rather than several agents' changes needing to be reconciled against each other mid-flight. Parallelizing F2/F3/F4 (all three only need F1) is a reasonable optimization to revisit once the team has confidence in the pattern from F1 alone — not a default to start with.

**Why F6 sits after F5, not interleaved with it:** F6 exercises both F2 (its two save calls) and F5 (the shell it renders inside) together, so it cannot start before either is done — sequential delivery naturally handles this without needing a separate note in the graph.

Rationale for this order versus the document's original suggested sequence: the document proposed "registration→routing" before "persistent state" before "business type," but repository evidence shows registration→routing (§4) is **already correct** and needs no feature slot at all — it falls out of `TenantGate`'s existing zero-tenant check once F1 exists. Business type and persistent onboarding state are the same migration and the same transaction (§5, §6, §8), so they're one feature (F1), not two sequential ones. Everything else follows the document's proposed sequence closely, reordered only to put all backend work (F1, F2, F3) ahead of all frontend work that depends on it, and to place the dashboard/F13 revision (F7) after the resume gate (F4) rather than before, since F7's completion-banner needs F4's fields resolved in the same provider tree it already sits inside.

---

## 28. Risks

- **Partially configured `ACTIVE` tenants:** the central risk this plan exists to close (§8) — resolved by decoupling `onboarding_status` from `status` and gating public visibility on both (§19). Residual risk: until F3 ships, any tenant created via F1 alone is `IN_PROGRESS` yet still publicly visible (F1 and F3 must not ship independently in production with a gap between them, or ship them as one deploy).
- **Abandoned onboarding:** no cleanup/expiry mechanism is proposed. An `IN_PROGRESS` tenant can sit forever. Acceptable for this pass (matches the document's non-goal on speculative lifecycle work) but worth a product decision later (auto-expire drafts? surface them to the owner as reminders? — not designed here).
- **Multiple incomplete tenants per user:** allowed by design (§10); no backend cardinality limit, and none is proposed — the recorded mitigation is a UX one ("Draft" labeling, resume-first framing at `/onboarding/new`, §10), not a restriction. Acceptable at small scale, worth revisiting if it becomes noisy.
- **Changing business type:** blocked by omission, now stated as non-negotiable rather than a soft default (§7) — but no explicit error message exists for "why can't I change this" — a minor UX gap, not a security one.
- **Completion bypass (resolved in this revision):** this plan's first draft did not mandate that `POST .../onboarding/complete` validate any prerequisites before flipping `onboarding_status`, which would have let a caller reach public visibility (§19's formula) immediately after tenant creation with nothing configured. **This is now a mandatory part of F2** (§21, §24) — the residual risk is scoping *what* counts as "prerequisites satisfied" correctly: F2 only has the common baseline (F6) to check against; per-vertical requirements don't exist until F9+ vertical planning happens, so F2's check is intentionally minimal at first and will need revisiting once each vertical's real mandatory fields are known.
- **Legacy tenants with `business_type == nil`:** every tenant created before F1 ships has (and will indefinitely retain) a null `business_type` (§8) — this is not a transient migration artifact, it is a permanent, common state. F7's three render rules (§18) and F8's branch logic (§26) both handle it explicitly; the risk is a *future* feature forgetting this case exists and assuming `business_type` is always populated, since it will be for every tenant created after F1/F6 ship — worth a standing review note whenever new `business_type`-branching code is added.
- **Vertical configuration leakage:** no new leak surface identified (§24); revisit this assessment once F9+ vertical planning introduces real per-tenant domain tables with their own query patterns.
- **Duplicated frontend flows:** avoided by construction (§11, §17) as long as future vertical work adds to `steps.ts`/`dashboardNavItems` rather than branching the shell/shell components themselves — worth a lint/review reminder when F9+ starts, not a technical safeguard today.
- **Giant generic booking model:** explicitly rejected (§16); the main ongoing risk is a future agent "helpfully" merging vertical resource tables under time pressure — flag this document in review when that work starts.
- **JSON configuration becoming a dumping ground:** avoided by construction — no JSON blob is introduced anywhere in this plan (§8).
- **Migrations per vertical:** expected and acceptable (§22) — small, additive, per-vertical tables are the intended shape, not a smell.
- **Tenant list showing incomplete businesses:** intentional for the *owner's own* list (§9's resume mechanism depends on it); would be a real bug if it ever affected another user's view, which the existing per-user `ListAccessibleByUserID` scoping already prevents.
- **Public slug exposing unfinished tenants:** the specific defect this plan closes (§19) — the one behavior change in this entire plan, called out repeatedly rather than buried.
- **Dashboard rendering before configuration exists:** F7's completion banner (§18) is the mitigation — no vertical dashboard module renders until its backend domain exists, because no nav-config row exists for it yet.
- **Role confusion between `BUSINESS_OWNER` and `SUPER_ADMIN`:** no new risk introduced — `business_type`/onboarding fields have no interaction with the role/permission system at all.
- **Future mobile app reuse:** the public-route branch-point design (§19) and the API-first contract (§21) both keep the vertical decision server-driven and response-field-based rather than embedded in client routing structure, which is the right shape for a future native client to reuse — not further designed here.
- **Stale marketing copy:** `BusinessTypes.tsx`'s six generic categories don't reflect the four real verticals — cosmetic, flagged for a follow-up, not blocking this architecture.

---

## 29. Acceptance Criteria for This Architecture Foundation

This plan (through F1–F8) is considered successfully realized when, without any vertical booking domain existing yet:

1. A new user can register, and land in onboarding automatically (already true today — confirmed unchanged).
2. Creating a tenant (from F6 onward) **requires** choosing one of the four canonical `business_type` values; the value is rejected if missing or outside that list. Tenants created before this requirement existed retain `business_type == NULL` permanently, by design, not as a defect (§8).
3. A tenant is `IN_PROGRESS` immediately after creation and `COMPLETED` only after an explicit completion call **that itself validates real completion prerequisites** — calling the completion endpoint alone, immediately after creation, with nothing else configured, must fail (§21, §24, the non-negotiable correction from this revision).
4. An `IN_PROGRESS` tenant's public slug returns `TENANT_NOT_FOUND`, indistinguishable from a nonexistent slug.
5. A `COMPLETED` tenant's public slug and existing profile/retrieval behavior are byte-for-byte unchanged from today, including for legacy tenants with `business_type == NULL`.
6. Logging in with one incomplete tenant resumes onboarding at the last saved step, from a different browser session, with no reliance on `localStorage` for anything but the last-selected tenant ID (which was already the only thing persisted client-side before this plan, and remains so) — and no onboarding state is ever read from anything user-scoped (§9).
7. Logging in with one completed tenant goes straight to `/dashboard`, unaffected by any other incomplete tenant the same user owns.
8. The dashboard shows the tenant's real name and business type (or a safe generic label when `business_type` is null), and a real completion banner **only** when incomplete — never for a completed tenant — no fabricated data anywhere.
9. `business_type` cannot be changed through any endpoint, at any onboarding stage, for the tenant's entire lifetime (§7's non-negotiable correction — stricter than this plan's first draft, which allowed changes while still `IN_PROGRESS`).
10. Every existing Feature 1–5 backend test and every existing frontend auth/tenant/permissions test still passes, **or** — where a test/fixture directly exercised `POST /tenants`'s request shape — has been mechanically updated to include `business_type`, consistent with that field's intentional transition to required (§21). No test covering unrelated behavior (slug validation, membership creation, atomicity, cross-tenant denial, etc.) should need any change.

---

## 30. Explicit Non-Changes

To be preserved exactly as-is by every feature in §26:

- `tenants.status` (`ACTIVE`/`DISABLED`) keeps its current lifecycle meaning — untouched by onboarding state.
- The Feature 2 transaction's atomicity (tenant + membership + `BUSINESS_OWNER` in one `BeginTx`/`Commit`) — extended with two more field writes, never restructured.
- Slug validation, reserved-word list, and uniqueness enforcement (Feature 5) — untouched.
- `UpdateProfile`'s field set — gains no `business_type` field, ever, through this endpoint. Restated from §7 because it is the one rule in this plan explicitly marked non-negotiable: `PATCH /api/v1/tenants/{id}` must never become a path to changing a tenant's vertical, at any onboarding stage, for the tenant's entire lifetime.
- The `identity`/`auth` domain — `model.User`, `Principal`, session/token structures — never gains an onboarding-related field of any kind (`onboarding_step`, `current_tenant`, or otherwise). Onboarding state is exclusively a property of `tenants` (§9); this is restated here as a boundary Epic 01's user/auth domain must stay outside of, not just a frontend routing convention.
- The auth token model (memory-only, Bearer, single-flight refresh) — untouched; onboarding resume works within the existing constraint that a hard reload requires re-login, same as everything else today.
- `PermissionsProvider`/`Can`/`useCan` remaining tenant-scoped-only and never reused for `/admin` — untouched, and not extended to cover onboarding-state gating either (that's a routing concern for `TenantGate`, not a capability check).
- `Authorizer`'s fail-closed behavior — every new endpoint reuses it unchanged, no new authorization primitive is introduced.
- The backend's own Epic 02 roadmap document (Features 6–12) — this plan does not implement, renumber, or supersede any of it; `onboarding_status`/`onboarding_step` are new concepts alongside that roadmap, not a replacement for its planned "Tenant Lifecycle Management" (Feature 8) or "Tenant Settings Foundation" (Feature 9).
- Epic 01 authentication/RBAC — no file under `internal/auth`, `internal/identity`, or `internal/authorization` is modified by any feature in §26.
