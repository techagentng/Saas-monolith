# Vertical Services, Bookable Resources & Availability — Implementation Plan

**Status: PLAN ONLY — repository-grounded. No production code, migrations, permissions, seeds, or frontend pages were written for this pass.**

Source baseline: `Vertical_Services_Bookable_Resources_Architecture.docx` (product intent). The repositories are technical truth; every conflict is surfaced in §3 rather than silently resolved.

Repositories inspected (by reading source, not by trusting the baseline's assumptions):

- Backend: `Saas-monolith` — Go 1.26.5 modular monolith. Inspected while the Vertical Onboarding F1–F3 backend work and the refresh-cookie auth change were still uncommitted on top of `d8160f8`; they were committed as `14000e9` during this pass, so the state described here is HEAD. `go build ./...`, `go vet ./...` and `go test ./...` all pass on it.
- Frontend: `Saas-frontend` — Next.js 16.3.2 / React 19 / TanStack Query v5, HEAD `8891450`, clean tree, through onboarding F4–F6.

### Relationship to the previous planning pass

`NAIL-TECHNICIAN-VERTICAL-IMPLEMENTATION-PLAN.md` (a plan document, never implemented) covered overlapping ground from a narrower baseline. **This document supersedes it.** The substantive reversals, all driven by the new baseline plus repository evidence, are listed in §38.6 — they are reversals, not oversights.

---

## 1. Executive Summary

Five decisions carry this plan. Each is argued in full below rather than asserted.

1. **`Service` is not a platform-wide abstraction.** It belongs to the *appointment scheduling booking model*, which Nail Technician is the first vertical to use. Restaurant, Hotel and Transport do not get squeezed into it, and no `offerings` base table is created. The four verticals differ in the **unit of consumption** — a duration on a person's calendar, a party's capacity inside a service window, a room-type's inventory across a date range, a seat on a fixed departure — and a shared table would need every one of those as a nullable column, which is exactly the shape the baseline forbids (§5).

2. **There is no shared `bookings` header table, and that is a technical requirement rather than a preference.** The race-safe booking guarantee (§15) is a PostgreSQL `EXCLUDE USING gist` constraint over `(staff_id, tstzrange(starts_at, ends_at))`. A constraint cannot span two tables. If the header owns the time range and a vertical detail table owns `staff_id`, the constraint becomes inexpressible and double-booking prevention falls back to application-level locking. The shared thing is the **column vocabulary and lifecycle semantics**, not the table (§17).

3. **A tenant member with the `STAFF` role is *not* a bookable technician.** `STAFF` today is a bare RBAC label with four read-only permissions; `users` has no `name` column at all; and a `BUSINESS_OWNER` who personally performs services must be bookable without being demoted. A separate tenant-scoped `staff_profiles` table with a **nullable `user_id`** is the clean boundary — nullable because non-login workers are the common case in a nail salon, and fabricating a user account (which requires a unique email *and* a password hash) to represent one is worse than a nullable FK (§10).

4. **Guest booking is allowed from day one, and booking creation is idempotent from day one.** Guest booking because the public journey has no authentication at all and requiring an account before a first booking is a conversion tax with no compensating benefit. Idempotency because the exclusion constraint actively *hurts* without it: a retried booking after a network timeout would be rejected as a slot conflict — the customer would be told their own successful booking's slot is taken (§16, §35).

5. **Nothing shared gets built before something needs it twice.** No `AvailabilityProvider` interface (one implementation), no event bus (no infrastructure exists), no cancellation-policy model, no add-ons, packages, templates, categories, or payment columns. The one genuinely shared new thing is the `customers` table, and even that lands with the booking feature that first needs it, not before.

The recommended next feature is **S1 — Nail Service Catalog (backend)**: one migration, one module, four permissions, five endpoints, no dependency on staff, availability, or booking.

---

## 2. Current Repository State

### Backend — verified absent

An exhaustive case-insensitive sweep across `internal/` for `technician|booking|appointment|reservation|availability|customer|price|currency|money|offering|inventory|capacity|vehicle|trip|room|schedule` returns **only** these hits, every one a false positive:

- `business_type.go` — the `NAIL_TECHNICIAN` enum value.
- `onboarding_service.go` — a doc comment saying "every future booking, availability window, and schedule is computed against [timezone]".
- `slug.go` — comments about slug *reservations* (reserved words).
- `public_tenant_handler.go` / `public_tenant_service.go` — comments about the "customer experience" a vertical router will pick.

There is **zero domain code** for services, staff, availability, customers, bookings, money, or any vertical resource. Go's own `internal/*/service` package-layer naming and the `STAFF` role string account for the rest of the raw grep noise.

### Backend — what exists and constrains the design

| Area | Actual state |
|---|---|
| Stack | Go 1.26.5, `net/http` `ServeMux` with Go 1.22 method+pattern routing, `database/sql` + `pgx/v5/stdlib`. **No ORM, no router library, no mocking library, no test framework beyond stdlib `testing`.** Direct deps: jwt/v5, uuid, pgx/v5, godotenv, x/crypto. |
| Module layout | `internal/<domain>/{model,repository,service,handler}` + cross-cutting `internal/{auth,authorization,config,database,errors,app}`. Domains are named for the *domain* (`tenant`, `identity`, `authorization`), never for a product vertical. |
| Migrations | `000001`–`000009`, plain numbered `.up.sql`/`.down.sql`. `database.ApplyMigrations` applies **each file inside a single transaction** and records it in `schema_migrations`. Consequence: `CREATE EXTENSION` is fine; `CREATE INDEX CONCURRENTLY` is not. Test DB is `postgres:16-alpine` (`docker-compose.test.yml`, port 5434, opt-in via `TEST_DATABASE_URL`). |
| `tenants` | `id, name, slug, status, description, contact_email, contact_phone, timezone, business_type, onboarding_status, onboarding_step, created_at, updated_at`. `status` CHECK is **`ACTIVE`/`DISABLED` only**. `timezone` is a nullable, `time.LoadLocation`-validated IANA string. **No `currency` column.** |
| `business_type` | Typed Go enum, four values, DB CHECK-constrained, **immutable** — `UpdateTenantProfileRequest` has no field for it and no endpoint writes it after creation. Nullable only for pre-F1 legacy rows. |
| Onboarding | `onboarding_status` (`IN_PROGRESS`/`COMPLETED`) is explicitly decoupled from `status`. `validateOnboardingCompletionPrerequisites` requires business type, name, canonical slug, a saved step, and a **valid IANA timezone**. |
| Public tenant identity | `GET /api/v1/public/tenants/{slug}` → `{slug, name, description, timezone, business_type}`. Gated on `status == ACTIVE && onboarding_status == COMPLETED`; reserved slugs, non-canonical slugs, disabled tenants and incomplete tenants all collapse to an identical `TENANT_NOT_FOUND`. No internal UUID is ever exposed. |
| `users` | `id, email, password_hash, status, created_at, updated_at`. **No `name`, no phone, no profile fields of any kind.** Email is uniquely constrained. |
| `tenant_memberships` | `id, tenant_id, user_id, status(ACTIVE/DISABLED)`, unique on `(tenant_id, user_id)`. No role, no profile, no job data. |
| RBAC | 13 seeded permissions, all `user.*` / `tenant.*` / `role.*` / `permission.*`. Verb granularity is **per-action** (`user.read`/`create`/`update`/`disable`), never `.manage`. Code pattern enforced: `^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`. |
| Roles | `SUPER_ADMIN` (PLATFORM, all 13), `BUSINESS_OWNER` (TENANT, 9), `STAFF` (TENANT, 4 read-only: `tenant.read`, `user.read`, `role.read`, `permission.read`). |
| **Permission resolution (load-bearing)** | `ResolveTenant` calls `ListByUserScope(userID, ScopeTenant, tenantID)` — it reads **only TENANT-scoped roles**. A `SUPER_ADMIN`'s PLATFORM role grants **nothing** inside a tenant. There is no platform-admin backdoor into tenant data today, by construction. |
| **Seed gotcha (load-bearing)** | `000006` grants `SUPER_ADMIN` every permission via `INSERT ... SELECT id FROM permissions` — a **one-time snapshot**. A permission added by a later migration is *not* automatically granted to `SUPER_ADMIN`. |
| Authorization | `Authorizer` is default-deny and never caches; `failClosed` maps unknown resolver errors to `SERVICE_UNAVAILABLE`, never to allow. `TenantPermissionMiddleware` requires both a trusted `Principal` and a trusted `TenantContext`. |
| Tenant context | `tenant.Middleware` → `TenantContextService.Resolve` re-derives tenant + ACTIVE membership from the DB on **every** request. A client-supplied `tenantID` is a lookup key, never a trust token. |
| Route chain | Every private tenant-scoped route is literally `authMiddleware.Wrap(tenantMiddleware.Wrap(TenantPermissionMiddleware{...}.Wrap(handler)))`. Wired by hand in `app.New()`; no route groups, no auto-registration. |
| Transactions | `TenantService.Create` is the only multi-write transaction: `db.BeginTx` → construct tx-scoped repositories via `NewPostgresXRepository(tx)` (the `dbtx` interface is satisfied by both `*sql.DB` and `*sql.Tx`) → `defer` rollback guarded by a `committed` bool → `Commit`. This is the pattern to copy. |
| Error infra | `AppError{Code, Message, Err}`; `Map()` refuses to leak unknown codes (falls back to `INTERNAL_ERROR`); adding a code is a two-map edit in `internal/errors/`. Per-entity 404 codes are the established convention (`USER_NOT_FOUND`, `TENANT_NOT_FOUND`, `ROLE_NOT_FOUND`). |
| PG error mapping precedent | `isSlugUniqueViolation` matches `pgconn.PgError` code `23505` **and** the specific `ConstraintName`; anything else is re-wrapped as a system failure. Exactly the pattern the booking-conflict mapping must copy. |
| Security convention | Handler decode-target field lists **are** the "client cannot set this" mechanism — `business_type`, `owner_id`, `onboarding_status` are all protected by simply having no field to decode into. |
| Tests | Table-driven, hand-written fakes (no mocking lib). `*_integration_test.go` skip unless `TEST_DATABASE_URL`/`DATABASE_URL` is set. Route-chain tests live in `internal/app`. **No concurrency test exists anywhere yet.** |
| Rate limiting | `CodeRateLimited` exists in the catalog but **nothing in the codebase emits it**. There is no rate limiter. |
| Eventing | No pub/sub, no queue, no worker goroutines, no outbox. Nothing to subscribe to. |

### Frontend — what exists and constrains the design

| Area | Actual state |
|---|---|
| Auth | `token-store.ts` memory-only by design; refresh is an **HttpOnly cookie** with single-flight `refreshAccessToken()` in `lib/api/client.ts` and `credentials: "include"`. |
| API client | `apiClient.{get,post,put,patch,delete}`, single-flight refresh-and-retry on 401, `ApiError{code,status,details}` normalized from the Go `{error:{code,message}}` envelope. Callers branch on `code`, never message text. |
| Module convention | `modules/<domain>/{api,keys,queries}.ts` triad — `api.ts` (raw fetch fns + input types), `keys.ts` (query-key factory), `queries.ts` (`"use client"` hooks with cache seeding + invalidation). Followed consistently by `tenant`, `permissions`, `onboarding`, `auth`. |
| Tenant state | `TenantProvider` — selection persisted as an **id only** in localStorage, never trusted; auto-selects on exactly one tenant. `TenantGate` is fully resume-aware, reading only `currentTenant.onboarding_status` (never user/session state). |
| Permissions | `PermissionsProvider` sources `GET /v1/tenants/{id}/permissions`, keyed per tenant, **fails closed to an empty `Set`**. `Can`/`useCan`/`can` documented as UX-only and TENANT-scoped only. `types/permission.ts` is a union of the 13 real codes widened with `(string & {})` so an unknown backend code still works. |
| Nav | `dashboardNavItems` has exactly **one** entry (`Dashboard`). `Sidebar` filters on a single predicate: `!item.permission || can(permissions, item.permission)`. `NavItem` has no `businessType` field. |
| Dashboard | `dashboard/page.tsx` renders "Welcome back, {email}" and nothing else. No fabricated metrics. F7 has **not** shipped. |
| Public routes | `(public)/` contains marketing, privacy, terms. **No `book/[slug]` route exists.** The vertical public router (onboarding-plan F8) has not shipped. |
| Onboarding | F4–F6 shipped: `OnboardingShell`, the `business_profile` step with **presentation-only substeps** (`about/contact/timezone/review`) that are never persisted. A directly reusable pattern. |
| Vertical plumbing | `businessTypeLabel()` exists (presentational only, falls back to "Business"). `types/tenant.ts` carries `business_type`/`onboarding_status`/`onboarding_step`, verified live. |
| **Testing (load-bearing gap)** | `package.json` has **no test framework at all** — no vitest, jest, testing-library, playwright, or `test` script. Frontend TDD currently has zero infrastructure. |

---

## 3. Baseline vs Repository Reality

| Baseline assumption | Repository reality | Decision |
|---|---|---|
| §2 "Do not force all verticals into one generic service/resource model" | No domain code exists at all — nothing to force, and nothing to reuse | **Agreed and adopted.** `Service` is scheduling-model-owned, not platform-generic (§5). |
| §5 "Likely Service Fields" includes `price` and `currency` on the service row | No money convention exists anywhere; the only `int64`s are token-expiry seconds | **Partially overridden.** `price_minor BIGINT` on the service; **`currency` moves to the tenant** (§8). Per-service currency invites a mixed-currency basket with no exchange logic to resolve it. |
| §10 "common booking lifecycle/header plus vertical-specific details" | The race-safety mechanism (§15) is a single-table `EXCLUDE` constraint, which cannot span a header/detail split | **Overridden, with the reason stated.** Shared *vocabulary*, per-vertical concrete tables (§17). This is the baseline's own "must be repository-grounded before implementation" clause being exercised. |
| §9 `AvailabilityService` with four vertical implementations | One vertical will exist; the codebase only defines interfaces where there are ≥2 implementations or a test fake | **Deferred.** No interface until a second implementation and a shared caller both exist (§28). |
| §12 "determine whether STAFF-role members are the same as service-performing staff" | `STAFF` = 4 read-only permissions, no profile fields. `users` has **no name column**. `BUSINESS_OWNER` must be bookable | **Answered: separate `staff_profiles`, `user_id` nullable** (§10). |
| §13 "decide whether customers must have accounts or can book as guests" | The public app has no auth surface at all; refresh is an HttpOnly cookie a React Native client wouldn't hold | **Guest booking, yes** (§16). |
| §18 module direction `internal/nail/ restaurant/ hotel/ transport/` | Every existing package is named for a *domain* (`tenant`, `identity`, `authorization`), never a product segment | **Overridden.** Modules named for the **booking model** — `internal/scheduling`, later `dining`/`lodging`/`transit` (§27). The appointment model generalizes to barber/spa/tattoo/consulting with no new module; `internal/nail` would not. |
| §16 Nail dashboard modules include "Customers" | No customer concept exists; it arrives only with booking | **Approved only from S10 onward.** No placeholder nav entries (§23) — the existing `dashboard-nav.ts` comment already forbids fabricated entries. |
| §3 "Cancellation policy" listed as a shared concept | No policy data model, no payment, no notification | **Deferred entirely.** §20 confirms the booking model does not preclude it. |
| §20 "determine whether new permissions are required, e.g. `service.read/create/update/disable`" | Existing granularity is per-action; `disable` specifically means "bar an actor from acting" | **Four permissions, one verb changed**: `service.read/create/update/**archive**` (§28) — argued, not silently renamed. |
| §21 delivery sequence step 1 = "shared architecture decisions: money, timezone, customer identity, booking status vocabulary" | Timezone is **already decided and shipped** (F6); the other three need no table until the feature that uses them | **Sequence honored, but step 1 produces no code.** It is this document (§8, §9, §16, §18). There is no "F1 — Shared Foundations" feature (§35). |
| §19 "define transaction boundaries, locking or conflict-detection strategy" | PostgreSQL 16; `btree_gist` available; migration runner is transaction-per-file | **Exclusion constraint, Read Committed, no application locking** (§15). |
| §23 "Business type determines product behavior, not authorization" | `business_type` is already immutable and never consulted by `Authorizer` | **No change.** Verified, not re-implemented. Vertical gating stays a UX/nav concern. |

**No conflict required silently changing established backend behavior.** The two places this plan changes existing artifacts are called out explicitly: adding `tenants.currency` (§8) and adding a second predicate to `NavItem` (§23).

---

## 4. Shared vs Vertical-Owned Concepts

### SHARED CORE

| Concept | Why it is shared |
|---|---|
| **Tenant** | Already built. The isolation boundary for every table in this plan. Unchanged. |
| **Timezone** | Already on `tenants`, already validated, already required for onboarding completion. Every vertical computes every time against it. One field, one meaning, zero divergence. |
| **Money representation** | Integer minor units + ISO-4217 code is correct for all four verticals identically. Shared as a **Go value type and a column convention**, never as a table (§8). |
| **Currency (per tenant)** | A business trades in one currency. This is true of a salon, a restaurant, a hotel and a bus company alike. |
| **Customer** | Genuinely identical across verticals: a person with a name and a way to reach them, scoped to one tenant. A hotel guest, a diner, a passenger and a salon client have the same fields and the same privacy rules. **One shared table** (§16). |
| **Booking status vocabulary** | `CONFIRMED / CANCELLED / COMPLETED / NO_SHOW` mean the same thing in all four verticals, and terminality is a universal rule (§18). Shared as a **vocabulary and transition rule**, not a table. |
| **Audit timestamps** | `created_at`/`updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP` — already the convention on all nine tables. |
| **Public visibility gate** | `status == ACTIVE && onboarding_status == COMPLETED`, already shipped in `PublicTenantService`. Every vertical's public surface reuses it rather than re-deriving it (§30). |
| **Error contract** | `AppError` + code maps. Shared, extended per feature (§29). |

### VERTICAL-OWNED

| Concept | Owner | Why it cannot be shared |
|---|---|---|
| **Service** | scheduling (Nail) | A "service" is a *named duration performed by a person*. A hotel's sellable unit is a room type with nightly inventory; a restaurant's is a party-size slot against floor capacity; a transport company's is a seat on a fixed departure. Only `name/description/price/status` overlap — four cheap columns, not an abstraction. |
| **Staff / technician** | scheduling | A restaurant has staff but does not book *against* them; a hotel books against rooms; a bus books against vehicles. "The resource whose calendar is consumed" differs structurally. |
| **Staff–service capability** | scheduling | Presupposes both preceding concepts. |
| **Working hours / recurring schedule** | scheduling | Nail = weekly recurring wall-clock windows per person. Transport = a set of concrete dated departures, not a recurrence over people. Hotel = no schedule at all (inventory per date). Restaurant = tenant-level opening hours + turn times. Four different shapes. |
| **Exceptions / time off** | scheduling | Same reasoning — a subtractive interval against a recurring schedule that only this model has. |
| **Availability** | scheduling (computation); **shared as a question only** | "Is this available?" is a shared *question*; the arithmetic differs completely (interval intersection vs capacity counting vs date-range inventory vs seat count). §28 explains why this does not yet justify an interface. |
| **Price location** | vertical | Nail: on the service. Hotel: on a rate, per room type per date. Transport: on a fare, per route/class. Restaurant: often no price at all. The *representation* is shared; *where it hangs* is not. |
| **Capacity** | dining / transit | Meaningless for a nail service. A nail appointment's "capacity" is always exactly one person's time. |
| **Inventory** | lodging / transit | Meaningless for scheduling — availability is derived from a calendar, not decremented from a stock count. |
| **Resource** | **rejected as a shared concept** | The word covers a technician, a table, a room and a vehicle; those share nothing but the noun. A generic `resources` table would be the nullable-column trap the baseline forbids (§2 of the baseline). |
| **Booking table** | vertical | See §17. The lifecycle is shared; the table is not. |
| **Cancellation policy** | deferred everywhere | No model in any vertical yet (§20). |

---

## 5. Generic Service/Offering Decision

**Recommendation: option A, generalized exactly one notch — `Service` belongs to the *appointment scheduling booking model*, not to "Nail" specifically and not to the platform generally.**

Rejected alternatives, with reasons:

- **B — a generic tenant catalog (`services` used by every vertical).** Requires `duration_minutes` (meaningless for a hotel room type), `capacity` (meaningless for a nail service), `date_range` semantics (meaningless for both), and `seat_class` (meaningless for three of four). Every one of them nullable. This is precisely the "giant nullable table" the baseline names as the failure mode.
- **C — an `offerings` base table with vertical child tables.** The base would carry `id, tenant_id, name, description, price_minor, status` — six columns, five of them trivially cheap to repeat. In exchange every read becomes a join, every write becomes a two-table transaction, and the FK integrity property that every existing table in this codebase enjoys (concrete FKs, CHECK constraints, no polymorphism anywhere) is broken by a base row whose child type is only knowable by querying. The abstraction costs more than it saves at n=1 and does not obviously pay off at n=4, because the child tables would carry ~90% of each vertical's real columns anyway.
- **A taken literally — `internal/nail`.** Wrong axis. Barber shops, spas, massage therapists, tattoo studios, tutors and consultants all book *a named service of a known duration with a specific practitioner*. That is one booking model with many market segments. Naming the module `nail` would force a second, near-identical module the first time a barber signs up — and it breaks the codebase's own convention of naming packages for domains rather than products.

**What "one notch" means concretely:** the module is `internal/scheduling`, its aggregate root concepts are `Service`, `StaffProfile`, `WorkingHours`, `ScheduleBlock`, `Appointment`, and `NAIL_TECHNICIAN` is simply the `business_type` that switches this module's UI on. Adding `BARBER` later is an allow-list change plus a nav predicate — no new backend module. Adding `HOTEL` is a new module, correctly.

---

## 6. Nail Technician Domain Model

```
Tenant (exists — id, slug, timezone, business_type, currency*)
  │
  ├── Service            (scheduling)  name, duration_minutes, price_minor, status
  │
  ├── StaffProfile       (scheduling)  display_name, user_id?, is_bookable, status
  │      │
  │      ├── staff_services      → many-to-many with Service (capability)
  │      ├── staff_working_hours → weekday + [start_time, end_time) local wall clock
  │      └── schedule_blocks     → [starts_at, ends_at) instants, subtractive
  │
  ├── Customer           (shared, tenant-scoped)  name, email?, phone?, user_id?
  │
  └── Appointment        (scheduling)  ← the booked entity
         service_id + snapshot(name, duration, price, currency)
         staff_id   + snapshot(display_name)
         customer_id
         starts_at, ends_at  (TIMESTAMPTZ)
         status, idempotency_key
```

`*` = the one addition to an existing table (§8).

**Ownership boundaries:**

- `internal/scheduling` owns everything above except `Customer` and `Tenant`.
- `internal/customer` owns `Customer` and nothing else.
- `internal/tenant` is **not modified** except for the `currency` column and its accessor.
- Availability is a **derived value**, owned by `scheduling`, persisted nowhere. There is no `available_slots` table — materializing slots would need invalidation on every hours edit, every block, and every booking, and would be wrong the moment it lagged.

**The one cross-module FK:** `appointments.customer_id → customers.id`. Both are tenant-scoped and the composite-FK guard in §11 applies to it too.

---

## 7. Service Catalog Model

Proposed `services` columns, each judged independently rather than accepted from the baseline's list:

| Field | Verdict | Reasoning |
|---|---|---|
| `id UUID PK` | **Include** | Codebase convention; `uuid.NewString()` at the service layer, never DB-generated. |
| `tenant_id UUID NOT NULL REFERENCES tenants(id)` | **Include** | The isolation boundary. Non-negotiable. |
| `name VARCHAR(255) NOT NULL` | **Include** | Matches `tenants.name`'s length and its service-layer trim+length validation. |
| `description TEXT` | **Include, nullable** | Mirrors `tenants.description`: empty string is a legitimate value, 1000-char cap at the service layer. |
| `duration_minutes INT NOT NULL` | **Include** | Availability is impossible without it, and a later non-nullable backfill would be painful. `CHECK (duration_minutes > 0 AND duration_minutes <= 480)`. |
| `price_minor BIGINT NOT NULL` | **Include** | See §8. `CHECK (price_minor >= 0 AND price_minor <= 100000000)`. |
| `currency` | **Reject — lives on the tenant** | §8. |
| `status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE'` | **Include** | `CHECK (status IN ('ACTIVE','ARCHIVED'))`. See the vocabulary note below. |
| `created_at`, `updated_at TIMESTAMPTZ` | **Include** | Convention on all nine existing tables. |
| `category_id` | **Reject for v1** | The new baseline dropped categories entirely. A category with no starter-template system behind it is a second table, a second CRUD surface and a second nav concern to make a list heading. Additive later (`ALTER TABLE ... ADD COLUMN category_id UUID NULL`). *This reverses the previous plan, which made it required — see §38.6.* |
| `display_order INT` | **Reject for v1** | Order by `name ASC, id ASC` — the same deterministic-ordering discipline `ListAccessibleByUserID` already uses. Additive later with `DEFAULT 0` and no backfill risk. |
| `buffer_before/after_minutes` | **Reject for v1** | §14 explains the forward path, which does not require these columns to exist now. |
| `online_bookable BOOLEAN` | **Reject for v1** | "Active but not self-bookable" is not a v1 requirement and the baseline does not ask for it. `status` alone is the gate. Additive later with `DEFAULT true`. |
| `deposit_*` | **Reject** | No payment architecture (§32). |

**Answers to the specific §6 questions:**

- **Soft delete vs active/inactive.** Archive, never hard delete. Appointments hold a real FK to `services`; a `DELETE` would either fail or orphan history. `status = 'ARCHIVED'` is the only delete.
- **Status vocabulary.** `ACTIVE`/`ARCHIVED`, deliberately *not* the existing `ACTIVE`/`DISABLED`. In this codebase `DISABLED` consistently means "an actor is barred from acting" (a user, a tenant, a membership). A service is not an actor; it is a catalog entry that stops being offered. Overloading `DISABLED` would blur a word that currently has a precise access-control meaning. This is one new vocabulary word, argued rather than assumed.
- **May duration be zero?** No. `CHECK > 0`. A zero-duration service generates degenerate slots and an empty `tstzrange`, which the exclusion constraint would then never conflict on — a correctness hazard, not just a modelling nicety.
- **Maximum duration?** Yes, 480 minutes. It bounds slot-generation cost and catches the classic unit error (someone entering seconds). Trivially raised later.
- **May price be zero?** Yes. Free consultations and patch tests are real nail-salon services. Negative is rejected.
- **Currency ownership.** Tenant (§8).
- **Archived service behavior.** Excluded from the public catalog, excluded from availability, rejected as a booking target with a `SLOT_UNAVAILABLE`-class error; still readable in the owner catalog under an explicit filter; existing appointments unaffected.
- **Do old bookings retain historical service information?** Yes — snapshot **and** FK, both (§19).
- **Unique service name per tenant?** No constraint. Two "Gel Manicure" rows (one archived) are legitimate, and a partial unique index over active names would block re-creating a name the owner had archived. Not worth the constraint.

---

## 8. Money/Currency Decision

**Storage: integer minor units in `BIGINT`, plus a separate ISO-4217 currency code. No floating point, no `NUMERIC`.**

- **Why not float:** non-negotiable, and the baseline agrees.
- **Why not `NUMERIC(12,2)`:** correct arithmetic, but Go's `database/sql` has no native decimal type — it round-trips through `string` or a third-party decimal package, and the project has exactly five direct dependencies and no appetite for a sixth. `int64` minor units are exact, comparable, sortable, and sum without loss for every realistic total.
- **Why a value object:** a bare `int64` is indistinguishable from the token-expiry-seconds `int64`s already in this codebase. A minimal `internal/money` package with `type Amount struct { Minor int64; Currency string }`, a validating constructor, `Add`, and formatting — **no multiplication, no division, no rounding, no FX** — makes the unit un-mistakable at every service boundary while storing as two plain columns. Cross-cutting non-domain packages (`internal/errors`, `internal/auth`, `internal/config`) are already an established shape here.

**Currency placement: on the tenant.**

| Candidate | Verdict |
|---|---|
| Per service | **Rejected.** Invites a basket mixing GBP and NGN with no exchange logic to resolve it, and no salon prices one service in a different currency from another. |
| Per booking only | **Rejected as the source.** A booking must *snapshot* the currency, but it needs somewhere authoritative to read it from at creation time. |
| **Per tenant (+ snapshot on booking)** | **Recommended.** One place to validate, one place to display, and the snapshot keeps historical rows correct even if the tenant's currency is later changed. |

**Concrete shape:** `tenants.currency CHAR(3) NULL`, added in S1's migration, validated against an explicit ISO-4217 allow-list at the service layer (start small — the currencies the business actually serves — and reject anything else, matching the reject-over-normalize philosophy of `ValidateSlug`/`ValidateBusinessType`).

**Write-once semantics.** Currency is settable while `NULL` and immutable once set, enforced at the service layer. Changing it after priced services and historical bookings exist would silently reinterpret every stored amount. Because bookings snapshot the currency, history stays correct regardless — but the live catalog would not, so the immutability rule stands.

**How it gets set.** S1 requires a tenant currency before the first priced service can be created. Two options, and the recommendation is the second:

1. Add `currency` to `UpdateTenantProfileRequest` — but that request's doc comment explicitly limits it to profile data, and a write-once field with different mutability rules does not belong in a partial-update endpoint.
2. **Accept `currency` in the service-catalog creation flow**: `POST /tenants/{id}/services` requires the tenant to have a currency; a dedicated `PUT /api/v1/tenants/{tenantID}/currency` (idempotent, write-once, `tenant.update`) sets it. Small, explicit, and keeps the profile endpoint's documented contract intact.

**Deliberately NOT done in this pass:** currency is **not** added to `validateOnboardingCompletionPrerequisites`. That would change shipped F2/F6 behavior and would block tenants who have completed onboarding under the current rules. Currency becomes required at the moment a price is first entered, which is where it actually matters.

**Multi-currency future:** a per-service `currency` override column plus an FX table. Additive, not required now, and not designed here.

---

## 9. Timezone/Time Decision

`tenants.timezone` is already a validated IANA identifier and already required for onboarding completion. This section defines what everything downstream does with it.

**Canonical storage — three distinct types, chosen per meaning:**

| Data | Type | Why |
|---|---|---|
| Appointment `starts_at` / `ends_at`, schedule blocks | `TIMESTAMPTZ` | These are **instants**. Already the convention on every timestamp column in the schema. An instant is unambiguous under DST; a naive local timestamp is not. |
| Recurring working hours | `SMALLINT weekday` (0–6, ISO: 0=Monday) + `TIME start_time, end_time` (no zone) | These are **wall-clock rules**, not instants. "Tuesdays 09:00–17:00" must stay 09:00 across a DST transition — storing it as an instant would silently shift it by an hour twice a year. |
| Date-only concepts (future closures, hotel stays) | `DATE` | Not needed in v1; recorded so the convention is not invented ad hoc later. |

**There is no "local timestamp" column anywhere.** The ambiguous middle case the baseline warns about is structurally excluded by the table above.

**Conversion boundaries:**

- **Working hours → instants:** the availability engine loads `time.LoadLocation(tenant.Timezone)` once per query and constructs each candidate slot with `time.Date(y, m, d, hh, mm, 0, 0, loc)`. This is the *only* place wall-clock rules become instants.
- **Booking input:** the client submits `starts_at` as **RFC3339 with an explicit offset or `Z`** — never a naive local string. `ends_at` is derived server-side from the service's persisted duration and is never accepted from the client (§34).
- **Customer display:** the public booking page renders slots in the **tenant's** timezone with the zone visibly labelled. A customer in another zone must see the salon's local time; showing them their own device time for a physical appointment is a real-world failure, not a formatting preference.
- **Owner display:** tenant timezone throughout.
- **Database:** `TIMESTAMPTZ` normalizes to UTC internally; the session timezone is never relied upon.

**DST, explicitly:**

- Because working hours are wall-clock rules, the *rule* is DST-immune by construction. Only materialization is affected.
- **Spring forward (nonexistent local times).** If a shift starts at 01:30 and 01:00–02:00 does not exist that day, Go's `time.Date` normalizes forward rather than erroring. That is acceptable behavior but must be a *tested, documented* decision, not an accident.
- **Fall back (ambiguous local times).** Go's `time.Date` resolves to the **first** occurrence. Consequence: the repeated hour is offered once, not twice. Correct for a salon (they do not work a 25-hour Sunday) and must be asserted by test.
- **Test zones:** `Africa/Lagos` — the likely first customer — has **no DST at all**. The DST tests must therefore deliberately use `Europe/London` or `America/New_York`, or the entire class of bug ships untested.

---

## 10. Staff/User/Membership Decision

**Recommendation: a separate tenant-scoped `staff_profiles` table with a nullable `user_id`. A `STAFF`-role membership is *not* a bookable technician, and being bookable is *not* a role.**

Each scenario the request names, checked against the repository:

| Scenario | Why membership+role alone fails |
|---|---|
| An owner who also performs services | `BUSINESS_OWNER` and `STAFF` are separate roles; making bookability a role would force the owner to hold `STAFF` and inherit its read-only permission set, or force a dual-role hack. Bookability must be orthogonal to authority. |
| Non-login workers | A technician who never signs in has no `users` row — and creating one requires a **unique email and a password hash**, both of which would have to be fabricated. A nullable `user_id` is far cleaner than synthetic accounts. |
| Staff with multiple roles | `user_roles` already supports multiple tenant roles per user. Nothing about bookability should interact with that. |
| Display name, photo, bio | `users` has **no name column at all** — confirmed. There is nowhere to put this today, in any existing table. |
| Service assignment, working hours | Neither `users` nor `tenant_memberships` has anywhere to hang them; both would need new columns of a shape that has nothing to do with identity or access. |
| Disabled membership | A revoked membership must stop someone *signing in to the workspace* without deleting their appointment history or their name from past bookings. Separate lifecycles, separate rows. |
| Staff who leave | Archive the profile; the FK stays intact and the appointment snapshot preserves the display name shown to the customer at the time. |

**Shape:**

```
staff_profiles
  id UUID PK
  tenant_id UUID NOT NULL REFERENCES tenants(id)
  user_id UUID NULL REFERENCES users(id)      -- NULL = non-login worker
  display_name VARCHAR(255) NOT NULL
  bio TEXT NULL
  is_bookable BOOLEAN NOT NULL DEFAULT true
  status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE'  CHECK (status IN ('ACTIVE','ARCHIVED'))
  created_at, updated_at TIMESTAMPTZ
  UNIQUE (id, tenant_id)                                        -- enables composite FKs, §11
  CREATE UNIQUE INDEX ... ON staff_profiles (tenant_id, user_id) WHERE user_id IS NOT NULL
```

The partial unique index is not invented here — `user_roles` already uses exactly this pattern twice (`user_roles_platform_unique` / `user_roles_tenant_unique`) to express "unique when this nullable column is set".

**`is_bookable` vs `status`:** separate on purpose. A receptionist is an `ACTIVE` staff profile who is not bookable; a departed technician is `ARCHIVED`. Collapsing them would make "temporarily not taking appointments" indistinguishable from "no longer works here".

**Linking a profile to a user is optional and later.** S3 creates profiles with no `user_id`; associating one with a real login (so a technician can see their own calendar) is a later feature that needs no schema change.

**Explicitly not built:** no employment data, no pay rates, no HR fields, no avatar upload (no file storage exists in this codebase).

---

## 11. Staff-Service Assignment

```
staff_services
  staff_id   UUID NOT NULL
  service_id UUID NOT NULL
  tenant_id  UUID NOT NULL
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
  PRIMARY KEY (staff_id, service_id)
  FOREIGN KEY (staff_id,   tenant_id) REFERENCES staff_profiles(id, tenant_id)
  FOREIGN KEY (service_id, tenant_id) REFERENCES services(id, tenant_id)
```

**The composite foreign keys are the point.** With `UNIQUE (id, tenant_id)` on both parents, the database itself makes it *impossible* to link Tenant A's technician to Tenant B's service. Today every cross-tenant guarantee in this codebase is enforced in Go (`TenantContextService`, `Authorizer`); this is the first place the schema can enforce one directly, and it should. **Flagged as a new convention** — it is a deliberate addition, not an existing pattern, and it should be applied to `appointments` too.

**Activation/deactivation: row presence, no status column.** A capability is a fact, not a lifecycle. Unassigning a service deletes the join row; historical appointments are unaffected because they snapshot the staff display name and hold their own FKs. A `status` column here would need its own transitions, its own filter in every eligibility query, and would answer no question that row presence does not.

**Per-staff price/duration overrides: rejected for now.** No evidence of the requirement in the baseline or the repository, and it complicates the two most correctness-critical paths in the system (availability arithmetic and price snapshotting). The forward path is purely additive — `price_minor_override BIGINT NULL, duration_minutes_override INT NULL` on this join row, with the availability engine and the booking snapshot preferring the override when set. No restructuring, so deferring costs nothing.

---

## 12. Working Hours

```
staff_working_hours
  id UUID PK
  tenant_id UUID NOT NULL
  staff_id  UUID NOT NULL
  weekday   SMALLINT NOT NULL  CHECK (weekday BETWEEN 0 AND 6)   -- 0 = Monday
  start_time TIME NOT NULL
  end_time   TIME NOT NULL
  created_at, updated_at TIMESTAMPTZ
  CHECK (end_time > start_time)
  FOREIGN KEY (staff_id, tenant_id) REFERENCES staff_profiles(id, tenant_id)
```

**Owner: staff, not tenant, not service.**

- **Not the service.** A service has a duration, not a schedule. "Gel manicures only on Tuesdays" is a real but rare rule and belongs to a later per-service availability window, not to the base model.
- **Not the tenant, for v1.** A solo nail technician *is* the business — tenant hours would be a duplicate source of truth for the single most common tenant shape. Shop-wide closures, the real need behind tenant hours, are fully covered by tenant-scoped exception rows (§13). **Revisit when** a multi-technician tenant needs a shop envelope that overrides individual hours; the engine already intersects sets, so adding a tenant layer is one more intersection and no restructuring.
- The engine's composition (§14) is written as an intersection from the start precisely so this stays true.

**Multiple shifts per day: multiple rows per `(staff_id, weekday)`.** This is the decision that matters most in this section, because it gets lunch breaks for free — 09:00–13:00 and 14:00–18:00 is two rows with a gap. A single row with `break_start`/`break_end` columns is explicitly **rejected**: it hard-codes exactly one break per day, cannot express two, and duplicates in nullable form what row multiplicity expresses natively.

**Overnight shifts: forbidden in v1** via `CHECK (end_time > start_time)`. A nail salon does not operate past midnight, and allowing `end_time <= start_time` to implicitly mean "crosses midnight" creates a row whose meaning cannot be read without knowing the convention. The forward path (an explicit `crosses_midnight BOOLEAN`, or splitting at 00:00 in the engine) is additive. Rejecting it with a constraint is honest; allowing it ambiguously is not.

**Closed days: absence of rows.** No `is_closed` flag — a flag would create two representations of the same fact.

**No overlap constraint between rows** for the same staff and weekday. Overlapping shifts are merged by the engine (it unions intervals before intersecting), so an overlap is harmless rather than corrupting. A DB constraint here would need `EXCLUDE` over a `timerange` per weekday and buys nothing.

---

## 13. Exceptions / Time Off

```
schedule_blocks
  id UUID PK
  tenant_id UUID NOT NULL REFERENCES tenants(id)
  staff_id  UUID NULL           -- NULL = applies to the whole tenant
  starts_at TIMESTAMPTZ NOT NULL
  ends_at   TIMESTAMPTZ NOT NULL
  reason VARCHAR(255) NULL
  created_at, updated_at TIMESTAMPTZ
  CHECK (ends_at > starts_at)
  FOREIGN KEY (staff_id, tenant_id) REFERENCES staff_profiles(id, tenant_id)
```

**One table with a nullable `staff_id`, not two tables.** `NULL` means "the whole tenant". This is not invented polymorphism — it is **the exact pattern `user_roles.tenant_id` already uses** in this codebase, where `NULL` means platform scope and non-null means tenant scope, with partial unique indexes enforcing each. One nullable column with a single crisp meaning is a different thing from the many-nullable-FK table the baseline forbids.

**Separate from working hours, always.** A recurring rule and a dated exception are different kinds of statement. Overloading working-hours rows with exception semantics (a `date` column that is usually null, an `is_exception` flag) is exactly the mistake this separation avoids.

**Instants, not dates.** A half-day off is the common case; a date-granular table could not express it. A full-day closure is a block from local midnight to local midnight, constructed in the tenant's zone.

**Covers all five requested cases:** vacation (a staff block spanning days), one-day closure (a tenant block, `staff_id IS NULL`), staff time off (a staff block), public holiday (a tenant block — **no holiday calendar table, no country data**; a holiday is not a platform concept and importing one would be a product decision nobody has made).

**Special openings — additive exceptions — are deferred.** v1 blocks are **subtractive by definition**, which is why the table carries no `kind` column: a single-valued enum is dead weight. Adding additive exceptions later means `ALTER TABLE ... ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'BLOCK'` — additive, defaulted, no backfill risk.

---

## 14. Availability Architecture

**Where it lives: a dedicated service inside `internal/scheduling`, computing over data the repositories fetch. Not in SQL, not in the booking service.**

- **Why not repository SQL.** The computation is interval set-arithmetic with timezone and DST semantics. Go's `time` package gets zone conversion right; a `generate_series` implementation would need the same logic expressed in SQL, would be extremely hard to reason about across a DST boundary, and — decisively — would be **untestable without Docker**, whereas the whole point of this design is that the hard part is unit-testable in milliseconds.
- **Why not the booking service.** Availability is read by the anonymous public catalog path where no booking exists. Coupling them would drag booking-write dependencies into a read-only public endpoint.
- **Why a dedicated service rather than a method on the service catalog.** It consumes five different repositories; it is the natural seam where a second vertical would eventually diverge (§28).

**The testability decision — the most important structural choice here:**

```
// Repositories fetch. This function does the thinking, and touches nothing else.
func ComputeSlots(in SlotInput) []Slot
type SlotInput struct {
    Location      *time.Location
    Date          time.Time         // a local calendar date
    DurationMin   int
    Grid          time.Duration     // 15 minutes
    NotBefore     time.Time         // "now", injected — never time.Now() inside
    WorkingHours  []LocalInterval   // wall-clock, this weekday
    Blocks        []Interval        // instants
    Busy          []Interval        // existing appointments
}
```

`ComputeSlots` is a **package-level pure function over plain structs**: no database, no clock, no fakes, no interfaces. Every DST case, boundary case and overlap case becomes a table-driven unit test with zero setup. The surrounding `AvailabilityService` is then thin — load, convert, call, return — and needs only integration coverage for the loading.

**Composition:**

```
slots(service, staff, date) =
      grid_starts(date, 15min)
    ∩ fits_entirely_within( ∪ working_hours(staff, weekday) )
    − overlaps( blocks(staff) ∪ blocks(tenant) )
    − overlaps( appointments(staff) where status <> 'CANCELLED' )
    − starts_before(now)
```

For **"any available technician"**, the same computation runs per eligible staff member and the results are unioned; the response carries slot start times only. Which technician is assigned is settled **inside the booking transaction** (§15), not by the availability read — otherwise the assignment is stale the moment it is returned.

### Slot generation specifics

- **Grid: fixed 15 minutes, not configurable in v1.** It divides evenly into the common nail-service durations (30/45/60/90) and is the industry norm. A tenant-configurable grid is a column and a validation rule away, later.
- **Duration:** a slot is offered only if `[start, start+duration)` fits **entirely** inside one contiguous working interval. This is why overlapping working-hours rows are unioned before intersecting — otherwise a service spanning a lunch break would be wrongly offered or wrongly refused.
- **Current-time cutoff:** `NotBefore` is injected, never read from `time.Now()` inside the function. Slots starting in the past are never offered.
- **Minimum lead time:** **not in v1.** The forward path is a tenant-level column feeding `NotBefore = now + lead`; nothing about the model changes. Flagged in §37 as the most likely early product request.
- **Booking horizon:** a **hard server-side cap of 90 days** and a **maximum queried range of 31 days per request**, both enforced on the public endpoint. This is a security control as much as a product one — an uncapped availability endpoint is an unauthenticated, unbounded compute primitive, and there is no rate limiter in this codebase (§37).
- **Overlapping services:** structurally impossible per technician — the exclusion constraint forbids it. Across technicians, concurrent appointments are correct and expected.
- **Boundary semantics:** half-open `[start, end)` everywhere, matching the `'[)'` bound argument used by the exclusion constraint. A 10:00–11:00 appointment and an 11:00–12:00 appointment do not conflict. Stating this once, consistently, prevents the classic off-by-one-minute bug.

---

## 15. Double-Booking Strategy

**Recommendation: a PostgreSQL `EXCLUDE USING gist` constraint on the `appointments` table. Read Committed isolation. No advisory locks, no `SELECT ... FOR UPDATE`, no `SERIALIZABLE`.**

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;   -- needed for '=' on uuid inside gist

ALTER TABLE appointments ADD CONSTRAINT appointments_no_overlap
  EXCLUDE USING gist (
      staff_id  WITH =,
      tenant_id WITH =,
      tstzrange(starts_at, ends_at, '[)') WITH &&
  ) WHERE (status <> 'CANCELLED');
```

**Why this and not the alternatives:**

| Approach | Verdict |
|---|---|
| Re-check availability inside the transaction | **Necessary but insufficient**, exactly as the baseline says. Under Read Committed two transactions both read "free" and both insert. This must still be done — for eligibility, hours, lead time and archived-service checks — but it is not the conflict mechanism. |
| `SERIALIZABLE` | Works, but escalates every booking to a serialization-failure retry loop, and would be the first place in this codebase to use a non-default isolation level. Heavier and less precise than a constraint that states the invariant directly. |
| `SELECT ... FOR UPDATE` on the staff row | Serializes **all** bookings for a technician, including non-overlapping ones, and only works if every writer remembers to take the lock. A forgotten lock is an invisible bug; a constraint cannot be forgotten. |
| Advisory locks | Same "must remember" weakness, plus a hand-rolled key space, plus no protection against direct SQL. |
| **Exclusion constraint** | **Recommended.** The invariant lives in the schema, so it holds for every writer — including a future admin tool, a migration, or a manual `psql` session. It is exact (interval overlap, not row-level), needs no retry loop, and costs one GiST index. |

**Why the `WHERE (status <> 'CANCELLED')` predicate rather than listing occupying statuses:** it is automatically correct when `PENDING` is added later (§18), and it keeps `COMPLETED`/`NO_SHOW` occupying their historical time — which is right, because back-dating a conflicting appointment into a slot someone actually attended should fail.

**`tenant_id WITH =` is redundant** (a staff member belongs to one tenant) and included anyway: it makes the constraint self-documenting and holds even if a future bug were to move a staff row.

**Transaction shape for booking creation**, following `TenantService.Create` exactly:

1. Validate input, resolve the service (must be `ACTIVE`), derive `ends_at` from persisted duration. **Before `BeginTx`** — a malformed request never opens a transaction, mirroring the slug-validation placement in `TenantService.Create`.
2. `BeginTx(ctx, nil)` — default isolation. `defer` rollback guarded by `committed`.
3. Construct tx-scoped repositories via `NewPostgresXRepository(tx)`.
4. Match-or-create the customer.
5. Re-check inside the transaction: staff eligible for the service, staff bookable and `ACTIVE`, slot inside working hours, not inside a block, not in the past, within horizon.
6. `INSERT` the appointment. **The constraint decides the race.**
7. Commit.

**Error mapping**, copying `isSlugUniqueViolation` precisely:

```go
func isAppointmentOverlap(err error) bool {
    var pgErr *pgconn.PgError
    return errors.As(err, &pgErr) &&
        pgErr.Code == "23P01" &&                        // exclusion_violation
        pgErr.ConstraintName == "appointments_no_overlap"
}
```

→ `SLOT_UNAVAILABLE` (409). Any other constraint failure falls through to the existing system-failure wrapping rather than being guessed at — the same discipline the slug mapping already applies.

**"Any available technician":** the service picks a candidate, attempts the insert, and on `23P01` retries with the next candidate. Bounded by the eligible-staff count, so it terminates; exhausting the list yields `SLOT_UNAVAILABLE`. No locking, no coordination.

**Verify before S10:** confirm `btree_gist` is available on the production Postgres, not just `postgres:16-alpine`. It is a standard contrib module and is supported by every major managed provider, but it is an extension and must be checked rather than assumed.

---

## 16. Customer Domain

**Recommendation: a shared, tenant-scoped `customers` table, separate from `users`. Guest booking permitted from day one.**

```
customers
  id UUID PK
  tenant_id UUID NOT NULL REFERENCES tenants(id)
  user_id UUID NULL REFERENCES users(id)     -- future account link
  name VARCHAR(255) NOT NULL
  email VARCHAR(320) NULL
  phone VARCHAR(20) NULL
  created_at, updated_at TIMESTAMPTZ
  CHECK (email IS NOT NULL OR phone IS NOT NULL)
  UNIQUE (id, tenant_id)                                              -- composite-FK guard
  CREATE UNIQUE INDEX ... ON customers (tenant_id, lower(email)) WHERE email IS NOT NULL
  CREATE UNIQUE INDEX ... ON customers (tenant_id, user_id)     WHERE user_id IS NOT NULL
```

Point by point:

**A. Guest booking without an account — YES, and it is the v1 default.** The public app has no authentication surface at all; the customer journey is entirely anonymous today. Requiring registration before a first booking adds a signup funnel in front of the primary conversion event for no security benefit — the booking itself carries no privileged data. Just as importantly, it keeps the React Native path open (§39): the auth stack depends on an HttpOnly cookie a native client would not naturally hold, and an anonymous booking flow sidesteps that entirely.

**B. Registered customer accounts — deferred, with the hook present.** `user_id` is nullable now so linking needs no migration later.

**C. Linking guest history to a future account.** Deferred, and deliberately **not** designed as an automatic email match. Auto-linking every guest booking that shares an email address with a new registration is an account-takeover-shaped feature (register with a known email, inherit that person's booking history). When it ships it must involve a verification step. Recording that now prevents the "obvious" unsafe implementation later.

**D. Duplicate records.** Match-or-create on `lower(email)` **within the tenant**, at booking time, enforced by the partial unique index. Explicitly **no** fuzzy matching, no phone-based dedup, no cross-tenant identity resolution in v1. Known and accepted limitation: two people sharing an email within one tenant merge into one customer record — acceptable, reversible, and far better than the false merges fuzzy matching produces.

**E. Tenant-scoped, always.** A customer of Salon A is not a customer of Salon B. Any cross-tenant customer graph would be a platform-level data-sharing decision nobody has made, and would violate the isolation invariant every other table upholds.

**F. Email/phone identity.** Neither is individually required; at least one is, enforced by CHECK. A phone-only walk-in customer entered by the owner is a real case; so is an email-only online booking.

**G. Privacy.** Customer PII appears on **no** public endpoint, ever — including the booking-confirmation response, which echoes only what the caller just submitted. There is no public "look up my bookings" endpoint in v1, precisely because without accounts it would need a guessable identifier. The owner dashboard reads customers behind `customer.read`.

---

## 17. Booking Boundary

**Recommendation: one concrete table per vertical booking model, carrying the shared columns inline. No shared `bookings` header table. A shared `customers` table referenced by FK from each.**

The three candidates, judged against repository reality:

| Option | Verdict |
|---|---|
| Single `bookings` table + vertical detail tables | **Rejected on a technical impossibility, not a preference.** The race-safe guarantee (§15) requires `staff_id` and `tstzrange(starts_at, ends_at)` **in the same row** — an `EXCLUDE` constraint cannot span tables. Header-owns-time + detail-owns-staff makes the invariant inexpressible in the schema and forces every vertical back onto application-level locking. It also makes every booking write a two-table transaction and every read a join, for the benefit of sharing about eight columns. |
| Single table with nullable vertical FKs (`room_id`, `table_id`, `staff_id`, `trip_id`) | **Rejected**, and the baseline rejects it too. |
| **One table per booking model, shared column vocabulary** | **Recommended.** `appointments` today; `reservations`, `stays`, `trip_bookings` later. Each carries `id, tenant_id, customer_id, status, starts_at, ends_at, currency, total_minor, created_at, updated_at` with identical names and semantics, plus its own vertical columns and its own correctness constraint. |

**What "shared" concretely means without a shared table:**

- Identical column names and types across vertical booking tables — a convention this document establishes and each feature honors.
- One shared status vocabulary and one shared terminality rule (§18), living in a small shared package.
- One shared `customers` table, FK'd from each.
- One shared money convention (§8).

**Cross-vertical reporting later** ("all bookings for this tenant across verticals") does not require restructuring: a `UNION ALL` view over the vertical tables answers it, and a tenant only ever has one `business_type` anyway — so in practice the query is against exactly one table.

**Proposed `appointments` (S10, not final until then):**

```
appointments
  id UUID PK
  tenant_id   UUID NOT NULL REFERENCES tenants(id)
  customer_id UUID NOT NULL
  service_id  UUID NOT NULL
  staff_id    UUID NOT NULL
  starts_at, ends_at TIMESTAMPTZ NOT NULL          CHECK (ends_at > starts_at)
  status VARCHAR(16) NOT NULL DEFAULT 'CONFIRMED'  CHECK (status IN ('CONFIRMED','CANCELLED','COMPLETED','NO_SHOW'))
  service_name_snapshot   VARCHAR(255) NOT NULL     -- §19
  duration_minutes_snapshot INT       NOT NULL
  price_minor_snapshot    BIGINT      NOT NULL
  currency_snapshot       CHAR(3)     NOT NULL
  staff_name_snapshot     VARCHAR(255) NOT NULL
  customer_note TEXT NULL
  idempotency_key UUID NULL                         -- §31
  cancelled_at TIMESTAMPTZ NULL
  created_at, updated_at TIMESTAMPTZ
  FOREIGN KEY (customer_id, tenant_id) REFERENCES customers(id, tenant_id)
  FOREIGN KEY (service_id,  tenant_id) REFERENCES services(id, tenant_id)
  FOREIGN KEY (staff_id,    tenant_id) REFERENCES staff_profiles(id, tenant_id)
  EXCLUDE USING gist (staff_id WITH =, tenant_id WITH =, tstzrange(starts_at, ends_at, '[)') WITH &&)
      WHERE (status <> 'CANCELLED')
  CREATE UNIQUE INDEX ... ON appointments (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL
```

**Deferred deliberately:** multi-service appointments, group bookings, recurring appointments, payment linkage, cancellation reason/policy, reschedule audit trail. Each is additive; none is precluded.

---

## 18. Booking Lifecycle

**Recommended v1 vocabulary: `CONFIRMED`, `CANCELLED`, `COMPLETED`, `NO_SHOW`. `PENDING` is deliberately excluded.**

`PENDING` has no business meaning in v1: nothing produces it. It would exist only if a booking could await payment authorization or owner approval, and neither feature exists or is planned in this roadmap. Shipping an unreachable state means every query, filter and UI branch carries a case that never occurs, and the first real pending-producing feature would likely want different semantics anyway. Adding it later is one line in a CHECK constraint — and §15's `status <> 'CANCELLED'` predicate is already written so that it stays correct when that happens.

Each retained value earns its place: `CANCELLED` frees the slot and is required by §20; `COMPLETED` and `NO_SHOW` are the two distinct real outcomes of a past appointment and drive genuinely different owner behavior.

**Transitions:**

```
CONFIRMED ──▶ CANCELLED   (owner in v1; customer later)
CONFIRMED ──▶ COMPLETED   (owner, after starts_at)
CONFIRMED ──▶ NO_SHOW     (owner, after starts_at)
CANCELLED / COMPLETED / NO_SHOW  ──▶ (terminal)
```

**Shared vs vertical:**

| Rule | Owner |
|---|---|
| The vocabulary itself | **Shared** — a hotel stay and a nail appointment mean the same thing by `NO_SHOW`. |
| Terminality (terminal states never transition again) | **Shared** — a universal integrity rule. |
| *Who* may trigger each transition | **Vertical/policy** — a restaurant may auto-`NO_SHOW` after 15 minutes; a salon will not. |
| *When* a transition is legal (e.g. no `COMPLETED` before `starts_at`) | **Vertical** — depends on the time semantics of the vertical. |
| Side effects (refunds, inventory release, notifications) | **Vertical**, all deferred. |

Enforcement lives in the service layer, not the database: a CHECK constraint cannot see the previous value, and a trigger would be the first trigger in this codebase.

---

## 19. Historical Snapshot Strategy

**Recommendation: snapshot *and* keep the foreign keys. Both, not either.**

| Approach | Why alone it fails |
|---|---|
| Resolve everything live via FK | A price change silently rewrites history. The customer paid ₦8,000; the receipt now says ₦12,000. Same for a renamed service ("Gel Manicure" → "Gel Manicure (Classic)") and an archived one. |
| Snapshot only, no FK | Breaks every legitimate join: "how many appointments did this service get", "this technician's schedule", "revenue by service". Also loses referential integrity. |
| **Both** | Snapshots answer *what was agreed*; FKs answer *what it relates to*. They are different questions and both get asked. |

**Snapshotted at creation, never recomputed:** `service_name`, `duration_minutes`, `price_minor`, `currency`, `staff_display_name`.

- `duration_minutes` is snapshotted even though `ends_at` already encodes it — because `ends_at` may later be edited by a reschedule, and the agreed service length is a separate fact from the scheduled window.
- `currency` is snapshotted because it lives on the tenant (§8) and the tenant is a mutable row.
- `staff_display_name` is snapshotted because a technician can be renamed or archived, and the customer's confirmation should keep saying who they booked with.
- Customer contact details are **not** snapshotted: unlike a price, the *current* contact detail is the useful one — you call the customer at their current number, not the one they gave in March.

**Never snapshotted:** anything derivable at read time without semantic drift (tenant name, slug, timezone).

---

## 20. Cancellation / Rescheduling

**Cancellation: in the first baseline, owner-only.** Not a nice-to-have — the exclusion constraint means a mistaken booking **permanently blocks that technician's slot** with no way to release it. A booking system that can create but not cancel is operationally broken on day one. Scope: `POST /tenants/{id}/appointments/{id}/cancel`, sets `status = 'CANCELLED'` and `cancelled_at`. The constraint's `WHERE` predicate frees the slot automatically the moment status changes — no extra work.

**Customer-initiated cancellation: deferred.** It needs an identity mechanism for an anonymous customer (an unguessable booking reference emailed to them), and there is no email delivery in this codebase (§32).

**Rescheduling: deferred.**

**Data-model consequences settled now, so it is never blocked:**

| Requirement | Already satisfied? |
|---|---|
| Changing `starts_at`/`ends_at` must re-check conflicts | Yes — the exclusion constraint is on the table, so an `UPDATE` is checked identically to an `INSERT`. Reschedule can be a plain update. |
| Cancel-and-recreate must also work | Yes — cancelling frees the slot via the predicate; nothing prevents a new row. |
| History must survive a reschedule | Snapshots are creation-time and never recomputed (§19), so they already do. |
| Policy (free until 24h before, etc.) | Needs a policy model; **nothing in the current design precludes it.** No column is required today. |

**No columns are added now for cancellation policy or reschedule audit.** Both are additive.

---

## 21. Business Owner APIs — Nail

Every route follows the established chain `authMiddleware.Wrap(tenantMiddleware.Wrap(TenantPermissionMiddleware{...}.Wrap(handler)))` and is registered by hand in `app.New()`.

### S1 — Services

| Method | Path | Auth | Permission | Purpose |
|---|---|---|---|---|
| `POST` | `/api/v1/tenants/{tenantID}/services` | Bearer | `service.create` | Create a service. Body: `name, description?, duration_minutes, price_minor`. |
| `GET` | `/api/v1/tenants/{tenantID}/services` | Bearer | `service.read` | List. Query: `status=ACTIVE\|ARCHIVED\|ALL` (default `ACTIVE`). |
| `GET` | `/api/v1/tenants/{tenantID}/services/{serviceID}` | Bearer | `service.read` | Single service. |
| `PATCH` | `/api/v1/tenants/{tenantID}/services/{serviceID}` | Bearer | `service.update` | Partial update via nullable-pointer diffing, exactly as `UpdateProfile` does. |
| `POST` | `/api/v1/tenants/{tenantID}/services/{serviceID}/archive` | Bearer | `service.archive` | One-way `ACTIVE → ARCHIVED`. Idempotent. |
| `PUT` | `/api/v1/tenants/{tenantID}/currency` | Bearer | `tenant.update` | Write-once tenant currency (§8). Idempotent when unchanged. |

**No `DELETE`** anywhere — appointments FK to services and staff. Archive is the delete.

### S3 — Technicians

| Method | Path | Permission | Purpose |
|---|---|---|---|
| `POST` | `.../staff` | `staff.create` | Create a profile. Body: `display_name, bio?, user_id?, is_bookable?`. |
| `GET` | `.../staff` | `staff.read` | Roster. |
| `PATCH` | `.../staff/{staffID}` | `staff.update` | Update name/bio/`is_bookable`. |
| `POST` | `.../staff/{staffID}/archive` | `staff.archive` | One-way archive. |
| `PUT` | `.../staff/{staffID}/services` | `staff.update` | **Replace** the whole capability set (`{service_ids: [...]}`). One idempotent call beats per-link POST/DELETE for a checkbox UI. |
| `GET` | `.../staff/{staffID}/services` | `staff.read` | Current capability set. |

### S5 — Hours & Exceptions

| Method | Path | Permission | Purpose |
|---|---|---|---|
| `PUT` | `.../staff/{staffID}/working-hours` | `staff.update` | **Replace** the full weekly schedule (`{hours: [{weekday, start_time, end_time}, ...]}`). Whole-week replacement makes multi-shift editing atomic and avoids per-row reconciliation. |
| `GET` | `.../staff/{staffID}/working-hours` | `staff.read` | Read it back. |
| `POST` | `.../schedule-blocks` | `staff.update` | Create a block; `staff_id` omitted = tenant-wide. |
| `GET` | `.../schedule-blocks` | `staff.read` | List within a bounded date range. |
| `DELETE` | `.../schedule-blocks/{blockID}` | `staff.update` | Blocks have no history worth keeping — a real delete, unlike services and staff. |

### S7 — Availability (owner)

| Method | Path | Permission | Purpose |
|---|---|---|---|
| `GET` | `.../availability?service_id&staff_id?&from&to` | `service.read` | Owner-side slot preview. Same engine as the public endpoint, no separate code path. |

### S11 — Bookings

| Method | Path | Permission | Purpose |
|---|---|---|---|
| `GET` | `.../appointments?from&to&staff_id?&status?` | `booking.read` | Calendar/list. Bounded range required. |
| `GET` | `.../appointments/{id}` | `booking.read` | Detail, including customer contact. |
| `POST` | `.../appointments` | `booking.create` | Owner books on a customer's behalf (phone booking) — the same transaction as the public path. |
| `POST` | `.../appointments/{id}/cancel` | `booking.update` | §20. |
| `POST` | `.../appointments/{id}/status` | `booking.update` | `COMPLETED` / `NO_SHOW`. A single transition endpoint, not two. |
| `GET` | `.../customers?query?` | `customer.read` | Customer list; PII behind its own permission. |

**Explicitly not created:** no bulk endpoints, no `PUT` full-replace on services or staff, no `DELETE` on anything that owns history, no CRUD on join tables (replaced wholesale instead), no reporting/analytics endpoints.

---

## 22. Public / Customer APIs — Nail

All anonymous, all under `/api/v1/public/`, all registered **before** the auth middleware is constructed — the structural safeguard `app.go` already documents for the existing public tenant route.

| Method | Path | Auth | Returns |
|---|---|---|---|
| `GET` | `/public/tenants/{slug}` | none | **Exists.** `{slug, name, description, timezone, business_type}`. Unchanged. |
| `GET` | `/public/tenants/{slug}/services` | none | `[{id, name, description, duration_minutes, price_minor, currency}]` — `ACTIVE` only. |
| `GET` | `/public/tenants/{slug}/services/{serviceID}/staff` | none | `[{id, display_name, bio}]` — eligible, bookable, `ACTIVE` only. |
| `GET` | `/public/tenants/{slug}/availability?service_id&staff_id?&from&to` | none | `{timezone, days:[{date, slots:[{starts_at}]}]}`. `staff_id` omitted = any available. |
| `POST` | `/public/tenants/{slug}/appointments` | none | Create. Body: `{service_id, staff_id?, starts_at, customer:{name,email?,phone?}, note?}` + `Idempotency-Key` header. Returns `{id, starts_at, ends_at, service_name, staff_name, price_minor, currency, status}`. |

**Which require authentication: none.** Guest booking is the design (§16). If customer accounts ship later, the same endpoints gain an *optional* bearer token that links the booking to a `user_id` — additive, not a second API.

**Data minimization, per endpoint:**

- **Never exposed:** tenant UUID, tenant `status`/`onboarding_status`, contact email/phone, service `status`, staff `user_id`, staff email, `is_bookable`, any customer record, any internal timestamps.
- **Service and staff UUIDs *are* exposed** — a deliberate decision. They are v4 UUIDs (unguessable, non-enumerable), and the alternative (public per-service slugs) introduces a new uniqueness domain and a new validation surface for no security gain. The tenant remains addressed by **slug** everywhere, matching the established public pattern.
- Every public read reuses `PublicTenantService.GetBySlug`'s visibility gate rather than re-deriving it, so `DISABLED` and `IN_PROGRESS` tenants stay uniformly invisible.

---

## 23. Nail Customer Journey

Route: `/(public)/book/[slug]` — resolves tenant identity, switches on `business_type`, renders `NailBookingExperience`. (This is the onboarding plan's F8 shell; see §37 for the dependency.)

| # | Screen | Data needed | API call | Loading | Empty | Errors | Not yet supported |
|---|---|---|---|---|---|---|---|
| 1 | **Business page** | name, description, timezone, business_type | `GET /public/tenants/{slug}` | Skeleton header | — | `TENANT_NOT_FOUND` → 404 page (identical for disabled, incomplete, reserved, nonexistent) | Branding, logo, gallery, reviews |
| 2 | **Select service** | service list | `GET .../services` | Skeleton list | "This business hasn't published services yet." — **a real state**, since a tenant can complete onboarding with zero services (§26) | Network → retry | Categories, search, add-ons, packages |
| 3 | **Select technician** | eligible staff + "Any available" | `GET .../services/{id}/staff` | Skeleton | Zero eligible staff → block progress with "This service isn't bookable online right now" | — | Photos, ratings, per-staff pricing |
| 4 | **Select date** | which days have any slot | `GET .../availability?from&to` (month, ≤31 days, ≤90 ahead) | Calendar skeleton | Fully-booked month → "No availability this month", next-month affordance | Range too wide → clamp client-side before sending | Waitlists, recurring bookings |
| 5 | **Choose time** | slots for the chosen date | same response, filtered | Inline spinner | "No times available on this date" | Stale slots → step 7 handles it | Time-of-day preference filters |
| 6 | **Customer details** | — | none | — | — | Client-side: name required, at least one of email/phone | Saved profiles, marketing consent |
| 7 | **Review & book** | full selection + price | `POST .../appointments` + `Idempotency-Key` | **Disable the button while in flight** | — | `SLOT_UNAVAILABLE` → "That time was just taken", auto-refetch availability, return to step 5. `VALIDATION_FAILED` → field errors. Network timeout → **retry with the same key** (§31) | Payment, deposits, coupons |
| 8 | **Confirmation** | the POST response only | none | — | — | — | "Add to calendar", email confirmation (§32), cancel/reschedule links (§20) |

**Cross-cutting rules:**

- All times rendered in the **tenant's** timezone with the zone visibly labelled — never the visitor's device zone.
- The `Idempotency-Key` is generated **once when the user reaches step 7**, not per click, and is reused for every retry of that same booking attempt.
- Availability is never cached across a booking failure — the failure path always refetches.
- No screen shows another customer's data, and no screen exposes a tenant UUID.

---

## 24. Nail Dashboard Implications

Approved modules, gated on **both** `business_type` and permission, each landing only with the feature that gives it content:

| Nav item | Lands with | Permission | Notes |
|---|---|---|---|
| Dashboard | exists | — | Stays minimal until there is real data to show. |
| Services | **S2** | `service.read` | First new module. |
| Technicians | **S4** | `staff.read` | Roster + capability assignment. |
| Availability | **S6** | `staff.read` | Working hours + time off. |
| Bookings | **S11** | `booking.read` | Calendar/list. |
| Customers | **S11** | `customer.read` | Only meaningful once bookings create customers. |
| Settings | later | `tenant.update` | Profile/currency; not part of this roadmap. |

**No fabricated analytics.** No revenue charts, no occupancy metrics, no "bookings this week" tile until `appointments` exists and the number is real. `dashboard-nav.ts`'s own comment already forbids placeholder entries, and this plan honors it.

**What can be built before the booking engine exists:** everything through S6 — the whole catalog, roster and schedule configuration surface. That is a genuinely useful product on its own (an owner can set up their business), and it is deliberately the bulk of the early roadmap.

**The one change to existing frontend code**, flagged rather than assumed: `NavItem` gains an optional `businessType?: BusinessType[]`, and `Sidebar`'s filter gains a second predicate:

```ts
dashboardNavItems.filter(item =>
  (!item.permission   || can(permissions, item.permission)) &&
  (!item.businessType || (currentTenant?.business_type != null &&
                          item.businessType.includes(currentTenant.business_type)))
)
```

Fails closed on an unknown or null `business_type`, matching `PermissionsProvider`'s philosophy. This is ~5 lines and belongs to **S2**. It is *not* the onboarding plan's F7 — see §37.

---

## 25. Restaurant Divergence

*Not designed in this pass. Documented only to prove the scheduling model is not reusable.*

**Why Nail's Service + Staff scheduling cannot simply be reused:**

- **The customer does not choose a named service.** They choose a *date, time and party size*. There is no duration the customer selects — turn time is a house rule (typically 90–120 minutes) that the restaurant sets, not a product the customer picks.
- **The constrained resource is aggregate capacity, not a person's calendar.** Availability at 19:00 is "does the floor have a free two-top", answered by counting concurrent seated parties against capacity — a **counting** problem, not an interval-intersection problem.
- **The exclusion constraint does not transfer.** Two parties at 19:00 are perfectly legal. Overlap is normal; exceeding capacity is not. Restaurant conflict prevention needs a capacity check under a lock or a serializable transaction — genuinely different machinery from §15.
- **Tables are usually allocated late**, often at the door. The booked entity is a capacity commitment, not a specific table — so a "book a table" FK would be wrong on day one.
- **Hours are tenant-level**, not per-person, and are typically split into distinct services (lunch/dinner) — closer to `staff_working_hours`' multi-row shape but hanging off the tenant.

**Likely restaurant-owned concepts:** opening hours (tenant), service periods, turn time, party-size rules (min/max, large-party approval), capacity, areas/zones, tables, table combinations, reservation, allocation.

**What genuinely is reused:** `customers` (unchanged), the booking status vocabulary, the money convention, tenant timezone, the public-visibility gate, the error contract, the whole auth/RBAC/tenant-context stack, and the `[start, end)` half-open time convention.

---

## 26. Hotel Divergence

*Not designed in this pass.*

**Why appointment slots are the wrong abstraction:**

- **The time axis is a date range, not an instant plus a duration.** A stay is `[check_in_date, check_out_date)` in **dates**, not timestamps — a fact `TIMESTAMPTZ` columns model badly and slot generation does not model at all.
- **Availability is inventory arithmetic across a range.** "Are 2 deluxe rooms free from the 3rd to the 7th" means: for every night in that range, `count(rooms of type) − count(overlapping stays) >= 2`. There is no grid, no duration fitting, no working hours.
- **The customer shops a room *type*, not a specific room.** Physical assignment happens at or near check-in. So the booked entity is a *type-plus-dates* commitment, which has no analogue in the appointment model.
- **Price is not a property of the sellable unit.** It is a rate that varies by date, length of stay, occupancy and channel — a table of its own, not a column.
- **Occupancy rules** (max adults/children, extra beds) constrain bookability in a way nothing in the nail model does.

**Likely hotel-owned concepts:** room type, physical room, rate plan, rate calendar, inventory/allotment, occupancy rules, amenities, stay/reservation, check-in/check-out policy.

**What is reused:** the same list as §25.

---

## 27. Transport Divergence

*Not designed in this pass.*

**Why a route is not the booked entity:** a route (Lagos → Abuja) is a *template*. What a passenger buys is a **scheduled trip** — the 07:00 departure on 14 March, operated by a specific vehicle with a specific seat count and a specific fare. Two passengers booking "the Lagos–Abuja route" have bought nothing in common unless they are on the same trip. Modelling the route as bookable would make capacity meaningless, because a route has no capacity — only a trip does.

**Why the scheduling model does not transfer:** availability is not computed from working hours; it is `seats_total − seats_sold` on a row that already exists. Departures are **published**, not derived — the operator creates them (possibly by generating them from a route timetable), so there is no availability *engine* at all, just a count. And a seat may be a specific assigned seat, which is closer to a hotel room than to a technician's calendar.

**Likely transport-owned concepts:** location/terminal, route, route stop, vehicle, seat map, scheduled trip, trip status, fare/fare class, passenger (per-seat, distinct from the booking's customer), ticket.

**One genuinely new shared need:** transport is the first vertical where the **passenger ≠ the booker** (one person books four seats for four named travellers). That is a `passengers` table hanging off the trip booking, not a change to `customers`.

**What is reused:** the same list as §25.

---

## 28. Shared Availability Interface

**Recommendation: do not define `AvailabilityProvider` or `AvailabilityQuery` now.**

Reasons, in order of weight:

1. **One implementation.** An interface with a single implementer adds indirection and removes nothing. This codebase's own convention supports that: interfaces exist where there are ≥2 implementations or a test fake needs one (`TenantRepository`, `Authorizer`, `PermissionResolutionService`) — never as decoration.
2. **There is no shared caller.** An abstraction is only useful if something calls it without knowing which implementation it has. Every availability caller in this roadmap is vertical-specific by construction: the Nail booking page calls Nail availability. A tenant has exactly one `business_type`, so even the public router resolves the vertical before availability is ever queried.
3. **The signatures genuinely differ.** Nail: `(service, staff?, date range) → []slot start`. Restaurant: `(party size, date, service period) → []time with capacity`. Hotel: `(room type, date range, occupancy) → available count`. Transport: `(origin, destination, date) → []trip with seats left`. Forcing these through one signature produces a parameter bag with mostly-ignored fields — the interface equivalent of the nullable table the baseline forbids.

**The concrete trigger for revisiting** (so this is a deferral, not a refusal): when a second vertical's availability ships **and** a caller appears that must query availability without knowing the vertical — a unified public search across tenant types, or a cross-vertical reminder scheduler. Until both conditions hold, the abstraction has no client.

**What *is* worth sharing now, and costs nothing:** the vocabulary. `[start, end)` half-open intervals, `TIMESTAMPTZ` for instants, tenant-timezone materialization, and "availability is derived, never stored" are conventions this document establishes and every vertical follows. Conventions transfer; premature interfaces do not.

---

## 29. Module Architecture

```
internal/
  app/            (exists) — route wiring; grows by hand, as today
  auth/           (exists)
  authorization/  (exists)
  config/         (exists)
  database/       (exists)
  errors/         (exists) — extended with new codes per feature
  identity/       (exists)
  tenant/         (exists) — touched only for the currency column
  money/          NEW — Amount value type, ISO-4217 allow-list. No DB, no HTTP.
  customer/       NEW — model/repository/service/handler. Shared, tenant-scoped.
  scheduling/     NEW — the appointment booking model. Nail's home.
    model/        Service, StaffProfile, WorkingHours, ScheduleBlock, Appointment, Status types
    repository/   Postgres*, narrow and interface-segregated
    service/      catalog, staff, schedule, availability (incl. pure ComputeSlots), booking
    handler/      private (tenant-scoped) + public handlers, kept in separate files
```

Later verticals: `internal/dining`, `internal/lodging`, `internal/transit` — same internal shape.

**Why not `internal/nail`:** §5. The module is named for the booking model because that is what generalizes.

**Why one `scheduling` module rather than `catalog` + `staff` + `availability` + `booking`:** the availability engine needs services, staff, capabilities, hours and appointments simultaneously; the booking service needs all of those plus customers. Splitting them into four packages either creates an import cycle or forces a fifth coordination package. This codebase has **already been bitten by exactly this** — `tenantService.Create` carries a comment explaining it cannot import `authzservice` because of a cycle through `internal/tenant`, and works around it with a tx-scoped repository call. One cohesive module avoids re-learning that.

`internal/customer` is separate because it genuinely is shared across verticals and has no dependency on scheduling. The dependency direction is one-way: `scheduling → customer`, never the reverse.

**Interface segregation:** follow the `OnboardingRepository`-vs-`TenantRepository` precedent — narrow interfaces per concern (`ServiceRepository`, `StaffRepository`, `ScheduleRepository`, `AppointmentRepository`) so test fakes never grow methods they don't use, even when one Postgres struct implements several.

**Future extraction:** because every cross-module reference is a Go interface and every cross-domain FK is explicit, extracting `scheduling` into a service later is a matter of replacing repository implementations and the two composite FKs to `customers`. **No microservice design is done now** — no message contracts, no service discovery, no API gateway.

---

## 30. Permission Plan

Following the existing per-action granularity — **not** `.manage`, because nothing in the current catalog is coarse-grained and introducing a second granularity model would make the catalog inconsistent.

**S1 (first feature) — exactly four:**

| Code | Purpose |
|---|---|
| `service.read` | View the catalog. |
| `service.create` | Add a service. |
| `service.update` | Edit name/description/duration/price. |
| `service.archive` | Take a service out of circulation. |

**On `archive` vs `disable`:** `user.disable` is the existing precedent verb, but in this codebase `disable` consistently means *barring an actor from acting*. A service is not an actor. Reusing `disable` would blur a word with a precise access-control meaning; `archive` matches the status value (§7) and the UI verb. One new verb, argued rather than assumed. All four satisfy `ValidatePermissionCode`'s pattern.

**Deferred to the features that need them** — listed so the shape is agreed, **not** created now:

| Feature | Permissions |
|---|---|
| S3 Technicians | `staff.read`, `staff.create`, `staff.update`, `staff.archive` |
| S5 Hours & exceptions | none new — reuses `staff.update` / `staff.read`. A separate `schedule.*` family would split one owner-facing concept ("manage this technician") across two permissions with no scenario where they differ. |
| S7 Availability (owner) | none new — reuses `service.read`. |
| S10/S11 Bookings | `booking.read`, `booking.create`, `booking.update`, `customer.read` |

`booking.cancel` is **rejected** in favour of `booking.update`: there is no realistic role that may mark a booking `COMPLETED` but not `CANCELLED`.

**Migration discipline:** each feature's migration inserts only its own permissions and role grants. `INSERT ... SELECT` over the whole permissions table is never repeated (see §31).

**Frontend:** `types/permission.ts`'s `KnownPermissionCode` union is extended in the same feature. The `(string & {})` widening means a version skew degrades to "no autocomplete", never a crash — already handled.

---

## 31. Role Matrix Impact

| Permission | BUSINESS_OWNER | STAFF | SUPER_ADMIN |
|---|---|---|---|
| `service.read` | ✅ | ✅ | ⚠️ see below |
| `service.create` | ✅ | ❌ | ⚠️ |
| `service.update` | ✅ | ❌ | ⚠️ |
| `service.archive` | ✅ | ❌ | ⚠️ |
| `staff.read` (S3) | ✅ | ✅ | ⚠️ |
| `staff.create/update/archive` (S3) | ✅ | ❌ | ⚠️ |
| `booking.read` (S11) | ✅ | ✅ | ⚠️ |
| `booking.create/update` (S11) | ✅ | ✅ | ⚠️ |
| `customer.read` (S11) | ✅ | ❌ | ⚠️ |

**`STAFF` gets reads, not writes.** A technician needs to see the menu, the roster and the day's bookings to do their job. They must not be able to change prices, add services, or edit colleagues' profiles — pricing is an owner decision, and `STAFF` today holds only read permissions, so granting writes would be a meaningful expansion of that role's meaning rather than an extension of it.

**`STAFF` gets `booking.create`/`booking.update`.** A technician taking a walk-in or marking a no-show is core to their job. This is the one place `STAFF` gains write capability, and it is scoped to the operational record, never to configuration.

**`STAFF` does not get `customer.read`.** Customer contact details are the most sensitive data in the system. Booking detail views for `STAFF` show the customer's name only; the phone number requires `customer.read`.

**`SUPER_ADMIN` — two facts that must be stated together:**

1. **Operationally it changes nothing.** `ResolveTenant` reads **only TENANT-scoped roles**; `SUPER_ADMIN` is PLATFORM-scoped, so it resolves to *zero* permissions inside any tenant today. Granting it `service.*` does not give a platform admin access to tenant catalogs — and **this plan deliberately does not create a platform path into tenant data.** That would be a significant security decision requiring its own design and audit trail.
2. **The seed will not do it for you.** `000006` granted `SUPER_ADMIN` every permission via `INSERT ... SELECT id FROM permissions` — a one-time snapshot. New permissions are **not** picked up. Each feature's migration must grant explicitly, or the catalog quietly becomes inconsistent (an audit would show `SUPER_ADMIN` missing permissions it appears to have by convention).

**Recommendation:** grant new permissions to `SUPER_ADMIN` explicitly in each migration, for catalog consistency, while noting in the migration comment that it confers no tenant access. Never use `SELECT id FROM permissions` again — it would re-grant everything on every migration and hide exactly this class of drift.

**No new roles.** `TECHNICIAN` as a role is explicitly rejected — bookability is a `staff_profiles` property, not an authority level (§10).

---

## 32. Error Contract

**Reuse first.** These need **no** new codes:

| Situation | Existing code |
|---|---|
| Invalid duration/price/name/time format | `VALIDATION_FAILED` |
| Malformed JSON, unparseable UUID | `INVALID_REQUEST` |
| Missing permission | `PERMISSION_DENIED` |
| Cross-tenant access / non-member | `TENANT_ACCESS_DENIED` |
| Unknown tenant slug (public) | `TENANT_NOT_FOUND` |
| Any infrastructure failure | `INTERNAL_ERROR` (via `Map`'s fallback) |

**New codes — four across the entire roadmap, each added by the feature that needs it:**

| Code | Status | Feature | Notes |
|---|---|---|---|
| `SERVICE_NOT_FOUND` | 404 | S1 | Per-entity 404s are this codebase's convention (`USER_NOT_FOUND`, `TENANT_NOT_FOUND`, `ROLE_NOT_FOUND`), not `RESOURCE_NOT_FOUND`. |
| `STAFF_NOT_FOUND` | 404 | S3 | Same. |
| `SLOT_UNAVAILABLE` | 409 | S10 | See the collapsing decision below. |
| `BOOKING_NOT_FOUND` | 404 | S11 | Same. |

**Deliberately collapsed into `SLOT_UNAVAILABLE`, and why:** `SERVICE_INACTIVE`, `STAFF_NOT_ELIGIBLE`, `STAFF_NOT_BOOKABLE`, `OUTSIDE_WORKING_HOURS`, `BOOKING_CONFLICT` and the raw exclusion-constraint violation all become one public code. This is a **security decision**: distinct codes let an anonymous caller probe a tenant's configuration — which technicians do which services, when they work, which services are hidden — by watching error codes change. One code discloses nothing, and the client's correct response is identical in every case: refetch availability and pick again.

**On the owner-facing endpoints** — where the caller is already authenticated, tenant-scoped and permitted — specific codes are appropriate, because there is nothing to disclose that they cannot already read. `SERVICE_NOT_FOUND` on the private path, `SLOT_UNAVAILABLE` on the public one.

**Business vs system errors** — the existing rule, unchanged: business outcomes become `apperrors.New(code, ...)` with a mapped code; system failures are `fmt.Errorf("...: %w", err)` and `Map` turns them into `INTERNAL_ERROR` because the code is unknown. `Map`'s `isKnownCode` guard means a forgotten map entry fails **safe** (500, no leak) rather than leaking an unmapped code.

**No codes are created in this pass.**

---

## 33. Security Invariants

1. **Every table is tenant-scoped.** `services`, `staff_profiles`, `staff_services`, `staff_working_hours`, `schedule_blocks`, `customers`, `appointments` all carry `tenant_id`, and every query filters on it.
2. **Cross-tenant references are impossible at the schema level**, via `UNIQUE (id, tenant_id)` parents and composite FKs (§11). This is stronger than any existing guarantee in the codebase and should be adopted wherever a new FK crosses tables.
3. **Tenant identity is never client-supplied.** It comes from `TenantContext` (private routes) or from the slug lookup (public routes). Handler decode targets have no `tenant_id` field.
4. **Public endpoints reuse the shipped visibility gate** — `ACTIVE && COMPLETED` — rather than re-deriving it. Disabled, incomplete, reserved-slug and nonexistent tenants stay indistinguishable.
5. **Public reads are filtered twice:** the tenant must be visible **and** the row must be publishable (`service.status = ACTIVE`; staff `ACTIVE && is_bookable` and eligible).
6. **No tenant UUID on any public surface.** Slug only, as today.
7. **No staff account data on any public surface** — no `user_id`, no email, no `is_bookable` flag, no archived staff.
8. **No customer data on any public surface**, including the confirmation response, which echoes only the caller's own submission.
9. **The client never supplies authoritative values** (§34).
10. **Booking confirmation re-checks inside the transaction**, and the exclusion constraint settles the race regardless (§15).
11. **Frontend capability checks stay UX-only.** `Can`/`useCan` never gate anything the backend does not also enforce.
12. **`business_type` never affects authorization** — it selects UI and nav, nothing else. Already true; must stay true.
13. **Public endpoints are bounded** — max 31-day range, max 90-day horizon — because no rate limiter exists (§37).

---

## 34. Booking Security — client-supplied values

| Value | Source |
|---|---|
| `price_minor`, `currency` | **Server**, from the service row and tenant currency. Never accepted. |
| `duration_minutes`, `ends_at` | **Server**, derived from the service. Never accepted. |
| `tenant_id` | **Server**, from the slug or tenant context. |
| Staff eligibility | **Server**, re-checked against `staff_services` in the transaction. |
| Availability | **Server**, re-checked in the transaction; the constraint is final. |
| `status` | **Server**, always `CONFIRMED` on creation. |
| `service_id`, `staff_id` (optional), `starts_at` | **Client** — these are selections, validated against persisted state. |
| Customer name/email/phone, note | **Client** — the only genuinely user-authored data. |
| `Idempotency-Key` | **Client**, treated as an opaque dedup token with no authority. |

**Enforcement mechanism: the decode target's field list**, exactly as `OnboardingHandler.SaveProgress` and `TenantHandler.Create` already do. A client sending `price_minor` has it silently discarded because no field exists to decode it into. This is structural, not a validation rule someone can forget.

---

## 35. Detailed Feature Breakdown

Ordering note: there is **no "F1 — Shared Foundations"** feature. The baseline's step-1 shared decisions (money, timezone, customer identity, status vocabulary) are *decisions*, made in this document; the code for each lands with the first feature that needs it. A foundations feature would ship a `money` package and a `customers` table with no caller.

---

### S1 — Nail Service Catalog (backend) ← **NEXT**

- **Goal:** a tenant can define its service menu over HTTP.
- **Backend:** `internal/money` (Amount + ISO-4217 allow-list); `internal/scheduling/{model,repository,service,handler}` for `Service`; six endpoints (§21).
- **Frontend:** none.
- **Migration:** `services` table (+ `UNIQUE (id, tenant_id)`); `tenants.currency CHAR(3) NULL`; four permissions + role grants.
- **Permissions:** `service.read/create/update/archive` → BUSINESS_OWNER all, STAFF read, SUPER_ADMIN explicit (§31).
- **Dependencies:** none.
- **TDD acceptance:** model validation unit tests (duration/price bounds, name trim, status transitions); repository integration tests (round-trip, tenant scoping, archive-not-delete, currency write-once); service unit tests with fakes; handler tests (decode-target protection — sending `tenant_id`/`status`/`currency` changes nothing); route-chain tests in `internal/app` (401 unauthenticated, 403 wrong permission, 403 cross-tenant, 200 owner); a **cross-tenant isolation test** proving Tenant B cannot read or patch Tenant A's service.
- **Non-goals:** no categories, no display order, no add-ons, packages, templates, buffers, `online_bookable`, deposits, staff, availability, booking, or public exposure.

### S2 — Service Catalog Owner UI

- **Goal:** the owner manages the menu in the dashboard.
- **Backend:** none.
- **Frontend:** `modules/services/{api,keys,queries}.ts`; `/dashboard/services` list + create/edit forms + archive; `NavItem.businessType` predicate and the `Sidebar` filter extension (§24); `types/permission.ts` union extended; money input in major units, converted to minor at the boundary and **never** stored as a float in component state.
- **Migration:** none. **Permissions:** none new.
- **Dependencies:** S1.
- **TDD acceptance:** **install Vitest + Testing Library first** (§36) — this is the first feature with real frontend logic and there is no test infrastructure today. Cover: query-key isolation and cache invalidation after mutations; the major↔minor money conversion (including `19.99 → 1999`, no float drift); permission gating hides create/edit; `business_type` gating hides the nav entry for non-Nail tenants; `SLOT_*`/`VALIDATION_FAILED` error rendering.
- **Non-goals:** no bulk edit, no drag-to-reorder (no `display_order`), no images.

### S3 — Technicians & Service Capability (backend)

- **Goal:** a tenant can define who works there and what each person does.
- **Backend:** `StaffProfile` + `staff_services` model/repository/service/handler; six endpoints (§21).
- **Migration:** `staff_profiles` (+ partial unique on `(tenant_id, user_id)`, + `UNIQUE (id, tenant_id)`); `staff_services` with composite FKs; four `staff.*` permissions.
- **Permissions:** `staff.read/create/update/archive`.
- **Dependencies:** S1 (capability references services).
- **TDD acceptance:** one profile per user per tenant (nullable `user_id` allows many NULLs); non-login profiles work end to end; **a composite-FK integration test proving the database itself rejects linking Tenant A's staff to Tenant B's service**; capability replacement is idempotent; archive preserves rows.
- **Non-goals:** no per-staff overrides, no avatars, no self-service staff login, no invitations.

### S4 — Technicians Owner UI

- **Goal:** roster management and service assignment.
- **Frontend:** `modules/staff/*`, `/dashboard/technicians`, checkbox capability editor calling the replace endpoint.
- **Dependencies:** S2, S3. **Migration/permissions:** none.
- **TDD acceptance:** capability editor sends the full set; optimistic state reconciles with the server response; permission and vertical gating.
- **Non-goals:** no calendar view (S6/S11).

### S5 — Working Hours & Exceptions (backend)

- **Goal:** when each technician works, and when they don't.
- **Backend:** `WorkingHours` + `ScheduleBlock` model/repository/service/handler; five endpoints (§21).
- **Migration:** `staff_working_hours`, `schedule_blocks`. **Permissions:** none new (reuses `staff.*`).
- **Dependencies:** S3.
- **TDD acceptance:** multi-shift days round-trip; `end_time > start_time` enforced; whole-week replace is atomic (a failed row leaves the previous week intact); tenant-wide blocks (`staff_id IS NULL`) and staff blocks both persist; blocks require `ends_at > starts_at`; cross-tenant rejection.
- **Non-goals:** no tenant business hours, no additive exceptions, no recurring time off, no holiday calendar.

### S6 — Availability Owner UI

- **Goal:** the owner edits hours and time off.
- **Frontend:** `modules/availability/*`, `/dashboard/availability`, weekly editor supporting multiple shifts per day, time-off list.
- **Dependencies:** S4, S5. **Migration/permissions:** none.
- **TDD acceptance:** multi-shift add/remove; overlapping-shift warning (UX only — the backend tolerates it); timezone label always shown.
- **Non-goals:** no drag-and-drop calendar.

### S7 — Availability Engine + Owner Endpoint

- **Goal:** compute bookable slots.
- **Backend:** `ComputeSlots` pure function + `AvailabilityService` + `GET .../availability`; horizon and range caps.
- **Migration:** none. **Permissions:** none new (`service.read`).
- **Dependencies:** S1, S3, S5.
- **TDD acceptance:** **the deepest unit-test matrix in the roadmap**, all against the pure function with zero fakes — grid alignment; duration must fit entirely inside one contiguous interval; multi-shift gaps excluded; blocks subtracted; existing appointments subtracted (fed as plain intervals, so this is testable before S10 exists); past slots excluded via injected `NotBefore`; half-open boundaries (10:00–11:00 and 11:00–12:00 do not conflict); **DST spring-forward and fall-back in `Europe/London`, explicitly, because `Africa/Lagos` has no DST**; "any available" union across eligible staff; empty results for an archived service, a non-bookable technician, or a day with no hours. Integration tests cover loading + horizon/range rejection.
- **Non-goals:** no public endpoint (S9), no lead time, no buffers, no caching.

### S8 — Public Service Catalog

- **Goal:** a customer can see a business's menu and its technicians.
- **Backend:** public service/handler for `/public/tenants/{slug}/services` and `.../services/{id}/staff`, reusing `PublicTenantService`'s visibility gate.
- **Migration/permissions:** none (anonymous).
- **Dependencies:** S1, S3.
- **TDD acceptance:** anonymous access works with no token; **non-disclosure proof** — archived services, archived staff, non-bookable staff and ineligible staff are absent; an `IN_PROGRESS` or `DISABLED` tenant yields `TENANT_NOT_FOUND` identically to a nonexistent one; no tenant UUID, no `status`, no contact data, no `user_id` in any response.
- **Non-goals:** no availability, no booking, no search.

### S9 — Public Availability

- **Goal:** a customer can see open times.
- **Backend:** `GET /public/tenants/{slug}/availability`, same engine, public gating, bounded range/horizon.
- **Dependencies:** S7, S8. **Migration/permissions:** none.
- **TDD acceptance:** anonymous access; range >31 days and horizon >90 days both rejected; identical results to the owner endpoint for the same inputs (one engine, proven); archived service → empty, not an error that discloses why.
- **Non-goals:** no booking, no holds/reservations of a slot.

### S10 — Booking Creation + Conflict Protection

- **Goal:** a customer can book, and two customers cannot book the same slot. **The correctness-critical feature.**
- **Backend:** `internal/customer` (model/repository/service); `Appointment` model/repository/service; the booking transaction (§15); idempotency (§31); `POST /public/tenants/{slug}/appointments` and the owner-side `POST .../appointments`.
- **Migration:** `CREATE EXTENSION IF NOT EXISTS btree_gist`; `customers` (+ partial unique indexes, + `UNIQUE (id, tenant_id)`); `appointments` (+ exclusion constraint, + composite FKs, + idempotency unique index); `booking.*` and `customer.read` permissions.
- **Permissions:** `booking.read/create/update`, `customer.read` (§31).
- **Dependencies:** S7, S8, S9.
- **TDD acceptance — the concurrency test is mandatory and blocking:**
  - **Concurrency (integration, real Docker DB):** N goroutines POST the identical slot simultaneously → **exactly one** 201, N−1 `SLOT_UNAVAILABLE`, exactly one row. This is the definition of done for this feature.
  - **Idempotency:** the same key replayed returns the same booking, creates no second row, and returns 200/201 rather than a conflict.
  - **Any-available fallback:** with two eligible technicians and one already booked, the request succeeds against the free one.
  - **Snapshots:** changing the service price after booking leaves the appointment's stored price unchanged.
  - **Security:** client-supplied `price_minor`/`ends_at`/`status`/`tenant_id` are discarded; booking an archived service, a non-bookable technician, an ineligible technician, a past slot, or a slot outside hours all yield `SLOT_UNAVAILABLE` on the public path.
  - **Customer dedup:** the same email within one tenant reuses the customer row; the same email across two tenants creates two.
  - **Cancel frees the slot:** cancel, then rebook the same slot successfully.
- **Non-goals:** no payment, no notifications, no customer-facing cancellation, no reschedule, no waitlist, no multi-service appointments.

### S11 — Bookings & Customers Owner UI + Management

- **Goal:** the owner works their day.
- **Backend:** `GET .../appointments`, `GET .../appointments/{id}`, cancel, status transition, `GET .../customers`.
- **Frontend:** `modules/bookings/*`, `modules/customers/*`, `/dashboard/bookings`, `/dashboard/customers`, two nav entries.
- **Migration:** none. **Permissions:** none new.
- **Dependencies:** S10.
- **TDD acceptance:** terminal states reject further transitions; `COMPLETED` before `starts_at` rejected; cancel frees the slot (proven by rebooking); `STAFF` can read bookings but not customer phone numbers; `customer.read` gating verified on both layers; bounded date range required.
- **Non-goals:** no analytics, no exports, no bulk actions.

### S12 — Public Nail Booking Journey

- **Goal:** the full customer flow (§23) end to end.
- **Frontend:** `(public)/book/[slug]` with `business_type` routing, `NailBookingExperience` and the eight screens; `Idempotency-Key` generation and reuse; `staleTime: 0` on availability.
- **Backend:** none.
- **Dependencies:** S8, S9, S10, plus the onboarding plan's F8 shell (§37).
- **TDD acceptance:** the whole journey with mocked APIs; `SLOT_UNAVAILABLE` returns the user to time selection with fresh availability; double-click submits once; retry after a simulated timeout reuses the key and produces one booking; times render in the tenant zone with the zone labelled; empty states for a business with no services and a fully-booked month.
- **Non-goals:** no payment, no account creation, no cancel/reschedule links, no "add to calendar", no branding.

---

## 36. Implementation Order

```
S1  Service Catalog (backend)          ← NEXT
S2  Service Catalog UI                 (+ Vitest + Testing Library setup)
S3  Technicians & Capability (backend)
S4  Technicians UI
S5  Working Hours & Exceptions (backend)
S6  Availability UI
S7  Availability Engine + owner endpoint
S8  Public Service Catalog
S9  Public Availability
S10 Booking Creation + Conflict Protection   ← the correctness-critical one
S11 Bookings & Customers (owner)
S12 Public Booking Journey
```

**Why this order rather than the baseline's suggestion:**

- **No shared-foundations feature.** Argued at the head of §35.
- **Backend and UI alternate rather than batching all backend first.** Each pair produces something an owner can actually use, and the UI immediately validates the API shape while it is still cheap to change.
- **Public read (S8/S9) precedes booking (S10).** It is genuinely useful alone — a business gets a public page with a real menu and real times — and it validates the availability engine against live data before the hardest feature depends on it.
- **S10 is not split further.** Booking creation without the exclusion constraint would ship a system that double-books; the constraint without booking creation ships nothing. This one feature is irreducible.
- **Testing infrastructure lands in S2**, at the first feature with real frontend logic, rather than as its own ceremony.

**Parallelization:** S3 is independent of S2, so technicians (backend) can proceed alongside the catalog UI. Everything from S5 onward is strictly sequential.

---

## 37. Risks

| # | Risk | Mitigation |
|---|---|---|
| 1 | **`btree_gist` unavailable on production Postgres.** The entire race-safety design depends on it. | Verify on the real target before S10 — not just `postgres:16-alpine`. It is standard contrib and supported by all major managed providers, but must be confirmed rather than assumed. Fallback (materially worse): `SERIALIZABLE` + retry loop. |
| 2 | **No frontend test infrastructure exists at all.** Every frontend TDD claim in §35 is aspirational until S2 installs one. | S2 explicitly includes Vitest + Testing Library. Do not let S2 ship without it, or the deficit compounds across six more frontend features. |
| 3 | **No rate limiting anywhere**, and this plan adds three anonymous endpoints, one of which (availability) is compute-heavy. | Range and horizon caps (§14) bound the damage. A real limiter is a separate infrastructure feature and should be scheduled before public launch, not before S8. |
| 4 | **DST bugs ship silently** because the likely first market (`Africa/Lagos`) has no DST — tests written against it would pass while the code is wrong. | S7's test matrix mandates `Europe/London` spring-forward and fall-back cases explicitly. |
| 5 | **Guest-booking impersonation.** Anyone who knows an email can book "as" that person within a tenant. | Inherent to guest booking and accepted. Mitigated by never exposing customer history publicly. Revisit if abuse appears — the fix is verification, not schema. |
| 6 | **Auto-linking guest bookings to new accounts is an account-takeover shape** if implemented naively later. | Recorded in §16.C now, while `user_id` is still unused, so the unsafe implementation is never the obvious one. |
| 7 | **`SUPER_ADMIN` permission drift** — the seed's `SELECT id FROM permissions` snapshot means new permissions are silently not granted. | §31: explicit grants in every migration; never repeat the wildcard insert. |
| 8 | **A tenant can complete onboarding with zero services**, so a public page can exist with an empty menu. | Correct and intentional — F3's completion gate is not touched (§38). The customer journey treats it as a real empty state (§23, screen 2). Revisit only if "booking readiness" becomes a product requirement, as a **separate** concept from `onboarding_status`. |
| 9 | **Scope creep back toward add-ons, packages, categories and templates**, all of which a previous plan approved. | §38.6 records the reversals and the reasons, so the decision is re-litigated deliberately rather than drifting. |
| 10 | **Minimum lead time will be requested early** ("stop letting people book 5 minutes from now"). | §14 pre-decides the shape: a tenant column feeding `NotBefore`. Cheap, additive, unblocked. |
| 11 | **Migration runner is transaction-per-file.** A `CREATE INDEX CONCURRENTLY` would fail. | Never use `CONCURRENTLY` in a migration file. `CREATE EXTENSION` inside a transaction is fine. |
| 12 | **`internal/scheduling` grows large** — five aggregates in one module. | Accepted deliberately (§29) against the import-cycle risk this codebase has already hit. If it becomes unwieldy, split along the repository seam (catalog vs booking), not along the service seam. |

---

## 38. Explicit Non-Changes

1. **Auth, RBAC, tenant context, and the middleware chain are not redesigned.** Every new route reuses `authMiddleware.Wrap(tenantMiddleware.Wrap(TenantPermissionMiddleware{...}.Wrap(...)))` verbatim.
2. **`business_type` immutability is preserved.** No endpoint in this plan writes it.
3. **`onboarding_status` is never overloaded or extended.** No `BOOKING_READY` value, no vertical requirements added to `validateOnboardingCompletionPrerequisites`. A tenant that completes the common profile is publicly visible with zero services — already true today, and this plan does not change it (§37.8).
4. **`PublicTenantService`'s visibility gate is reused, not re-implemented.** No second definition of "publicly visible".
5. **The public tenant identity response shape is unchanged.** New public data lives on new endpoints.
6. **Reversals from `NAIL-TECHNICIAN-VERTICAL-IMPLEMENTATION-PLAN.md`**, stated so they are deliberate:
   - **Add-ons and packages** (its N4/N5) — **dropped from the roadmap.** The new baseline's service model has neither, and a polymorphic `package_items` join was the single most complex thing in that plan.
   - **Starter service templates** (its N2) — **dropped.** A seeded template catalog is a content decision with a migration-shaped delivery mechanism; nothing in the new baseline asks for it.
   - **`category_id` as required-from-day-one** — **reversed to deferred** (§7).
   - **`buffer_before/after`, `online_booking_enabled`** — **reversed to deferred** (§7).
   - **No currency at all** — **reversed**: currency is included, on the tenant (§8).
   - **Module names `internal/catalog`, `internal/staff`, `internal/availability`, `internal/booking`** — **replaced** by a single `internal/scheduling` plus `internal/customer` (§29).
   - **A shared `bookings` aggregate** — **replaced** by per-vertical tables (§17).
7. **No `internal/nail`, `internal/restaurant`, `internal/hotel`, or `internal/transport` package** is created (§29).
8. **No event bus, no message queue, no outbox.** The booking service method boundary is the seam a future notifier attaches to; nothing is built for it now (§32/Notifications below).
9. **No payment model.** No deposit columns, no `PENDING` status, no payment tables. The attach point is a future `payments` table referencing `appointment_id` — the service and availability models never touch it.
10. **No frontend page, component, module, or nav entry is created in this pass.** No production file in either repository is modified.

### Payment and notification boundaries (recorded, not designed)

- **Payment.** v1 is **pay in person**. When payment arrives it attaches at exactly two points: a `payments` table keyed by `appointment_id`, and the reintroduction of a `PENDING` status meaning "awaiting authorization" — for which §15's `status <> 'CANCELLED'` predicate is already correct (a pending booking holds its slot). `services` gains no deposit column now; adding one before a payment flow exists to act on it is speculative.
- **Notifications.** The lifecycle events a future notifier consumes are `BookingCreated` and `BookingCancelled` (plus `BookingRescheduled` later). **No event bus is built** — none exists, and one implementation with one consumer does not justify infrastructure. The only architectural requirement is negative: **all booking writes go through the single booking service**, so a notifier can later be injected in one place rather than chased across call sites. The single-service rule already guarantees this.

---

## 39. Next Feature Recommendation

> ## NEXT FEATURE TO IMPLEMENT:
> ## **S1 — Nail Service Catalog (backend)**

**Why this is the smallest useful foundation:**

- **It has zero dependencies.** No staff, no schedule, no availability, no customers, no booking. It can start immediately against the current working tree.
- **It is independently verifiable.** An authenticated `BUSINESS_OWNER` can create, list, update and archive services; a `STAFF` member can read; a cross-tenant caller gets nothing. All provable with the existing test conventions, no new infrastructure.
- **It settles the two decisions everything else inherits** — the money representation (§8) and the module boundary (§29) — at the smallest possible blast radius. Discovering a mistake in either here is cheap; discovering it after staff, hours, availability and booking are built on top is not.
- **It does not commit to anything deferred.** No categories, templates, add-ons, packages, buffers, or bookability flag. Every one of those remains an additive migration.
- **It is a real product increment.** A business owner can enter their actual service menu — the first thing they will want to do after finishing onboarding.

**Deliverables for S1:** one migration (`services`, `tenants.currency`, four permissions and their role grants); `internal/money`; `internal/scheduling/{model,repository,service,handler}` for `Service`; six routes wired in `app.New()`; `SERVICE_NOT_FOUND` added to both maps in `internal/errors`; the full test matrix in §35.

**Before starting S1:** nothing blocking. The Vertical Onboarding F1–F3 backend work was committed as `14000e9` during this planning pass, so S1 starts from a clean tree on `main` rather than on top of an uncommitted feature.

**Dependency to resolve before S2:** S2 needs the `NavItem.businessType` predicate (§24), which overlaps the onboarding plan's F7. Recommendation: **S2 includes only the ~5-line nav predicate**; F7's broader dashboard work (real tenant identity, setup state, richer dashboard content) stays a separate feature and is not started here.
