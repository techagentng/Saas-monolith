# Sign in with Google (OAuth 2.0 / OpenID Connect)

Authentication only. This feature never requests, stores, or uses a Google API
credential, and never grants a tenant, membership, or role.

## Flow

```
Browser  GET  {API}/api/v1/auth/google[?return_to=/internal/path]
  backend    mint state (32 random bytes + expiry), set HttpOnly bk_oauth_state
             + bk_oauth_return, 302 to Google
Google     authenticate + consent  (scopes: openid email profile)
  Google     302 to {API}/api/v1/auth/google/callback?code=...&state=...
  backend    clear both OAuth cookies
             -> Google `error` param?           redirect {FRONTEND}/login?auth_error=...
             -> validate state (constant time, server-enforced expiry)
             -> exchange code server-side (golang.org/x/oauth2)
             -> verify ID token (go-oidc: signature, issuer, audience, expiry)
             -> require non-empty VERIFIED email; take Google `sub`
             -> resolve/link/create local user
             -> AuthenticationService.IssueForUser -> the SAME session as password login
             -> Set-Cookie: bk_refresh (existing RefreshCookieConfig)
             -> 303 to {FRONTEND}{return_to or /dashboard}
Frontend   AuthProvider's existing startup bootstrap POSTs /v1/auth/refresh,
           receives the ordinary Ed25519 access token, and ProtectedRoute ->
           TenantGate performs the existing onboarding/dashboard routing.
```

No token — Google's or this application's — ever appears in a URL, a response
body, or a log line.

## Routes

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/api/v1/auth/google` | anonymous | Mints and binds state, redirects to Google. `?return_to=` is optional and is reduced to an internal path. |
| GET | `/api/v1/auth/google/callback` | anonymous | Validates state, exchanges the code, sets `bk_refresh`, redirects to the frontend. |

Both answer `503 SERVICE_UNAVAILABLE` (typed JSON) when the deployment has no
Google credentials, rather than 404.

## Google Cloud console

Create an **OAuth 2.0 Client ID** of type *Web application*.

- **Authorized redirect URI** — must match `GOOGLE_REDIRECT_URL` exactly:
  - local: `http://localhost:8090/api/v1/auth/google/callback`
  - production: `https://<backend-domain>/api/v1/auth/google/callback`
- **Authorized JavaScript origins** — none required. The browser never talks to
  Google from this application's own JavaScript.
- **Scopes** — `openid`, `email`, `profile`. Nothing else. Adding a Gmail,
  Drive, Calendar or Contacts scope would be a consent-screen change with no
  code in this repository that could use it.

`internal/config.GoogleCallbackPath` is the single source of the callback path:
the router registers it and configuration validation asserts
`GOOGLE_REDIRECT_URL` ends with it, so the two cannot drift into a
`redirect_uri_mismatch` that is only visible on Google's error page.

## Backend environment variables

| Variable | Required | Example |
|---|---|---|
| `GOOGLE_CLIENT_ID` | with the group | `…apps.googleusercontent.com` |
| `GOOGLE_CLIENT_SECRET` | with the group | server-only, never logged |
| `GOOGLE_REDIRECT_URL` | with the group | `https://api.example.com/api/v1/auth/google/callback` |
| `FRONTEND_URL` | with the group | `https://app.example.com` |

All four or none. Setting a subset fails startup naming the missing variable.
In `APP_ENV=production` both URLs must use `https`.

## Frontend environment variables

**None.** The button is an anchor to `NEXT_PUBLIC_API_URL` + `/v1/auth/google`.
`GOOGLE_CLIENT_SECRET` must never appear in a `NEXT_PUBLIC_*` variable, and
`GOOGLE_CLIENT_ID` is not needed in the browser either.

## Local development

```bash
# backend .env
GOOGLE_CLIENT_ID=…apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=…
GOOGLE_REDIRECT_URL=http://localhost:8090/api/v1/auth/google/callback
FRONTEND_URL=http://localhost:3000

go run ./cmd/migrate     # applies 000014_create_user_identities
go run .

# frontend
npm run dev              # NEXT_PUBLIC_API_URL=http://localhost:8090/api
```

`localhost:3000` and `localhost:8090` are same-site (a differing port does not
make a request cross-site), so the default `COOKIE_SAMESITE=Lax` /
`COOKIE_SECURE=false` work unchanged.

## Render production setup

1. Backend service → Environment: add the four variables above. Mark
   `GOOGLE_CLIENT_SECRET` as a secret.
2. `GOOGLE_REDIRECT_URL` = `https://<backend>.onrender.com/api/v1/auth/google/callback`;
   add that exact string to the Google console.
3. `FRONTEND_URL` = the deployed frontend origin.
4. `ALLOWED_ORIGINS` must already contain that same frontend origin — the
   frontend's `/v1/auth/refresh` call after the redirect is a credentialed
   cross-origin request.
5. Run the migration on the production database.

### SameSite: cross-origin vs cross-site

This matters and is easy to get wrong.

- If the frontend and backend share a registrable domain (`app.example.com` and
  `api.example.com`), they are **cross-origin but same-site**. `COOKIE_SAMESITE=Lax`
  is correct and nothing changes.
- If they are on different registrable domains (a common Render + Vercel split:
  `app.vercel.app` and `api.onrender.com`), they are **cross-site**. The
  frontend's `fetch('/v1/auth/refresh', {credentials:'include'})` is a
  cross-site subresource request, and a `Lax` cookie is not sent on it — so the
  session would appear to vanish immediately after the redirect. Such a
  deployment requires `COOKIE_SAMESITE=None` **and** `COOKIE_SECURE=true`
  (startup refuses `None` without `Secure`).

