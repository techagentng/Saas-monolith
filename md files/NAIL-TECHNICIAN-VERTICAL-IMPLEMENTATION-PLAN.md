# Nail Technician Vertical — Services, Staff, Availability & Booking Implementation Plan

**Status: PLAN ONLY — repository-grounded. No production code, migrations, tests, or frontend pages were written for this pass.**

Source baseline: `Nail_Technician_Vertical_Services_Staff_Availability_Booking_Architecture.docx` (conceptual — see §3 for where repository evidence overrode it).

Repositories inspected:
- Backend: `Saas-monolith` (Go modular monolith), working tree currently ahead of the last commit (`d8160f8`) with uncommitted F1–F3 vertical-onboarding work (this session's own prior features) plus refresh-cookie/auth-cookie changes from elsewhere.
- Frontend: `Saas-frontend`, HEAD `8891450`, through F4–F6 of the onboarding architecture (resume-aware `TenantGate`, `OnboardingShell`, the `business_profile` step with a required timezone field).

---

## 1. Executive Summary

The Nail vertical starts from a genuinely empty backend domain: no `service`, `staff`, `availability`, or `booking` concept exists anywhere in the Go codebase today — confirmed by an exhaustive keyword sweep, not assumed. There is also no established money or duration storage convention to inherit; this plan sets one. Everything proposed here is new, which is both the risk (nothing to lean on) and the opportunity (nothing to work around).

Three decisions matter more than the rest and are argued in full below rather than asserted:

1. **Service vs. Add-on vs. Package get three distinct tables, not one polymorphic catalog table and not a bolt-on flag.** Add-ons are structurally close to services but semantically different (never independently bookable, always a delta on a host service); packages are structurally different again (a bundle referencing heterogeneous components). The document's own warning against a fully polymorphic table is honored — polymorphism is used narrowly, only at the package-composition join, where the domain genuinely requires referencing two different item kinds.
2. **Starter templates are tenant-scoped reference data seeded via migration, not Go constants** — this matches the codebase's own existing precedent (`roles`/`permissions` are seeded via migration INSERT, not hardcoded), and it is the only option among those considered that supports template editing, versioning, and localization without a binary redeploy.
3. **`onboarding_status` (F1–F3, already shipped and already gating public profile visibility) is never touched or overloaded.** A tenant that finishes the common business-profile step is publicly visible as a *profile* today, with zero services configured — that is already true and this plan does not change it. "Booking ready" becomes a separate, later, additively-computed concept (§31), not a new value squeezed into the existing status.

No migration is created, no code is written. This document identifies exactly what is required for the smallest useful slice (§40) and sequences the rest.

---

## 2. Repository State

### Backend — confirmed absent (exhaustive keyword sweep across `internal/`, case-insensitive, whole-word: `service|staff|technician|employee|availability|schedule|working hours|booking|appointment|deposit|customer`)

Every match is a false positive: Go's own `internal/*/service` package-layer naming, the `STAFF` role string, or the word "schedule" inside unrelated comments. **Zero existing domain code** for catalog, staff, availability, or booking.

| Area | Actual state |
|---|---|
| Migrations | `000001`–`000009`, ending at Vertical Onboarding F1 (`business_type`/`onboarding_status`/`onboarding_step` on `tenants`). No `service`, `staff`, `booking`, or `customer` table anywhere. |
| Permission catalog | 13 seeded permissions (`000006`), all `user.*`/`tenant.*`/`role.*`/`permission.*`. Nothing service/staff/booking-shaped. |
| Roles | `SUPER_ADMIN` (PLATFORM), `BUSINESS_OWNER` (TENANT, 9 permissions), `STAFF` (TENANT, 4 permissions: `tenant.read`, `user.read`, `role.read`, `permission.read`) — `STAFF` today is a bare RBAC label with **zero profile fields**: no display name, bio, avatar, or bookable flag. Neither does `users` (confirmed: no `name` column exists on `users` at all — the frontend's own comment records this was previously assumed and removed) nor `tenant_memberships` (`id, tenant_id, user_id, status` only). |
| Money | No convention anywhere. The only `int64` fields in the codebase are token-expiry seconds — not money. This is a green-field decision (§12). |
| Duration | No convention anywhere. Green-field (§13). |
| Tenant model | `id, name, slug, status, description, contact_email, contact_phone, timezone, business_type, onboarding_status, onboarding_step, created_at, updated_at`. `timezone` is a validated (`time.LoadLocation`) nullable IANA string — the existing, reusable convention for "what timezone does this business operate in," now required for onboarding completion (F6). |
| Public tenant identity | `GET /api/v1/public/tenants/{slug}` → `{slug, name, description, timezone, business_type}`, gated on `status == ACTIVE && onboarding_status == COMPLETED` (F3, this session). No services, no staff, nothing booking-shaped exposed. |
| Repository/service/handler conventions | Consistent module layout: `internal/<domain>/{model,repository,service,handler}`. Repositories narrow and interface-segregated by concern (e.g. `OnboardingRepository` kept separate from `TenantRepository` specifically so unrelated fakes don't grow unused methods — directly relevant precedent for §17/§20's module boundaries). Handlers thin, decode-target field lists double as the "client cannot set this" security mechanism throughout (`business_type` immutability, `owner_id` never trusted, etc.). |
| Error infrastructure | `apperrors.AppError` + `ErrorCode` + a `statusByCode`/`publicMessageByCode` map (`internal/errors/codes.go`, `http.go`). Adding a code is a two-map, one-file change — cheap, but the existing codes already cover most needs here (§34). |
| App routing | `app.go` wires everything by hand in `New()`; no router-group abstraction, no middleware auto-registration. Every tenant-scoped private route follows `authMiddleware.Wrap(tenantMiddleware.Wrap(TenantPermissionMiddleware{...}.Wrap(handler)))`. |

### Frontend — further along than expected

| Area | Actual state |
|---|---|
| Onboarding | F1–F6 shipped: `TenantGate` is fully resume-aware (reads only `currentTenant.onboarding_status`, never user/session state); `OnboardingShell` + `business_profile` step (with **presentation-only substeps** — `about/contact/timezone/review` — that are never persisted, only the one backend-known step id `business_profile` is). This substep-vs-step separation is a directly reusable pattern for planning Nail's own future onboarding extension (§30). |
| Dashboard | `dashboard/page.tsx` still the pre-F7 minimal state ("Welcome back, {email}"). `dashboard-nav.ts` has exactly one entry (`Dashboard`). F7 (vertical-aware nav + real tenant identity) has **not** shipped yet — confirmed by direct file read, not assumed. |
| Public vertical router (F8) | Not built. No `app/(public)/book/[slug]` route exists yet. |
| `types/tenant.ts` | Already carries `business_type`/`onboarding_status`/`onboarding_step`, confirmed live against the backend — a healthy precedent of keeping frontend types honestly synced to what the backend actually returns, not aspirational. |
| React Query / API client / permissions | `PermissionsProvider` (tenant-scoped, fails closed), `apiClient` (single-flight refresh, `ApiError{code,status,message}`), `modules/<domain>/{api,queries,keys}.ts` triad — all directly reusable for a future `modules/catalog`, `modules/staff`, etc. |

Nothing Nail-specific exists on the frontend beyond generic `BusinessType` plumbing already built for F1.

---

## 3. Baseline vs. Repository

| Document Concept | Repository Reality | Decision |
|---|---|---|
| "What is the minimum Service schema... without prematurely baking in other verticals?" (open question) | No schema exists at all; every field is a fresh decision | Answered field-by-field in §4, none assumed from the document's suggested list wholesale. |
| Suggested `18. Open Architecture Questions` implies these are still open | Correct — none have been answered anywhere in the repo | This entire document exists to answer them; see §5–§32. |
| "The existing shared DashboardShell should remain common" | Confirmed true and already the case — `DashboardShell` is structural-only, `Sidebar` reads a filterable config array (`dashboardNavItems`) | No change needed to the shell; Nail modules are new config rows + new routes, per the already-established pattern (§29). |
| "Should platform starter templates live in code/seed data or dedicated template tables?" | Codebase's own precedent (`roles`/`permissions`) is migration-seeded DB rows, not Go constants | Dedicated template table, seeded via migration — §5. |
| Section 35's implicit assumption that vertical setup timing is still undecided | The *mechanism* is already built and shipped: `onboarding_status == COMPLETED` already gates public profile visibility today, with zero services required | The document's own instruction — "do not silently change it" — is honored: this plan does not touch `onboarding_status`'s meaning at all. A new, separate, additive concept governs booking-readiness (§31). |
| "How should money be represented consistently in the existing backend?" | No existing representation to be consistent *with* | Green-field: integer minor units (§12), the only real decision available. |
| Section 8 "controlled template categories" vs. "tenant-owned rows" as if mutually exclusive | Neither the doc nor the repo forces a choice — categories can be tenant-owned rows *seeded from* template category names at selection time | Tenant-owned only; templates carry a category label, never a separate template-category entity (§8). |

No conflict required silently overriding established backend behavior — the backend has no established behavior to conflict with in this domain, which is the defining fact of this planning pass.

---

## 4. Domain Boundary Decisions — Service Fields

Evaluated against the document's own suggested field list, each judged independently:

| Field | Verdict | Why |
|---|---|---|
| `tenant_id` | **REQUIRED NOW** | Every catalog row is tenant-scoped from row one — this is the isolation boundary, non-negotiable. |
| `category_id` | **REQUIRED NOW** | Every starter template and every customer-facing browse flow is organized by category; retrofitting it later means backfilling every existing row. |
| `name` | **REQUIRED NOW** | Trivially required. |
| `description` | **OPTIONAL NOW** | Nullable from day one — matches the tenant profile's own precedent (`description` is optional there too). |
| `duration_minutes` | **REQUIRED NOW** | Availability computation (§26) is impossible without it; deferring it would mean a painful non-nullable backfill later. |
| `price_minor` | **REQUIRED NOW** | Same reasoning as duration — the public catalog (§19) and booking snapshot (§31) both need it from the start. |
| `deposit`/booking fee | **DEFER** | No payment architecture exists or is in scope (explicit non-goal, §20 of the source doc). Adding a deposit column with no payment flow to act on it is speculative. Revisit when a payment feature is actually planned. |
| `buffer_before`/`buffer_after` | **OPTIONAL NOW, nullable, default 0** | Cheap to add now (same shape as duration), genuinely used by the availability algorithm (§26) the moment N11 exists, but zero-cost to leave unset for N1–N10. Including it now avoids a second migration touching every service row later. |
| `online_booking_enabled` | **REQUIRED NOW** | This is the exact gate the public catalog (§19) and the eventual public availability API (§27) both need; without it there is no way to have a service that's used internally but not self-bookable, which the source document explicitly calls out. |
| `status` (ACTIVE/ARCHIVED) | **REQUIRED NOW** | Lifecycle/archiving is mandatory the moment any service can be referenced by a booking (§14) — cannot be bolted on after the fact without a data-integrity gap. |
| `display_order` | **REQUIRED NOW** | Trivial integer column; catalog UI (§29) is unusable without deterministic ordering, and adding it later means an ugly one-time backfill by creation order. |
| `eligible_staff` | **DEFER to N8** | This is the staff-capability *relationship*, not a Service column — modeled as its own join table once staff profiles exist (§21), not a field on Service. |
| `compatible_add-ons` | **DEFER to N4** | Same reasoning — the `service_addons` join table (§10), not a Service column. |
| `source_template_id` | **OPTIONAL NOW, nullable** | Provenance-only (§6) — never read for authorization or live syncing, purely "which template did this start from" for future analytics/re-import tooling. |

**Fields the document does not mention but that should NOT exist**, stated explicitly per the instruction to flag `SHOULD NOT EXIST`:
- No `currency` column on the service row itself (§12) — currency is a tenant-level concern if/when multi-currency ever matters, not per-price.
- No `is_active` *and* `status` both — one lifecycle field only (§14).
- No `technician_id`/`room_id`/`table_id`-shaped nullable columns on Service — that is exactly the cross-vertical dumping-ground anti-pattern both this document and the earlier vertical-onboarding plan reject on sight.

---

## 5. Starter Template Architecture

**Decision: dedicated `service_templates` table (option C), seeded via migration INSERT — not Go constants (A), not JSON/config (D).**

Argued against each alternative:

- **A (static Go definitions):** rejected because it requires a binary redeploy to fix a typo in a starter description, and gives no path to per-tenant-locale template text without embedding locale logic in Go code. It also breaks with the codebase's own established precedent: `roles` and `permissions` — the closest existing analog to "controlled reference data the platform owns" — are migration-seeded rows, not Go constants. Templates should follow the same convention for the same reason: an admin/ops change to seed data shouldn't require a compiled release.
- **B (seed migration directly creating tenant rows):** meaningless — templates aren't owned by any tenant; there is no tenant to seed into at migration time.
- **D (config/JSON):** rejected for the same redeploy-coupling reason as A, plus it fragments "where does authoritative data live" across the database *and* a config file, when the database already does this job for roles/permissions.
- **C (dedicated table), chosen:** supports future admin tooling to edit templates without touching code, supports `business_type` scoping (reusing the existing `BusinessType` enum precedent from Vertical Onboarding F1 — no new enum invented), supports adding a `version`/`updated_at` column later without a schema redesign, and costs nothing more than any other seeded reference table already in this codebase.

**Minimum shape:** `service_templates(id, business_type, category_label, name, description, suggested_duration_minutes, suggested_price_minor, display_order, created_at)`. `business_type` reuses the F1 enum (`NAIL_TECHNICIAN`, etc.) — templates are already scoped per vertical by construction, so `GET /service-templates?business_type=NAIL_TECHNICIAN` (§18) is a straightforward `WHERE` clause, not a new indexing decision.

**Localization:** not solved now (no i18n exists anywhere in this codebase), but a DB-row-based design is the only one of the four options that doesn't foreclose it — a future `service_template_translations(template_id, locale, name, description)` table is a clean additive extension; a Go-constants or JSON-config design would need a parallel redesign to support the same thing.

**Future vertical reuse:** `service_templates` itself (id, business_type, category_label, name, description, duration, price, display_order) is entirely vertical-agnostic structurally — Restaurant/Hotel/Transport templates live in the *same* table, scoped by their own `business_type` value, no new table needed per vertical.

---

## 6. Tenant Catalog Model — Template → Tenant Service

**Decision: copy-on-select, with an optional nullable `source_template_id` for provenance only — never a live reference.**

| Approach | Tradeoff |
|---|---|
| Copy once, no reference kept | Simplest; but loses the ability to ever ask "how many tenants are still using the original Gel X template text" or offer a future "update from template" action. |
| Copy + keep `source_template_id` (chosen) | Same simplicity and same immutability guarantee (the FK is read-only provenance, never a join the booking/catalog read paths depend on), but leaves a clean door open for optional future tooling ("14 tenants selected this template — see how they customized it," or an opt-in "pull latest template text" action) without ever *forcing* a sync. |
| Live reference (tenant service always reads through to template) | Rejected outright — this is exactly what the source document explicitly forbids ("tenant must never mutate platform template data," and implicitly, template edits must never silently mutate tenant data either). A live reference makes both directions of that guarantee fragile. |

Selecting a template is a straight `INSERT INTO services (...) SELECT ... FROM service_templates WHERE id = $1` (or equivalent in Go) — the owner then edits the resulting *tenant* row exactly like any custom service (§7). No synchronization code exists or is ever needed.

---

## 7. Custom Services

No closed enum, ever — `services.name`/`description`/`duration_minutes`/`price_minor` are free-form owner input from day one, validated only for shape (non-empty name, positive duration/price), never against a fixed vocabulary. Template selection (§6) and custom creation are the *same* write path (`POST /services`) with different origins (one pre-filled from a template, one blank) — not two different code paths.

**Owner capabilities:** create, edit, deactivate (`status = ARCHIVED`), reorder (`display_order`), delete.

**Deletion with historical bookings (§14, restated here for the service-level decision):** a service referenced by any historical booking must never hard-delete. `DELETE /services/{id}` means **archive** (`status = ARCHIVED`) whenever a booking references it, full stop — no conditional branch needed at the service layer beyond "does at least one booking reference this row," because archiving is always safe and hard-delete is only ever safe when that count is zero. Recommendation: **always archive, never hard-delete**, even when no booking exists yet — this removes the need for a conditional check entirely, and an unused archived row costs nothing. A genuinely empty catalog cleanup (hard delete of a never-booked, never-published service) can be a deliberate, separate, rarely-used operation if ever needed — not the default `DELETE` behavior.

---

## 8. Category Model

**Decision: tenant-owned rows only.** No separate "template category" entity.

Reasoning: the document's own five example categories (Natural Nails, Extensions, Pedicures, Add-Ons, Packages) are not a closed set — tenants must be able to create custom categories (explicit requirement) — so a rigid platform-controlled category enum would immediately need an escape hatch anyway. Instead, `service_templates.category_label` (a plain string, §5) is used at template-selection time to find-or-create the matching tenant `categories` row (`WHERE tenant_id = $1 AND name = $2`, insert if absent) — the five starter categories become real tenant rows the first time a tenant imports any template from that category, not a parallel controlled taxonomy.

**Shape:** `categories(id, tenant_id, name, display_order, status)`.
- **Ordering:** `display_order`, same convention as Service.
- **Active status:** yes — a category can be archived (hides it and, by extension, everything display-grouped under it from the public catalog) without deleting the services inside it; services keep their `category_id` even if the category is archived, only the *public listing* of the category itself is affected.
- **Deletion:** never hard-delete a category with services still assigned to it (FK constraint, `ON DELETE RESTRICT`) — force an explicit re-assignment or archive-first flow. A category with zero services can be hard-deleted safely.
- **Public display:** only ACTIVE categories, and only alongside at least one publicly-visible service under them (empty-category display is a UI nicety decision for later, not a backend concern).

---

## 9. Service vs. Add-on

**Decision: Add-On as its own entity (Option A), related to Service via a compatibility join (`service_addons`, §10) — not a single polymorphic catalog table (Option B), and not folded into Service via a type flag.**

Why not a shared `services` table with an `is_addon` boolean (the "cheap" option that avoids a second table): rejected because Service and Add-on diverge in more than shape — `online_booking_enabled`, `eligible_staff`, buffers, and category assignment all have different or absent meaning for an add-on (an add-on is never independently bookable, so "online booking enabled" is meaningless for it; it doesn't need its own staff-eligibility relationship distinct from whatever service it's attached to). Cramming both into one table with a discriminator either forces nullable columns that mean nothing for one of the two kinds (the generic-dumping-ground smell) or forces application code to constantly branch on the discriminator to know which columns are meaningful. A second, smaller table is cheaper long-term than that ongoing cognitive tax.

Why not full polymorphism (Option B, folding Package in too): rejected explicitly per the document's own instruction — Package is structurally different enough (no inherent price/duration of its own, defined entirely by its components) that lumping it in triples the nullable-column problem above.

**Add-on shape:** `addons(id, tenant_id, name, description, price_delta_minor, duration_delta_minutes, status, display_order)`. Note `price_delta_minor`/`duration_delta_minutes`, not `price_minor`/`duration_minutes` — an add-on's numbers are deltas applied on top of a host service's base numbers (§31's pricing composition), not standalone values.

---

## 10. Add-On Compatibility

**Decision: `service_addons(service_id, addon_id)`, a plain many-to-many join — no per-pair price/duration override yet.**

- **Tenant-wide add-ons:** an add-on with zero rows in `service_addons` is meaningless (never compatible with anything) — there is no separate "tenant-wide" flag needed; if a tenant wants an add-on available on every service, that's simply a row per service (or, if that proves tedious in practice once real usage is observed, a future `service_addons` row with `service_id = NULL` meaning "all services" — deferred until that friction is actually reported, not designed speculatively now).
- **Per-service price/duration override:** deferred, per the document's own explicit default recommendation. `addons.price_delta_minor`/`duration_delta_minutes` apply uniformly regardless of which compatible service they're attached to. Add an override column to `service_addons` itself (`price_delta_minor_override NULL`) only when a real tenant need is reported — the join table is exactly where such an override would live later, so deferring costs nothing structurally now.

---

## 11. Packages

**Decision: `packages` (own table) + `package_items` (component join, narrowly polymorphic by necessity) — no recursion.**

`packages(id, tenant_id, category_id, name, description, price_minor, duration_minutes, status, display_order, online_booking_enabled)` — `price_minor`/`duration_minutes` here are **explicit, always-set values**, not "sum of components unless overridden." Reasoning: a package's selling price is a deliberate business decision (a bundle discount), not a derived value that happens to equal the sum — treating it as always-explicit avoids a whole class of "is this the override or the computed total" ambiguity bugs. The owner-facing UI (§29) can *suggest* the summed total as a starting point when creating a package, but the stored value is final once saved.

`package_items(id, package_id, item_type, item_id, quantity DEFAULT 1)` — `item_type` is `SERVICE` or `ADDON` only. This is the one narrow, justified use of a polymorphic reference in this whole plan: a package genuinely needs to reference two structurally different tables, and there is no way to express that with a single FK. **Constraint honestly stated, not glossed over:** a plain FK cannot enforce "item_id must exist in whichever table item_type names" across two different target tables — Postgres has no native polymorphic FK. This is enforced at the **service layer**, not the database, and is recorded as a real, accepted limitation in §44, not silently assumed away.

**No recursion:** `item_type` has exactly two allowed values, `SERVICE` and `ADDON` — `PACKAGE` is never a valid `item_type`. This is a `CHECK` constraint (`item_type IN ('SERVICE', 'ADDON')`), enforced at the database level, unlike the FK-target problem above. A package can never contain another package, full stop, by construction.

---

## 12. Money

**No existing convention — this plan sets one: integer minor units, stored as a Go `int64`/Postgres `BIGINT`.**

Rejected: floating point (the document's own instruction, and the standard reason applies — cumulative rounding error across price + delta + delta + package-discount arithmetic is a real, observed class of bug industry-wide, not a theoretical concern).

- `services.price_minor`, `addons.price_delta_minor`, `packages.price_minor` — all `BIGINT`, representing the smallest unit of the tenant's operating currency (kobo, given the codebase's existing Nigerian phone-number test fixtures — `+234...` — strongly implying NGN is the only currency in scope today).
- **Deposit:** deferred (§4) — no column, no representation decided, until a payment feature is actually planned.
- **Currency column:** **not added now**, anywhere. No price row gets a `currency` column. Currency is implicitly the tenant's single operating currency. **Later, if/when multi-currency genuinely matters:** add one column, `tenants.currency` (ISO 4217, default `'NGN'`), not a currency column repeated on every price row — a tenant operates in one currency; per-price currency would only matter for a multi-currency-per-tenant model nothing in this document or the broader product direction asks for.

---

## 13. Duration

**Decision: integer minutes (`INT`/`int` in Go), for `duration_minutes`, `buffer_before_minutes`, `buffer_after_minutes`, and every add-on/package duration-delta field.**

Rejected: persisting `time.Duration` (a Go-specific nanosecond `int64`) directly — this is explicitly warned against by the prompt and for good reason: it's not naturally JSON-representable in a way a frontend or a future mobile client can use without a conversion layer, and it invites unit-confusion bugs (nanoseconds vs. minutes) at every boundary. Integer minutes is simple, matches how every duration in the source document is expressed ("90 min," "+30 min"), and requires zero conversion at the API boundary — the wire representation *is* the storage representation *is* the domain representation.

**API representation:** every duration field serializes as a plain JSON integer (minutes), e.g. `"duration_minutes": 90`. No ISO-8601 duration strings, no nested `{hours, minutes}` objects — one number, one unit, documented once.

---

## 14. Status / Archiving

**Decision: exactly two states, `ACTIVE` / `ARCHIVED`, on every catalog table (`services`, `addons`, `packages`, `categories`) — no additional state machine.**

Matches the existing `tenants.status` (`ACTIVE`/`DISABLED`) and `tenant_memberships.status` (`ACTIVE`/`DISABLED`) precedent in shape (a `VARCHAR` + `CHECK` constraint), though the *values* differ (`ARCHIVED` rather than `DISABLED`) because "archived" better communicates "retained for history, not merely suspended" — a disabled tenant can be re-enabled with its meaning unchanged; an archived service is conceptually retired, even though re-activation remains a simple state flip if ever needed.

**DELETE contract, stated once and applied everywhere in this plan:** `DELETE` on any catalog resource always means archive (`status = ARCHIVED`), never a row removal, regardless of whether a booking currently references it. This removes the need for conditional "is it referenced" branching (§7) and gives one simple, safe, universally-applicable rule instead of a per-resource special case.

---

## 15. Online Booking Flag

**Decision: `online_booking_enabled BOOLEAN NOT NULL DEFAULT true` on `services` and `packages`** (not needed on `addons`, which are never independently listed or booked — their visibility is entirely derived from their host service's flag).

**Public catalog visibility formula** (mirrors F3's own `ACTIVE AND COMPLETED` visibility formula in shape, extended one level down):

```
publicly bookable service  ⟺  tenant.Status == ACTIVE
                             AND tenant.OnboardingStatus == COMPLETED   (F3, unchanged)
                             AND service.Status == ACTIVE
                             AND service.OnlineBookingEnabled == true
```

This is the exact query shape §19's public endpoint implements — no new tenant-level gate, just one more `AND` clause layered on top of the existing, already-correct F3 gate.

---

## 16. Permission Model

**Current catalog inspected: 13 permissions, all `user.*`/`tenant.*`/`role.*`/`permission.*`. Nothing service-shaped exists.**

**Recommendation (not implemented, no migration): a new `service.*` family, added when N3 actually ships, not now:**

| Code | `BUSINESS_OWNER` | `STAFF` |
|---|---|---|
| `service.read` | ✅ | ✅ (matches STAFF's existing read-only pattern on `tenant.read`/`user.read`) |
| `service.create` | ✅ | ❌ |
| `service.update` | ✅ | ❌ |
| `service.delete` | ✅ | ❌ |

Reasoning for STAFF getting `service.read` but nothing else: STAFF already holds exactly this shape of access elsewhere (read tenant/user/role/permission data, no mutation rights) — extending the same pattern to the catalog is consistent, not a new policy decision. Whether STAFF should ever manage the catalog is a product question explicitly out of scope here; the *default*, minimal-privilege answer is no.

`categories.*`, `addons.*`, `packages.*` permissions: **not recommended as separate families.** A tenant member who can manage the service catalog can reasonably manage its categories/add-ons/packages too — introducing four parallel permission families for what is functionally one management surface would be exactly the kind of speculative catalog-of-permissions the source document's sibling planning pass (Epic 2) explicitly warned against ("do not invent role checks... do not add them automatically"). `service.*` governs the whole catalog surface (services, categories, add-ons, packages) until a real product need for finer separation is demonstrated.

---

## 17. Private API Plan

The document's suggested REST shape is directly consistent with the existing `/api/v1/tenants/{tenantID}/...` convention (already used by profile, onboarding, members, role-assignments) — **adopted as proposed**, no reason to deviate:

```
GET    /api/v1/tenants/{tenantID}/services
POST   /api/v1/tenants/{tenantID}/services
GET    /api/v1/tenants/{tenantID}/services/{serviceID}
PATCH  /api/v1/tenants/{tenantID}/services/{serviceID}
DELETE /api/v1/tenants/{tenantID}/services/{serviceID}   (archives, per §14)

GET    /api/v1/tenants/{tenantID}/categories
POST   /api/v1/tenants/{tenantID}/categories
PATCH  /api/v1/tenants/{tenantID}/categories/{categoryID}
DELETE /api/v1/tenants/{tenantID}/categories/{categoryID}

GET    /api/v1/tenants/{tenantID}/addons
POST   /api/v1/tenants/{tenantID}/addons
PATCH  /api/v1/tenants/{tenantID}/addons/{addonID}
DELETE /api/v1/tenants/{tenantID}/addons/{addonID}
PUT    /api/v1/tenants/{tenantID}/services/{serviceID}/addons   (replace the compatible-addon set — body: addon_id[])

GET    /api/v1/tenants/{tenantID}/packages
POST   /api/v1/tenants/{tenantID}/packages
PATCH  /api/v1/tenants/{tenantID}/packages/{packageID}
DELETE /api/v1/tenants/{tenantID}/packages/{packageID}
```

**Why not query-param consolidation** (e.g. `GET /services?include=addons,packages`): no `?include=` pattern exists anywhere in this codebase today; introducing one here would be a new, unprecedented convention layered onto a single feature rather than an established idiom. One-endpoint-per-resource-type matches the existing membership/role-assignment precedent exactly. Endpoint count is larger but each is trivial and consistent — not "explosion," just the established shape repeated four times.

**Middleware chain** for every route above: `Authentication → Tenant Context → TenantPermissionMiddleware{"service.read"|"service.create"|...} → Handler` — identical structure to every existing tenant-scoped route, no new middleware primitive needed.

---

## 18. Starter Catalog API

**Decision: authenticated, not public; not tenant-specific.**

```
GET /api/v1/service-templates?business_type=NAIL_TECHNICIAN
```

- **Not public:** template suggested pricing/descriptions are platform IP presented as a business-configuration tool, not customer-facing content — no reason to expose it to anonymous callers, and doing so would put it on the same trust boundary as the public catalog (§19) for no benefit.
- **Not tenant-specific:** templates aren't owned by any tenant (§5) — the query is scoped by `business_type` only, requiring just authentication (any authenticated user can browse what templates exist for a vertical), not tenant context or `tenant.update`-style permission. This mirrors `GET /api/v1/tenants` (Feature 3) in shape: authenticated, no tenant-context middleware, because there's no specific tenant to check membership against.

---

## 19. Public Service Catalog API

```
GET /api/v1/public/tenants/{slug}/services
```

**Contract**, matching the document's suggested field list, filtered through the "public candidates" list in §16 of the source doc and this plan's own §15 formula:

```json
{
  "categories": [
    {
      "name": "Natural Nails",
      "display_order": 1,
      "services": [
        {
          "id": "...",
          "name": "Gel X Full Set",
          "description": "...",
          "duration_minutes": 90,
          "price_minor": 2500000,
          "addons": [
            { "id": "...", "name": "Chrome Powder", "price_delta_minor": 400000, "duration_delta_minutes": 15 }
          ]
        }
      ]
    }
  ],
  "packages": [
    { "id": "...", "name": "NailSavvy Signature", "description": "...", "price_minor": ..., "duration_minutes": ... }
  ]
}
```

**Excluded, explicitly:** internal notes, inactive/archived items, `source_template_id`, `display_order` on individual items beyond what's needed for sort order, any staff/operational data. Every row returned is already filtered server-side by the §15 formula — the response contains nothing the client needs to further filter for visibility, matching F3's own "never let the client filter what should already be hidden" precedent.

**Packages share this endpoint** (a top-level `packages` array alongside `categories`), rather than a separate `GET .../packages` public route: a customer browsing a tenant's public page reasonably expects one page load to show everything bookable, and packages are a small, bounded list (not paginated catalog data) — no technical reason to split the request. Categories/services/addons are nested in one response for the same reason: the public browse experience (§28) needs the whole tree to render a category-grouped list without N+1 requests.

---

## 20. Staff / Technician Domain

**Decision: Option B — a dedicated staff profile, separate from (but 1:1-linked to) `tenant_memberships`, not "STAFF role alone" (Option A).**

Option A is definitively insufficient: neither `users` nor `tenant_memberships` carries a display name, bio, avatar, specialties, or bookable flag — there is *nothing* to attach that data to today except a new table. This isn't a judgment call, it's a gap already confirmed by inspection (§2).

**Shape:** `staff_profiles(id, tenant_id, user_id, display_name, bio, avatar_url, bookable BOOLEAN, status, created_at, updated_at)`, unique on `(tenant_id, user_id)` — one profile per membership, mirroring `tenant_memberships`' own uniqueness shape. `display_name` is intentionally separate from anything on `users` (which has no name field, and shouldn't gain one just for this — a user's platform identity and a tenant's presentation of that person as "Ada, Senior Nail Technician" are different concerns, and a user could plausibly want a different display name at different tenants if this platform ever supports one user working across tenants).

**Not every STAFF-role member is necessarily a bookable technician**, and not every bookable technician is necessarily assigned the `STAFF` role (a `BUSINESS_OWNER` might also take bookings personally) — `staff_profiles` is deliberately decoupled from the RBAC role itself; it answers "is this person a technician customers can book," not "what can this person administratively do." A row can exist for a `BUSINESS_OWNER` membership too.

**Cross-vertical reuse (§37):** this table's shape (display identity + bookable flag, tenant-scoped) is entirely vertical-agnostic — a restaurant's "which staff member takes reservations" or a transport company's "which driver" could reuse the identical table. Recommended module home: **`internal/staff`**, not `internal/nail` — see §32 for the full module-boundary rationale.

---

## 21. Staff ↔ Service Capability

**Decision: `staff_service_capabilities(staff_profile_id, service_id, status)`, a plain many-to-many join — defer per-staff price/duration overrides, per the document's own default recommendation.**

- **Activation/deactivation:** the join row's own `status` (`ACTIVE`/inactive), rather than deleting the row — preserves "this technician used to be qualified for this" as a soft fact rather than losing it, at negligible cost, and avoids re-litigating the same archive-vs-delete question already settled in §14 for a third time; consistent handling everywhere.
- **Overrides:** deferred. No `price_override_minor`/`duration_override_minutes` columns now. If a real need emerges (a senior technician charging more for the same service), the join table is exactly where those columns would land later — no structural change needed to add them, only new nullable columns.
- **"Any available technician":** modeled at the *query* level, not the data model — a booking request with no staff preference simply queries `staff_service_capabilities` for any `ACTIVE` row matching the requested service, then intersects with availability (§26). No `staff_profile_id = NULL` sentinel needed on the join table itself.

**This table is the one piece of this plan that is genuinely Nail-flavored** (or more precisely, appointment-vertical-flavored) rather than universally shared — a restaurant doesn't have "which staff member can make which dish" in the same booking-relevant sense. Kept in a Nail/appointment-specific location conceptually, even if the underlying module naming stays generic (§32).

---

## 22. Staff Public Visibility

**Not implemented now — data model implications only, per instruction.**

Fields that will eventually be needed on `staff_profiles` (already covered by the shape in §20, no new columns required today): `bookable` (already proposed) doubles as "hide certain staff publicly" when `false`. The *policy* — must customers select a technician, may they, or is it always "any available" — is a **tenant-level setting**, not a staff-level one, and doesn't exist yet; it would live wherever tenant booking-rule settings eventually live (not designed in this pass — flagged as a genuine future need, not silently deferred without a home). No schema decision is made for that policy field now; `staff_profiles.bookable` alone is sufficient for the current planning horizon.

---

## 23. Business Hours

**Decision: `tenant_business_hours(id, tenant_id, weekday SMALLINT, opens_at TIME, closes_at TIME)`, with no uniqueness constraint on `(tenant_id, weekday)` — multiple rows per weekday are allowed natively, supporting split hours from day one at zero extra schema cost.**

Rejected: a single open/close pair per weekday (simpler, but the document explicitly flags "possible split hours eventually," and removing a uniqueness constraint later than adding one is the more disruptive direction to defer). Allowing multiple rows per weekday from the start costs nothing extra in the schema itself — it's the *absence* of a constraint, not additional complexity — so there's no reason to build the more restrictive version first.

**Timezone:** not stored per business-hours row — inherited from `tenants.timezone` (already exists, already required by F6). One tenant, one operating timezone, one place it's recorded — no duplication.

---

## 24. Staff Working Hours

**Decision: `staff_working_hours(id, staff_profile_id, weekday SMALLINT, starts_at TIME, ends_at TIME)`** — identical shape to business hours, scoped to a staff profile instead of a tenant.

- **Recurring weekly schedule:** yes, this is the whole model — a fixed weekly pattern.
- **Effective dates:** **deferred** — no concrete requirement drives "this schedule changes starting next month" yet, and adding effective-dating later is an additive column (`effective_from`), not a redesign.
- **Timezone:** inherited from the tenant's, same reasoning as §23 — an individual technician's personal timezone is not a concept this product needs (they work at the business's location, in the business's timezone).
- **Booking never mixes in here:** working hours describe *capacity*, existing bookings are consumed separately by the availability algorithm (§26) — kept as two distinct concerns per the explicit instruction not to blend them.

---

## 25. Time Off / Exceptions

**Decision: two separate tables — tenant-level and staff-level are genuinely different scopes and shouldn't share a model just because they're structurally similar.**

- `tenant_closures(id, tenant_id, starts_on DATE, ends_on DATE, reason TEXT NULL)` — whole-business closure (e.g. a public holiday, a shop renovation).
- `staff_time_off(id, staff_profile_id, starts_on DATE, ends_on DATE, reason TEXT NULL)` — one technician's vacation/sick leave/blocked time.

**Scope kept deliberately small for the first availability pass:** whole-day granularity only (`DATE`, not `TIMESTAMP` ranges) — a partial-day exception ("blocked 2–4pm only") is a real future need but not required for a correct first availability engine, and modeling it now would mean designing time-range overlap logic twice (once here, once for the booking-concurrency exclusion in §32) before either is proven necessary. Deferred, not forgotten — flagged in §44.

**Special/extended opening hours** (the inverse of a closure — "we're open later for a holiday promotion"): not modeled now. `tenant_closures` only expresses "closed," not "hours different from normal." Revisit if a real product need surfaces; speculative now.

---

## 26. Availability Architecture

**Decision: computed on request, not materialized or cached — the simplest correct architecture, matching the explicit instruction to avoid premature distributed-scheduling complexity.**

**Conceptual algorithm** (a dedicated service, `internal/availability`, never embedded in Service CRUD — per the source document's own explicit instruction):

```
bookable slots =
    tenant business hours (§23)
    ∩ staff working hours (§24), for staff qualified via staff_service_capabilities (§21)
    − tenant closures (§25)
    − staff time off (§25)
    − existing bookings (§28) [+ buffers, §4]
    , sliced by requested service's duration_minutes (+ any addon duration deltas)
```

**Why on-request, not materialized:** no evidence anywhere in this repository suggests the read volume that would justify a cache-invalidation problem (business hours change, staff schedule changes, a booking is created — each would need to invalidate materialized slots correctly, which is a real source of subtle bugs if built before it's needed). This monolith has one Postgres instance and modest expected load for a first vertical; a live query is easy to reason about and easy to keep correct. Revisit only if a production load profile actually demonstrates the live-computation path is too slow — not before.

**Hybrid note:** nothing here forecloses adding a cache layer later (e.g. a short-TTL cache keyed by tenant+service+date once real traffic patterns are known) — that's a pure performance optimization on top of an already-correct on-request algorithm, not a redesign.

---

## 27. Availability API

```
GET /api/v1/public/tenants/{slug}/availability?service_id=...&addon_ids=...&staff_profile_id=...&date=...
```

- **Service duration:** drives slot length; `addon_ids` (repeatable query param) adds their duration deltas on top (§9's composition model).
- **Technician selection:** `staff_profile_id` optional — omitted means "any qualified technician," computed per §26/§21.
- **Tenant timezone:** the `date` param is interpreted in the tenant's own timezone (`tenants.timezone`); returned slot times are timezone-aware (ISO 8601 with offset, or explicit UTC + the tenant's IANA name for client-side conversion — exact wire format is an implementation-time decision, not a planning-time one).
- **Slot granularity:** a fixed interval (e.g. 15 minutes) is the natural default for slot *start-time* candidates, independent of each service's own duration — the doc's own example durations (90, 30, 15 min) aren't all multiples of a single "slot size," so granularity governs candidate start times, not a rigid grid every service must fit.

**Plan only — no route wired, no handler written.**

---

## 28. Booking Aggregate

**Not modeled as a table now (per instruction) — the identifiers/snapshots a future Booking must carry, reasoned from §31's requirement:**

- `tenant_id` — isolation boundary, non-negotiable, same as every other table in this plan.
- `customer_id` — see §29.
- Exactly one of `service_id` or `package_id` set, never both, never neither (a `CHECK` constraint expressing this, or two nullable FKs with an application-level XOR check — exact mechanism is implementation-time).
- Selected add-on ids — a `booking_addons(booking_id, addon_id)` join, mirroring `service_addons`' shape.
- `staff_profile_id`, nullable (an "any qualified technician" request is resolved to a *specific* technician at booking-creation time and stored — the booking itself is never ambiguous once created, even though the *request* that created it may have been "any available").
- `starts_at`/`ends_at` — computed from the price/duration snapshot below, timestamptz.
- `price_snapshot_minor`, `duration_snapshot_minutes` — see §31, mandatory.
- `status` — see §30.
- `created_at`/`updated_at`.

Not designed further — table shape, indexes, and constraints are N14's job, not this plan's.

---

## 29. Customer Model

**No Customer domain exists today anywhere in the repository — confirmed by the same keyword sweep as §2.**

**Recommendation: a minimal tenant-scoped `customers` table from the start, not guest fields embedded directly on `bookings`.**

- **Guest-only for the first booking milestone** (no authentication built for customers, per instruction) — but even a guest gets a `customers` row, upserted by contact info within a tenant (`WHERE tenant_id = $1 AND (email = $2 OR phone = $2)`) at booking time, rather than duplicating name/email/phone as free columns on every booking row.
- **Why a table, not embedded fields:** gives a stable identity for "this customer's booking history" (a real, likely-near-term product need — a tenant will want to see repeat customers) without requiring authentication now, and leaves a clean, additive path to a real platform account later: add a nullable `user_id` to `customers` when platform customer accounts become real, with zero change to `bookings`, which only ever references `customer_id`.
- **Both future options (§29 of the prompt) are served** by this one shape: guest booking is "customer row with no `user_id`," a platform account is "customer row with a `user_id`" — not two different booking code paths, one model that grows into the second case additively.

---

## 30. Booking Status

**Decision: exactly the document's proposed five states — `PENDING`, `CONFIRMED`, `CANCELLED`, `COMPLETED`, `NO_SHOW`. No additions, no removals.**

No justification exists yet for more (e.g. `RESCHEDULED`) — a reschedule is better modeled later as "cancel + create a new booking with a link back to the original" (an `original_booking_id` nullable FK) than as a sixth status, since the *time* of a booking is fundamental to what it means, and "rescheduled" describes a transition between two bookings, not a state of one. Not designed further now — flagged as the natural extension point if/when reschedule support is planned.

Deposit/payment confirmation is explicitly kept out of booking *semantics* for this pass — `status` describes the appointment lifecycle, not payment state; if/when payments are built, that's an orthogonal `payment_status` concern, not a reason to add `AWAITING_DEPOSIT` etc. to this enum.

---

## 31. Price/Duration Snapshots

**Mandatory, reasoned in full since the prompt requires it:**

If `Gel X` is ₦25,000 today and an owner changes it to ₦30,000 next month, every booking created *before* that change must still show ₦25,000 — a booking is a historical commercial record, not a live view onto the current service row.

**What Booking must snapshot, not merely reference:**
- `price_snapshot_minor` — the *total* price at booking time (base service/package price + every selected add-on's price delta, summed once and frozen).
- `duration_snapshot_minutes` — same reasoning, the total duration used to compute `ends_at`.
- **Recommended, secondary:** a `service_name_snapshot` (and `addon_name_snapshots`) text field(s) too — not just the numbers. If "Gel X" is later renamed to "Gel Extension," a customer's confirmation email and a tenant's historical booking list should still show what was actually booked at the time, not the current name. This is a smaller, softer requirement than the price/duration snapshot (a renamed service doesn't create the same financial-correctness risk a repriced one does), but the same principle applies and it's cheap to include from the start.

**Why snapshot, never "join back to the live Service row" for historical display:** a live join is exactly the bug the document describes — it would silently rewrite history every time a price changes. Snapshotting at creation time is the only approach that gets this right, and it's a well-understood, standard pattern (an "order line" pattern, effectively) — not a novel design.

---

## 32. Concurrency / Double-Booking Prevention

**The critical architecture risk, addressed explicitly (not implemented):**

**Target design:** a Postgres `EXCLUDE` constraint using the `btree_gist` extension, on `(staff_profile_id, tsrange(starts_at, ends_at)) WHERE status NOT IN ('CANCELLED')` — this makes two overlapping bookings for the same technician **impossible at the database level**, immune to any application-logic race condition, regardless of how many application-server instances or concurrent requests exist. This is the gold-standard approach for exactly this problem in Postgres and is the recommended target for N14.

**Why not rely on application logic alone:** a "check availability, then insert" pattern always has a race window between the check and the write, no matter how careful the application code is — two simultaneous requests can both pass the check before either writes. An exclusion constraint closes that window at the only layer that can actually guarantee it: the database's own transaction/locking machinery.

**Transaction boundary:** booking creation must (1) re-validate availability server-side at write time — never trust a client-supplied slot from an earlier `GET /availability` response as authoritative, since time has passed and other bookings may have landed — and (2) attempt the insert inside a transaction that relies on the exclusion constraint as the final backstop, catching the constraint-violation error and mapping it to a `SLOT_UNAVAILABLE`-shaped response (§34) rather than a generic 500.

**Isolation level:** Postgres's default `READ COMMITTED` is sufficient *given* the exclusion constraint does the real work — the constraint, not the isolation level, is what prevents the double-booking; the transaction just needs to be atomic (single INSERT, or INSERT + related joins in one transaction, matching the existing `tenantService.Create` atomicity precedent from Feature 2).

**Prerequisite flagged honestly:** the `btree_gist` extension must be enabled (`CREATE EXTENSION IF NOT EXISTS btree_gist`) before the exclusion constraint's migration can run — a one-line addition to N14's migration, not a blocker to this plan, but explicitly noted so it isn't discovered as a surprise at implementation time.

---

## 33. Service Deletion With Bookings

Restated from §7/§14 as the dedicated cross-cutting answer: **archive, never destroy**, universally, regardless of whether bookings exist — this plan does not special-case "has bookings" vs. "has none," because always-archive is simpler and equally safe in both cases (§14's reasoning). Query behavior: every public catalog query (§19) and every owner-facing "current catalog" list filters `WHERE status = 'ACTIVE'`; a booking's historical display uses its own price/duration/name **snapshot** (§31), never a live join to the service row — so an archived (or even a since-recreated-with-the-same-name) service can never corrupt historical booking display.

---

## 34. Business Owner Frontend

**Plan only — no pages built.** Modules, matching the source document's own grouping, plugged into the existing `DashboardShell`/`dashboardNavItems` config pattern (§29 of the earlier vertical-onboarding plan, unchanged and directly reused here — no new shell needed):

```
Services   → Catalog, Categories, Add-Ons, Packages
Team       → Technicians, Capabilities
Availability → Business hours, Staff hours, Time off
Bookings   → Calendar, Upcoming, History
Customers
Business   → Profile, Branding, Booking page (already exists from F6)
Settings
```

**Implementation order recommendation:** Services → Team → Availability → Bookings, matching the backend dependency order (§37/§38) — a dashboard module can't meaningfully exist before its backend API does, and the backend order is itself driven by real dependencies (capability needs staff *and* services to exist first; availability needs capability; bookings need availability).

---

## 35. Vertical Onboarding Extension

**Future flow, not built now:**

```
[existing F1-F6 common onboarding, unchanged]
→ Choose starter services (N2/N6)
→ configure price/duration (N6)
→ configure add-ons/packages (N4/N5/N6)
→ add staff/technicians (N7)
→ assign service capabilities (N8)
→ set working hours (N9)
→ [booking rules — not designed in this pass]
→ ready for bookings
```

**Where this begins relative to existing onboarding completion (the major decision the prompt flags):**

The common onboarding flow (F1–F6) already ends at `onboarding_status = COMPLETED`, which already means "the business profile is real" and already, today, makes the tenant's *profile* page publicly resolvable (F3). **This plan does not move, rename, or gate that transition differently.** The Nail-specific setup flow above begins **after** common `onboarding_status = COMPLETED` — it is not a blocker to it, and it is not folded into the same `onboarding_status`/`onboarding_step` state machine at all. It is a separate, later, optional-to-start-immediately flow that a `BUSINESS_OWNER` is *guided toward* (e.g., a dashboard prompt: "add your first service to start taking bookings") once they land in the (now-existing) dashboard, not a gate that blocks reaching the dashboard.

This resolves the prompt's explicit A-vs-B framing: **A is correct for what it already governs** (public profile), **B is correct for a different, new, not-yet-built gate** (booking-readiness, §36) — they are not in conflict once separated, and the separation is the whole point of this section's recommendation.

---

## 36. Public Profile vs. Booking Readiness Decision

**Recommendation: keep them separate, and — the more specific, load-bearing recommendation — do not store "booking readiness" as a stat flag on `tenants` at all. Compute it live, the same way F3 already computes public-profile visibility live.**

Rather than a new `tenants.booking_setup_status` or `tenants.vertical_setup_status` column (which the prompt explicitly says not to add yet, and which this plan agrees would be premature schema for a concept with no consumer yet), the eventual "is this tenant bookable" question is answered by a query, not a stored flag: *"does this tenant have at least one `ACTIVE`, `online_booking_enabled` service (or package) with at least one `ACTIVE` capable, working-hours-configured staff member?"* — computed at the moment the public availability/booking API (§27, N13+) needs the answer, exactly mirroring how `PublicTenantService.GetBySlug` already computes `ACTIVE AND COMPLETED` live rather than caching a `is_publicly_visible` boolean anywhere.

**Why live-computed over stored, argued once so it isn't re-litigated per table:** every "is X ready" gate this codebase has built so far (§14's archiving, F3's public visibility) is a derived read, never a redundant stored flag that could drift from the facts it summarizes. A stored `booking_ready` boolean would need to be kept in sync on service creation, service archiving, staff capability changes, and working-hours changes — four write paths that would all need to remember to update one shared flag, a real source of drift bugs. A live query has no drift risk by construction. If a future performance need justifies caching this specific check, that's an additive optimization on top of a correct live query — not a reason to build the stored version first.

**Net effect:** `onboarding_status` keeps meaning exactly what it means today, forever, as far as this plan is concerned. "Booking ready" is a new, nameless-for-now, query-level concept that doesn't need a column until N13 actually needs to ask the question.

---

## 37. Cross-Vertical Reuse Boundary

| Concept | Verdict | Reasoning |
|---|---|---|
| Money (`price_minor` shape, minor-units convention) | **Shared** | Currency handling doesn't vary by vertical. |
| Catalog display concepts (category, display_order, status/archive pattern) | **Shared shape**, not shared tables | Restaurant/Hotel/Transport will each want *their own* categories/items table (their own resource shape is too different to share rows), but the same archive-not-delete, display_order, ACTIVE/ARCHIVED pattern applies uniformly — a convention, not a table. |
| Customers | **Shared** | A customer booking a nail appointment and a customer booking a hotel room are the same kind of entity; `customers` as designed (§29) has nothing Nail-specific in it. |
| Booking identity/status vocabulary | **Shared concept, not necessarily shared table** | `PENDING/CONFIRMED/CANCELLED/COMPLETED/NO_SHOW` and the general "a booking has a customer, a price snapshot, a duration snapshot, a status" shape travel well; the *referenced resource* (service vs. room vs. trip) differs enough per vertical (per the earlier Epic 02 plan's own §16 analysis) that one shared `bookings` table with nullable per-vertical FKs is still the rejected anti-pattern. Each vertical likely gets its own bookings table sharing the same *conceptual* shape. |
| Time/timezone handling | **Shared** | Already shared today (`tenants.timezone`), nothing Nail-specific about it. |
| Business hours | **Shared shape** | The `tenant_business_hours` table itself is entirely vertical-agnostic and could genuinely be the *same table* for every vertical (a restaurant's opening hours are the identical concept), not just a similar pattern. |
| Staff identity (`staff_profiles`) | **Shared table**, likely genuinely reusable as-is | Display name/bio/avatar/bookable is vertical-agnostic; a restaurant's "which staff take reservations" fits the identical shape. |
| Staff-service capability (§21) | **Nail/appointment-specific** | A restaurant's staff-to-resource relationship (if any) is a different question (which server owns which section, not "which dishes can they cook") — not assumed reusable. |
| Add-ons/packages as modeled here | **Nail-specific in the details, generic in the pattern** | "A base bookable item plus optional deltas plus bundles" as a *pattern* likely recurs (a hotel package bundling room + amenities); the specific `service_addons`/`package_items` tables as built here are scoped to Nail's `services` table and wouldn't automatically extend to a hotel's `rooms` table without their own equivalent join tables. |

**Conservative stance maintained throughout:** nothing in this plan assumes a shared table beyond `customers` and `tenant_business_hours` (and, with lower confidence, `staff_profiles`) — everything else is "same pattern, separate tables per vertical," matching the Epic 02 plan's own explicit rejection of forcing verticals into one generic resource shape.

---

## 38. TDD Strategy

**Backend**, per proposed feature (§39), each following the exact layered pattern already established by F1–F3 of the tenant-onboarding work:

- **Domain/model:** validation functions (service name/duration/price bounds, add-on delta sign rules, package `item_type` allow-list) — pure unit tests, no DB, mirroring `ValidateBusinessType`/`ValidateOnboardingStep`'s existing test shape.
- **Repository/integration:** real-Postgres round-trip tests against the disposable Docker DB (established convention, `docker-compose.test.yml`), covering scan-safety for every nullable field, tenant-scoping (`WHERE tenant_id`) on every query, and the archive-not-delete contract.
- **Service:** business-rule tests with fakes (compatibility validation, price/duration composition arithmetic, capability checks) — mirroring `OnboardingService`'s existing fake-repository test pattern.
- **Handler:** decode-target protection tests (client cannot set `tenant_id`, `source_template_id`, or another tenant's `category_id` through the request body) — mirroring the existing `TestCreateTenantHandlerUsesAuthenticatedCreatorNeverRequestBody`-style tests throughout this codebase.
- **Route/app:** full real-middleware-chain tests (`internal/app/*_route_test.go` pattern) — every private route proven to require authentication, tenant context, and the correct permission; every public route proven to require *none* of those and to hide inactive/archived data identically to nonexistent data (mirroring F3's non-disclosure test pattern exactly).
- **Security:** cross-tenant rejection (tenant A's service ID rejected against tenant B's context, same as the existing cross-tenant tests for profile/onboarding), inactive-item booking rejection.
- **Transaction/concurrency:** N14 specifically needs a concurrent-request test proving the exclusion constraint actually prevents two simultaneous bookings for the same slot — a genuine integration test spinning up concurrent goroutines against the real disposable DB, not a unit test (concurrency bugs don't reproduce in fakes).
- **Regression:** every existing suite (`internal/tenant/...`, `internal/authorization/...`, `internal/identity/...`, `internal/app/...`) re-run green after each feature, matching the established F1–F3 discipline.

**Frontend**, per feature with a frontend component:
- **API/query:** `modules/<domain>/{api,queries}.ts` tested the same way `modules/onboarding`/`modules/tenant` already are (mutation cache-update behavior, query-key scoping).
- **State/cache:** tenant-switch cache isolation (an already-proven pattern via `PermissionsProvider`'s "new query key per tenant" behavior) extended to catalog/staff/availability queries.
- **Form behavior:** service/category/add-on/package create-edit forms — required-field validation, price/duration input formatting (minor-units conversion at the UI boundary is a real, testable seam).
- **Permission gating:** `Can`/`useCan` gating catalog-management UI on `service.*` permissions once they exist (§16).
- **Public rendering:** the public catalog/booking pages rendering only what the backend returns, with a legacy/empty-catalog tenant rendering safely (mirrors the existing `business_type === null` safe-rendering precedent from F7's plan).
- **Error/loading:** `ApiError.code`-driven UI branching (never `.message` parsing), matching the established convention throughout this frontend.
- **Accessibility:** form labeling, focus management for the multi-step catalog/booking flows — standard practice, not deferred as an afterthought.

---

## 39. Detailed Feature Breakdown

Reordered minimally from the source document's suggested N1–N16 — categories are pulled into the foundation feature (N1) rather than left for N3, since `category_id` is a foundational column on Service from day one (§4), not a retrofit; N3 becomes specifically the *management API* surface built on N1's domain.

### N1 — Service & Category Catalog Foundation
- **Goal:** domain model, migration, repository for `services` and `categories` (§4, §8), including archive-not-delete lifecycle (§14).
- **Backend scope:** `internal/catalog/model` (`Service`, `Category`, `Status` types), migration (id, tenant_id, category_id, name, description, duration_minutes, price_minor, buffer_before/after_minutes nullable, online_booking_enabled, status, display_order + categories table), `internal/catalog/repository` (narrow, tenant-scoped CRUD, matching `PostgresTenantRepository`'s conventions).
- **Frontend scope:** none.
- **Dependencies:** none (first Nail feature).
- **TDD matrix:** domain validation, repository round-trip (Docker DB), tenant-scoping proof, archive-not-delete proof.
- **Acceptance:** a service can be created, read, updated, archived; every query is tenant-scoped; no cross-tenant leak possible.
- **Non-goals:** no HTTP layer yet (N3), no templates yet (N2), no add-ons/packages yet (N4/N5).

### N2 — Starter Service Templates
- **Goal:** `service_templates` table (§5), seeded via migration with the Nail starter catalog from the source document, template-selection service method (§6, copy-on-select).
- **Backend scope:** migration (seed INSERT, same convention as `000006`), `internal/catalog/service` template listing + "create service from template" method.
- **Frontend scope:** none.
- **Dependencies:** N1.
- **TDD matrix:** template listing scoped by `business_type`; selection copies correctly and sets `source_template_id`; template row is never mutated by a tenant action.
- **Acceptance:** every starter service/add-on/package name in the source document exists as seeded template data (add-ons/packages depend on N4/N5 existing first for their own template rows — see note below).
- **Non-goals:** no localization, no admin template-editing UI.
- **Note:** add-on and package *templates* are seeded once N4/N5's tables exist — N2 seeds Service templates first; add-on/package template seeding is a small addition revisited at N4/N5, not a blocker to N2 shipping services-only templates first.

### N3 — Service Management API
- **Goal:** private HTTP surface for services/categories (§17).
- **Backend scope:** `internal/catalog/handler`, routes wired in `app.go` matching the established `authMiddleware.Wrap(tenantMiddleware.Wrap(TenantPermissionMiddleware{...}.Wrap(...)))` chain; `service.*` permissions added (§16) — **this is the point where that migration actually happens**, not before.
- **Frontend scope:** none yet (N6).
- **Dependencies:** N1, N2 (template-selection endpoint), permission catalog migration.
- **TDD matrix:** full route-chain tests per §38, cross-tenant rejection, decode-target protection.
- **Acceptance:** an authenticated `BUSINESS_OWNER` can create/list/update/archive services and categories via HTTP; `STAFF` can read only; unauthenticated/cross-tenant requests denied.

### N4 — Add-Ons
- **Goal:** `addons` + `service_addons` (§9, §10).
- **Backend scope:** model/migration/repository/service/handler for add-ons, compatibility-join endpoint.
- **Dependencies:** N1, N3 (reuses its permission/route conventions).
- **TDD matrix:** compatibility-join CRUD, price/duration-delta arithmetic unit tests, cross-tenant rejection.
- **Non-goals:** no per-service override columns (§10).

### N5 — Packages
- **Goal:** `packages` + `package_items` (§11), no-recursion constraint.
- **Backend scope:** model/migration/repository/service/handler; `item_type CHECK` constraint; service-layer validation that `item_id` exists in the correct target table (the FK gap noted in §11, §44).
- **Dependencies:** N1, N4 (a package needs services and add-ons to reference).
- **TDD matrix:** no-recursion proof (attempting `item_type = 'PACKAGE'` rejected at the DB constraint level), cross-table `item_id` validation at the service layer, price/duration explicit-not-derived proof.

### N6 — Nail Service Catalog Owner UI
- **Goal:** the dashboard `Services` module (§34) — catalog list, category management, template-selection flow, add-on/package builders.
- **Frontend scope:** `modules/catalog/*`, new dashboard routes, `dashboardNavItems` entries gated by `business_type` (reusing the extension point already designed in the Epic 02 plan's §17 for exactly this purpose).
- **Dependencies:** N1–N5 fully shipped.
- **TDD matrix:** per §38's frontend section.
- **Acceptance:** an owner can select starter services, customize price/duration, and publish (`status = ACTIVE`, `online_booking_enabled = true`) — this is half of §40's minimum viable slice.

### N7 — Staff / Technician Profile
- **Goal:** `staff_profiles` (§20).
- **Backend scope:** `internal/staff/{model,repository,service,handler}`, routes under `/api/v1/tenants/{tenantID}/staff`.
- **Dependencies:** none beyond existing tenant/membership infrastructure (independent of N1–N6, can be built in parallel).
- **TDD matrix:** 1:1 uniqueness with membership, tenant-scoping, cross-tenant rejection.

### N8 — Staff-Service Capability
- **Goal:** `staff_service_capabilities` (§21).
- **Backend scope:** join-table model/repository/service/handler.
- **Dependencies:** N1 (services must exist), N7 (staff profiles must exist).
- **TDD matrix:** many-to-many CRUD, "any qualified technician" query proof, activation/deactivation via status not delete.

### N9 — Business & Staff Working Hours
- **Goal:** `tenant_business_hours` (§23), `staff_working_hours` (§24).
- **Backend scope:** two small model/repository/service/handler sets, split-hours support proven from day one.
- **Dependencies:** N7 (staff working hours needs staff profiles); business hours independent of staff, could ship slightly earlier if sequencing pressure demands.

### N10 — Time Off / Exceptions
- **Goal:** `tenant_closures`, `staff_time_off` (§25).
- **Dependencies:** N9.

### N11 — Availability Engine
- **Goal:** the algorithm from §26 as its own service, `internal/availability`.
- **Backend scope:** pure computation service consuming N1(service duration/buffers), N8 (capability), N9 (working hours), N10 (exceptions), and eventually N14 (existing bookings) — no HTTP surface yet.
- **Dependencies:** N1, N8, N9, N10.
- **TDD matrix:** the algorithm's ∩/− composition, DST-boundary correctness (§44), timezone-conversion correctness.
- **Non-goals:** no booking creation yet — this computes candidate slots only.

### N12 — Public Service Catalog
- **Goal:** `GET /api/v1/public/tenants/{slug}/services` (§19).
- **Backend scope:** public service/handler, gated by the §15 formula.
- **Dependencies:** N1–N5 (needs the full catalog shape to have something to expose), F3 (already shipped — reused, not re-implemented).
- **TDD matrix:** non-disclosure proof (archived/internal/inactive items 404 or absent, matching F3's own pattern), unauthenticated access proof.
- **Acceptance:** this is the second half of §40's minimum viable slice — a customer can see a tenant's published services publicly.

### N13 — Public Availability
- **Goal:** `GET /api/v1/public/tenants/{slug}/availability` (§27).
- **Dependencies:** N11, N12.

### N14 — Booking Core
- **Goal:** the Booking aggregate (§28), price/duration snapshotting (§31), concurrency-safe creation (§32).
- **Backend scope:** `internal/booking/*`, the `btree_gist` exclusion constraint, `customers` table (§29).
- **Dependencies:** N11, N13, `customers`.
- **TDD matrix:** the concurrency integration test (§38) is mandatory here, not optional.

### N15 — Public Nail Booking Journey
- **Goal:** the full customer flow (§28 of the source doc) as frontend pages, using N12–N14's APIs.
- **Frontend scope:** the public booking flow UI.
- **Dependencies:** N12, N13, N14.

### N16 — Owner Booking Calendar / Management
- **Goal:** the dashboard `Bookings` module (§34).
- **Dependencies:** N14.

---

## 40. Prioritization

**Confirmed: N1 + N2 + N3 + N6 + N12 is the correct smallest useful vertical slice — the source document's own suggestion is validated, not just accepted on faith.**

Reasoning: this slice delivers real, visible product value (an owner can build a real service menu; a customer can see it publicly) while deliberately deferring the two hardest, highest-risk pieces — availability computation (§26, genuinely nontrivial: three intersecting schedule sources minus two exception sources) and booking concurrency (§32, a correctness-critical distributed-systems-adjacent problem even in a single-Postgres monolith). Shipping the catalog-visible slice first also produces the fastest feedback loop on whether the *catalog model itself* (§4–§11) is right before staff/availability/booking code gets built on top of it — cheaper to discover a catalog modeling mistake before three more feature layers depend on it than after.

---

## 41. Migration Plan

**REQUIRED FIRST** (N1–N6's dependencies):
- `categories`, `services` (N1)
- `service_templates` (N2), seeded
- `service.*` permission rows (N3) — the only *existing-table* migration in this whole first wave (`role_permissions` insert)
- `addons`, `service_addons` (N4)
- `packages`, `package_items` (N5)

**LATER** (N7–N14):
- `staff_profiles` (N7)
- `staff_service_capabilities` (N8)
- `tenant_business_hours`, `staff_working_hours` (N9)
- `tenant_closures`, `staff_time_off` (N10)
- `customers`, `bookings`, `booking_addons` (N14) + `CREATE EXTENSION IF NOT EXISTS btree_gist` + the exclusion constraint

**NOT YET:**
- Any `currency` column (§12) — until multi-currency is a real requirement.
- Any `deposit`/payment-related column — until payment is planned.
- Any effective-dating on `staff_working_hours` (§24), any partial-day time-off granularity (§25) — until a real need is reported.
- Any `booking_ready`/`vertical_setup_status` column (§36) — computed live instead.

**Indexes/constraints, called out explicitly:**
- Every catalog table: index on `tenant_id` (tenant isolation is the dominant query filter everywhere).
- `categories`/`services`/`addons`/`packages`: composite index on `(tenant_id, status, display_order)` for the common "active items, in order" query shape (both owner-facing and public).
- `staff_profiles`: unique `(tenant_id, user_id)`.
- `staff_service_capabilities`: unique `(staff_profile_id, service_id)`.
- `bookings` (N14): the `btree_gist` exclusion constraint on `(staff_profile_id, tsrange(starts_at, ends_at)) WHERE status NOT IN ('CANCELLED')` — the one genuinely load-bearing constraint in this entire plan.
- **Not over-indexed:** no speculative indexes on `description`/`bio`/other free-text fields; no full-text search infrastructure proposed (no requirement for it exists in the source document).

---

## 42. Error Contract

**Reused, no new codes needed for most cases:**

| Situation | Code |
|---|---|
| Service/category/add-on/package not found, or cross-tenant | `TENANT_NOT_FOUND`-shaped 404 is wrong here — reuse the general `RESOURCE_NOT_FOUND` (already exists, unused elsewhere so far, exactly fits) |
| Invalid service field (empty name, non-positive duration/price) | `VALIDATION_FAILED` (existing) |
| Missing permission | `PERMISSION_DENIED` (existing) |
| Cross-tenant access attempt | `TENANT_ACCESS_DENIED` (existing) |
| Malformed ID in path | `INVALID_REQUEST` (existing) |

**Genuinely new codes identified, not created now** (per instruction — conceptual only):
- A booking-time slot conflict (§32's exclusion-constraint violation) doesn't map cleanly onto any existing code — `RESOURCE_NOT_FOUND`/`VALIDATION_FAILED` would both be misleading. A dedicated `SLOT_UNAVAILABLE` (409-shaped, matching `TENANT_SLUG_TAKEN`'s existing precedent for "someone else got there first") is the one code this plan believes will genuinely be needed, decided at N14's implementation time.
- "Invalid add-on for this service" (attaching an incompatible add-on) is adequately covered by `VALIDATION_FAILED` — not distinct enough to warrant its own code.
- "Service unavailable for booking" (archived/not online-bookable) is adequately covered by `RESOURCE_NOT_FOUND` at the public boundary (matching F3's non-disclosure philosophy — an unbookable service should look absent, not present-but-rejected).

---

## 43. Security Invariants

Every one of the source document's §17/§43 invariants, confirmed against this plan's specific design (not restated generically):

- **All management APIs require authenticated tenant context:** every N3/N4/N5/N7/N8/N9/N10 route follows the identical `Authentication → Tenant Context → TenantPermissionMiddleware → Handler` chain already proven correct by F1–F4 of the onboarding work — no new middleware primitive, no weaker parallel path.
- **Permissions govern service/staff management:** `service.*` (§16); staff/capability/hours permissions follow the same minimal pattern when their own features ship (not designed in detail now, same principle applies).
- **Tenant A cannot access tenant B's catalog:** every repository method takes and filters by `tenant_id`; every route resolves `tenantID` through the same `TenantContextService.Resolve` that already proves membership before any handler runs.
- **Public APIs expose only active/public data:** the §15 formula, applied identically at every public endpoint (N12, N13) — the same "collapse to indistinguishable-from-absent" pattern F3 already established, reused rather than reinvented.
- **Inactive services cannot be newly booked:** enforced at booking-creation time (N14) by re-checking `status = 'ACTIVE' AND online_booking_enabled = true` server-side, never trusting a client-supplied "this was active when I loaded the page" assumption.
- **Staff capability is tenant-scoped:** `staff_service_capabilities` join rows reference `staff_profile_id`/`service_id`, both already tenant-scoped FKs — no cross-tenant capability is representable even accidentally.
- **Booking creation revalidates price/duration/availability server-side:** §31 (snapshot computed server-side, never accepted from the client) and §32 (availability re-checked at write time, not trusted from an earlier read) — both explicit, both load-bearing.
- **Client totals are never authoritative:** direct consequence of the above two — a client may *preview* a total (matching the source document's own §6), but the booking-creation endpoint computes and stores its own total independent of whatever the client sent.
- **Archived services remain historically safe:** §31's snapshot mechanism means an archived (or renamed, or repriced) service can never corrupt a historical booking's displayed price/duration/name.
- **Service IDs from another tenant are rejected safely:** every lookup is `WHERE id = $1 AND tenant_id = $2`, not `WHERE id = $1` — a cross-tenant ID simply doesn't match, producing the same `RESOURCE_NOT_FOUND` as a nonexistent one (no distinguishable error leaking existence).

---

## 44. Risks

- **Generic Service model becoming a cross-vertical dumping ground:** actively guarded against by §37's conservative reuse boundary and §9's rejection of polymorphism — but the risk is real for *future* agents under time pressure who might be tempted to add a nullable `room_id`-shaped column to `services` when Hotel arrives. Flag this document in review whenever that work starts.
- **Template updates mutating tenant services:** structurally impossible by construction (§6 — copy-on-select, `source_template_id` is read-only provenance, never a live join) — low residual risk, but worth a standing test (§38) rather than trusting the design alone.
- **Hard deletes breaking booking history:** closed by the universal archive-not-delete rule (§14, §33) applied without exception.
- **Add-on price/duration composition bugs:** integer-minor-unit arithmetic (§12) removes the floating-point class of bug; remaining risk is ordinary summation-logic bugs, covered by the domain-level unit tests in §38.
- **Package recursion:** closed by the `item_type CHECK` constraint (§11) at the database level — the strongest guarantee in this plan, not just a service-layer convention.
- **Staff capability leakage:** low risk given both FKs in `staff_service_capabilities` are already tenant-scoped, but explicitly covered by a dedicated cross-tenant test (§38) rather than assumed safe.
- **Timezone errors / DST:** a real risk for the availability engine (N11) specifically — business/staff hours stored as plain `TIME` values interpreted against `tenants.timezone` means a DST transition day has an hour that either repeats or doesn't exist; N11's TDD matrix explicitly calls out DST-boundary tests (§38) rather than leaving this to be discovered in production. Not solved by this plan (no algorithm chosen for DST handling), only flagged as mandatory to address at N11's implementation time.
- **Simultaneous bookings:** the central risk of this entire plan, addressed as thoroughly as a planning document can (§32) — the exclusion-constraint approach is the recommended, not yet implemented, mitigation.
- **Stale availability:** inherent to on-request computation (§26) between the moment a customer views slots and the moment they book — mitigated, not eliminated, by §32's write-time revalidation; a customer can still see a slot "disappear" between viewing and confirming, which is normal, expected behavior for any live-inventory booking system, not a bug to engineer away.
- **Service price changes after booking:** closed by §31's snapshot mechanism.
- **Public exposure of inactive services:** closed by the §15 formula being the *only* path the public endpoint reads through — no alternate query path exists that could bypass it, by construction of N12's design.
- **Tenant switching / cache leakage:** the existing `PermissionsProvider` "new query key per tenant" pattern is the proven precedent; N6's frontend catalog queries must follow the identical pattern (called out explicitly in §38's frontend TDD matrix so it isn't accidentally skipped).
- **`onboarding_status` vs. booking-readiness semantics:** the second most important risk after concurrency — resolved by §36's decision to never conflate the two, and to compute booking-readiness live rather than storing it, removing the drift risk a stored flag would introduce.
- **Future Restaurant/Hotel/Transport reuse pressure:** addressed by §37's explicit, conservative, per-concept reuse table — the biggest risk here is social/process (an agent under deadline pressure reusing something inappropriately), not technical; this document is the artifact to point back to when that pressure arrives.

---

## 45. Acceptance Criteria (for this planning pass)

This document is complete when, without any code having been written:

1. Every domain decision in §4–§32 is traceable to either repository evidence or an explicit, argued tradeoff — never asserted without reasoning.
2. The document/repository conflict table (§3) contains no unresolved conflicts — every one has a stated decision.
3. §35/§36's onboarding-status question is answered with a concrete mechanism (live-computed readiness, no schema change), not left open.
4. §39's feature breakdown is small enough that N1 alone is independently implementable and testable without any of N2–N16 existing.
5. §44's risk list includes every risk the source document asked for, each with either a mitigation or an explicit "not solved here, flagged for implementation time" note — never silently omitted.

---

## 46. Explicit Non-Changes

- `internal/tenant/*` — untouched. No field, migration, or behavior from Vertical Onboarding F1–F3 is modified by this plan.
- `onboarding_status`'s meaning — unchanged, restated explicitly in §35/§36 rather than assumed safe by omission.
- The permission catalog — no migration created; `service.*` is a recommendation for N3's implementation time, not a change made now.
- `DashboardShell`, `TenantGate`, `TenantProvider`, `PermissionsProvider` — reused as-is; no redesign proposed anywhere in this plan.
- No Restaurant/Hotel/Transport table is designed, named, or scaffolded — only the *reuse boundary* (§37) is discussed, at the conceptual level the source document itself requested.
- No payment/deposit architecture — explicitly deferred throughout (§4, §12, §30), never partially built "just in case."
