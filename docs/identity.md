# Accounts and sessions (step 2)

The API now authenticates individual password accounts with PostgreSQL-backed, 12-hour opaque sessions. Passwords use bcrypt; the session store contains only token hashes. Browser cookies are HttpOnly, SameSite=Strict, scoped to /api, and Secure in production. State-changing authenticated requests require X-CSRF-Token from the login/session response; login requires JSON and rejects cross-origin browser requests. No public signup exists.

## Installation and upgrade

The parent setup-env.py generates a random BOOTSTRAP_ADMIN_PASSWORD and BOOTSTRAP_ADMIN_USERNAME=owner in a private environment file. Keep AUTH_COOKIE_SECURE=true in production and serve the frontend over HTTPS. Use false only for development HTTP; production configuration rejects it.

For an existing private .env, add an explicit bootstrap username and a new password containing 12–72 UTF-8 bytes. Keep the existing OPERATOR_USER_ID so historical ownership is preserved. Do not replace the Telegram session key. Run the new migration before starting the API:

```sh
docker compose run --rm backend-api migrate
docker compose run --rm backend-api bootstrap-admin
docker compose up -d --build backend-api frontend
```

The api/all commands also perform this explicit bootstrap when both environment values are supplied. It only initializes the first active super admin and does not overwrite an existing password or restore an administrator demoted later. Remove bootstrap credentials from the deployment environment after establishing access if desired. Losing every administrator requires an explicit operator recovery procedure; startup never silently resets accounts.

Migration 004 preserves rows/foreign keys/history, maps legacy user/operator/unknown roles to inactive buyer, retains admin roles as inactive, and requires explicit activation/password configuration. Only the configured operator ID is eligible for initial super-admin bootstrap. Unknown roles are never promoted. Case-insensitive username collisions stop migration for explicit resolution; they are not silently merged.

## Roles and current boundary

- super_admin manages users and roles. The last active super_admin cannot be deactivated or demoted, including under concurrent requests.
- admin can create, edit, reset and deactivate buyer accounts. It cannot manage another admin or grant elevated roles.
- buyer manages its own profile, preferences, password and interests, and can read the shared catalog. Shared offers/sources/conversations/analytics remain denied until step 3 implements source assignments and buyer-specific scoping.

Role/status changes and password changes/resets delete affected sessions. Every request reads current account status/role, so old cookies cannot retain privilege. Administrative writes use a transaction/advisory lock, recheck the acting administrator, protect the last super admin and record an audit event without credentials. Existing single-owner marketplace behavior for administrators is preserved in this checkpoint; cross-admin/shared-source behavior belongs to step 3.

Users are listed in a bounded initial administration view (up to 200); pagination is a future extension. Username/password account management is the implemented identity provider; social login/email delivery/MFA are outside step 2.

## API and client

POST /api/v1/auth/login accepts username/password; GET /api/v1/session returns account, permissions and CSRF token. POST /api/v1/auth/logout revokes the current session. GET/PATCH /me and /me/preferences are personal; POST /me/password requires the current password and revokes every session. /users and /users/{id} enforce administrative scope; /users/{id}/reset-password revokes target sessions. See canonical OpenAPI for bodies/errors.

Login attempts are bounded across API replicas: 20 per normalized username and 50 per socket peer per 15-minute window. Do not trust arbitrary forwarded client IP headers. A reverse proxy concentrates the peer limit; tune the deployment's authenticated/trusted edge policy if greater login volume is needed. Rate counters contain hashed keys and are cleaned opportunistically; unknown accounts use a dummy password hash to avoid an immediate timing distinction.

The frontend no longer stores or submits API_TOKEN. It resumes via the cookie, keeps CSRF in memory, refreshes session state, and provides Persian/English profile/preferences/password and account administration in Settings. Preferred location is persisted for the later buying filters; it does not claim a new matching behavior in step 2. Default sort, number format and language affect the current UI.

API_TOKEN is now monitoring-only: GET /api/v1/operations/status returns aggregate gateway availability. It cannot authenticate /session, read catalog/offers/users, or perform any user mutation. /ready checks dependency readiness. Token rotation does not revoke browser sessions; use account/password operations for that.

## Verification

Run the identity HTTP/database tests only against a disposable local database:

```sh
PERSO_IDENTITY_TEST_DSN='postgres://postgres@127.0.0.1:55439/perso_audit?sslmode=disable' go test -race -count=1 -v ./internal/identity
```

Tests isolate schemas and cover persisted sessions/preferences, CSRF/origin/cookie settings, legacy-token denial, roles, password/deactivation/role revocation, concurrent super-admin protection, login limits and populated upgrade/idempotency. Full-stack browser tests exercise the actual API router and UI. Keep Telegram mock mode and outreach disabled during these checks. No real seller communication is needed.
