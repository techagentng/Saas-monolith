# Auth API integration — Login & Register

Backend just added CORS support so this API can be called directly from the browser. **You must set `ALLOWED_ORIGINS` in the backend's `.env`** to your frontend's dev origin (e.g. `http://localhost:3000`) before any browser call will work — comma-separated for multiple origins. Without it, requests will fail with no `Access-Control-Allow-Origin` header and the browser will block the response before your JS ever sees it.

Base URL: `http://{HOST}:{PORT}` (dev default `http://127.0.0.1:8090`).

## Register — `POST /api/v1/users`

Public, no auth required.

Request:
```json
{ "email": "user@example.com", "password": "at-least-8-chars" }
```

Response `201`:
```json
{ "id": "uuid", "email": "user@example.com", "status": "ACTIVE", "created_at": "...", "updated_at": "..." }
```

This endpoint does **not** log the user in — no tokens are returned. Immediately follow a successful register with a login call using the same credentials.

Errors:
| Code | HTTP | Meaning |
|---|---|---|
| `VALIDATION_FAILED` | 400 | Bad email format or password outside 8–128 chars |
| `INVALID_REQUEST` | 400 | Malformed JSON |
| `USER_ALREADY_EXISTS` | 409 | Email already registered |

## Login — `POST /api/v1/auth/login`

Request:
```json
{ "email": "user@example.com", "password": "..." }
```

Response `200`:
```json
{
  "user": { "id": "uuid", "email": "...", "status": "ACTIVE", "created_at": "...", "updated_at": "..." },
  "access_token": "jwt...",
  "refresh_token": "opaque-string",
  "expires_in": 900
}
```

`expires_in` is seconds until `access_token` expires (default 900s / 15min — confirm against the backend's actual `ACCESS_TOKEN_TTL`).

Errors:
| Code | HTTP | Meaning |
|---|---|---|
| `INVALID_CREDENTIALS` | 401 | Wrong email/password, or account disabled. Same code/message for both — do not distinguish in the UI (avoids account enumeration). |
| `INVALID_REQUEST` | 400 | Malformed JSON |

## Refresh — `POST /api/v1/auth/refresh`

Request:
```json
{ "refresh_token": "opaque-string" }
```

Response `200`: same shape as login, **minus `user`** (omitted — `omitempty`). Reuses your already-cached user profile.

The refresh token **rotates on every use** — the response always contains a new `refresh_token`; the old one is invalidated. Always overwrite your stored refresh token with the new value. If you reuse an old/already-rotated token, or the session was revoked (logout) or is past `SESSION_TTL` (default 24h), you'll get:

| Code | HTTP | Meaning |
|---|---|---|
| `SESSION_EXPIRED` | 401 | Session past its TTL — force re-login |
| `SESSION_REVOKED` | 401 | Logged out elsewhere or explicitly revoked — force re-login |

## Logout — `POST /api/v1/auth/logout`

Requires `Authorization: Bearer <access_token>`. No body. Response `204`. Revokes the session server-side (refresh token becomes unusable immediately).

## Authenticated requests

Every protected route (`GET /api/v1/users/{id}`, tenant routes, etc.) requires:
```
Authorization: Bearer <access_token>
```
No cookies are set or read by this API — everything is bearer-token based. That also means **no `credentials: 'include'` needed** on fetch/axios calls, which keeps the CORS setup simple (no `Access-Control-Allow-Credentials`).

A missing/expired/invalid access token returns `401 INVALID_CREDENTIALS` uniformly, whether the token is absent, malformed, expired, or points at a revoked session.

## Recommended frontend flow

1. **Token storage**: keep `access_token` in memory (a store/context), not `localStorage`, to reduce XSS blast radius. `refresh_token` can go in `localStorage` (it's already opaque and single-use/rotating) or memory if you prefer to force re-login on tab close — your call, no backend constraint either way since nothing is set as a cookie.
2. **On app load**: if you have a stored `refresh_token`, call `/auth/refresh` immediately to get a fresh `access_token` rather than requiring a fresh login every page load.
3. **Axios/fetch interceptor**: on any `401 INVALID_CREDENTIALS` from a protected route, attempt one `/auth/refresh` call; if that also fails (`SESSION_EXPIRED`/`SESSION_REVOKED`/`INVALID_CREDENTIALS`), clear stored tokens and redirect to login. Don't retry-loop.
4. **Proactive refresh**: optionally schedule a refresh a little before `expires_in` elapses (e.g. at 80% of the TTL) to avoid a user-visible failed request mid-session.
5. **Error rendering**: every error response is `{ "error": { "code": "...", "message": "..." } }`. Switch UI copy on `error.code` (stable), never on `error.message` (human string, not meant to be parsed).
6. **Concurrent refresh**: if multiple requests 401 at once, de-dupe to a single in-flight `/auth/refresh` call and replay the queued requests with the new token — don't fire N parallel refreshes (each rotates the token, so only the last one wins and the others would get `SESSION_REVOKED` on their next use).

## What changed on the backend for this

- Added `ALLOWED_ORIGINS` env var (comma-separated) and a CORS middleware wrapping the whole router (`internal/app/cors.go`). No origin listed = no `Access-Control-Allow-Origin` header = browser blocks it, even though the server still processes the request. Set this before testing from the browser.
- No changes to the login/register/refresh/logout handlers themselves — behavior above reflects existing code, verified by reading `authentication_handler.go`, `authentication_service.go`, `user_handler.go`, and the error-code-to-HTTP-status map in `internal/errors/http.go`.