This constraint already applies to password login; Google sign-in does not
change it. What Google sign-in *does* add is the OAuth state cookie, which is
read on Google's cross-site top-level redirect back to the callback. `Strict`
would withhold it there and break every sign-in, so
`identity/handler.OAuthSameSite` maps a configured `Strict` down to `Lax` for
that cookie only — `None` is passed through, and the refresh cookie's own flags
are untouched. Nothing else is weakened.

## Identity persistence and linking policy

Migration `000014` adds `user_identities` (`id`, `user_id`, `provider`,
`provider_subject`, `provider_email`, timestamps) with
`UNIQUE (provider, provider_subject)` and `UNIQUE (user_id, provider)`.
It is tenant-independent: an external identity belongs to the platform user.

`GoogleAuthenticationService.resolveUser` applies, in order:

1. **`(GOOGLE, sub)` already linked** → that user. Google's `sub` is the
   identity, never the email, so an email change at Google does not create a
   second account. `provider_email` is refreshed for audit; `users.email` is
   not touched.
2. **No identity, but a local account has the same normalized email** → link
   them, *only* because Google both asserted the address and marked it
   verified. The existing `password_hash` is left exactly as it was, so the
   account keeps working by either method. No membership or role is read or
   written.
3. **Otherwise** → create the user and the identity in one transaction. A
   failure anywhere inside leaves no user row behind.

A non-`ACTIVE` user is refused at every branch with `INVALID_CREDENTIALS`.

A Google-only account stores `password_hash = ''`. That cannot be signed into
with a password: `Login` rejects an empty submitted password before the
verifier runs, and bcrypt rejects an empty hash regardless.

## Multi-tenant guarantees

Google sign-in writes exactly two rows on a first sign-in — one `users`, one
`user_identities` — and nothing else. It creates no tenant, no
`tenant_memberships` row, and no `user_roles` row; `BUSINESS_OWNER`, `STAFF`
and `SUPER_ADMIN` are unreachable from this path because the service is not
even handed a tenant, membership, or role repository. Post-login routing
(create-business / resume onboarding / dashboard / tenant selection) is the
frontend's existing `ProtectedRoute` → `TenantGate` chain, unchanged.

## Error codes

| Code | HTTP (JSON path) | Cause |
|---|---|---|
| `OAUTH_STATE_INVALID` | 400 | State missing, mismatched, or expired |
| `OAUTH_DENIED` | 401 | User cancelled at Google (`error=access_denied`) |
| `OAUTH_EXCHANGE_FAILED` | 502 | Code exchange failed / no `id_token` returned |
| `OAUTH_INVALID_IDENTITY_TOKEN` | 401 | Signature, issuer, audience, expiry, or claims |
| `OAUTH_EMAIL_UNVERIFIED` | 403 | Google email not verified |
| `EXTERNAL_IDENTITY_CONFLICT` | 409 | Provider account already linked |

On the redirect path these arrive at the browser as
`{FRONTEND_URL}/login?auth_error=<CODE>`; the login page maps the code to
friendly copy and the visitor can retry. Google's own `error` text is never
forwarded — it is untrusted input that would otherwise be reflected into a
rendered page.

## Manual browser test

1. Configure the four variables, run the migration, start both apps.
2. Open `/login`, click **Continue with Google**, pick an account.
3. Expect to land on `/dashboard`; a brand-new account is redirected onward to
   `/onboarding` by `TenantGate`. DevTools → Application → Cookies should show
   `bk_refresh` (HttpOnly) and **no** `bk_oauth_state` / `bk_oauth_return`.
4. Reload. The session survives (the startup refresh call).
5. Create a business, complete onboarding, sign out, sign in with Google again:
   the same account, the same tenant, no duplicate user.
6. Cancel at Google's consent screen: back on `/login` with a "cancelled"
   message and a working retry.
7. Hit `/api/v1/auth/google/callback?code=x&state=y` directly with no cookie:
   redirected to `/login?auth_error=OAUTH_STATE_INVALID`, no session issued.
8. Register a password account, then sign in with Google using the same
   address: one account, and the password still works afterwards.

## Tests

- `internal/config/google_oauth_config_test.go` — all-or-nothing configuration,
  URL shape, callback-path agreement, https in production.
- `internal/identity/service/google_oauth_test.go` — authorization URL: client
  id, redirect, state, identity scopes only, no secret, no offline access.
- `internal/identity/service/google_authentication_service_test.go` — create /
  return / link / normalize, rollback, disabled users, unverified and missing
  email, exchange and verification failures, no tenant or role writes.
- `internal/identity/handler/oauth_state_test.go` — state entropy and expiry,
  missing/mismatched/expired rejection, SameSite mapping, open-redirect
  rejection.
- `internal/identity/handler/google_auth_handler_test.go` — state binding,
  cookie clearing, refresh cookie, no credentials in the URL, Google error
  handling, safe redirects.
- `internal/identity/repository/postgres_identity_repository_integration_test.go`
  — schema constraints and real transaction rollback (needs `TEST_DATABASE_URL`).
- `internal/app/google_auth_route_test.go` — anonymous routes, method
  restriction, disabled deployment.
- Frontend: `src/app/(auth)/login/_components/login-form.test.tsx`,
  `src/providers/auth-provider.test.tsx`.

External Google calls sit behind `GoogleAuthorizationClient` and
`GoogleIDTokenVerifier`, so no test contacts Google.
