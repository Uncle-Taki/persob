# Audit verification

The API now denies access by default until real authentication is integrated.
`X-User-ID` is not authentication. Health, readiness and the specification remain
public. To use the fixed demo identity locally, explicitly set
`ENVIRONMENT=development` and `DEMO_AUTH_ENABLED=true`. Demo access is rejected
outside development/test and must not be used as a production identity system.

Real Telegram resolution now fails closed if no client is connected. Numeric IDs
require a verified entity/access-hash cache and are not resolved using mocks.
The separate API/collector account lifecycle remains unfinished. Real Telegram
requires a non-default session encryption key. LLM enablement requires a supported
provider and an API key; it no longer silently enables a mock enrichment provider.

The standard checks are `go test ./...`, `go test -race ./...`, `go vet ./...`, and
`go build ./cmd/perso`. The frontend checks are `npm ci`, `npm run lint` (TypeScript)
and `npm run build`. There is no existing frontend unit-test script or ESLint gate.

The opt-in `audit` suite includes acceptance assertions for unresolved defects;
its failures are release evidence and must not be weakened to obtain a green run.
Use only disposable services started from the audit Compose file in the parent
workspace. Tests guard their endpoint identities and create/remove their own
PostgreSQL schemas, MongoDB databases, and Redis stream keys.

```sh
PERSO_AUDIT_DATABASE_URL='postgres://postgres@127.0.0.1:55439/perso_audit?sslmode=disable' \
PERSO_AUDIT_REDIS_ADDR='127.0.0.1:56389' \
PERSO_AUDIT_MONGO_URI='mongodb://127.0.0.1:57039' \
go test -tags=audit -count=1 -v ./internal/audit ./internal/telegram
```

MongoDB upserts now retain `original_message_text` and update `message_text` to the
current edit, including for older documents. Required index initialization fails
startup if it cannot establish the indexes. No destructive data migration was run.

PostgreSQL schema migrations no longer contain demo-owner-dependent seed inserts.
Compose mounts both schema files before the optional demo seed file on first boot.
Database and local dashboard/API ports bind to loopback. Existing volumes still
require normal backup and migration procedures; never use `docker-down` from the
current Makefile on valuable data because it includes `down -v`.

The executable entry point under `cmd/perso/main.go` was excluded by the original
unanchored ignore rule. Include this now-visible file with the reviewed changes
when committing; otherwise a fresh Git checkout still lacks the application.
