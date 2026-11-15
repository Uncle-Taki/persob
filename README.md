# Perso backend

Go API, Telegram gateway, Redis worker, extraction/matching and storage for the Perso shared workspace with three-role password accounts. Use the parent `persop` repository to run the full stack; its Compose deployment builds the `persof` React frontend.

- **Go:** 1.26.11; **PostgreSQL:** 16; **MongoDB:** 7; **Redis:** 7.
- `api` serves session-authenticated REST endpoints. `super_admin`, `admin`, and `buyer` are enforced by the backend. `API_TOKEN` is monitoring-only; caller-provided identities/roles are ignored. See [accounts and upgrade](docs/identity.md).
- `gateway` (also `collector`) owns the sole encrypted Telegram session, source resolver, history scans, inbound reply routing and outbound dispatch. API and worker processes do not open that session.
- `worker` processes immutable queued snapshots, reclaims pending messages and deduplicates per-item offers/history/matches.
- `login` is interactive account authorization, without database dependencies. Stop the gateway first.
- `migrate` applies ordered embedded migrations with checksums and an advisory lock.
- `all` runs API, gateway and workers together with shared cancellation. It is convenient locally; Compose uses separate roles.

The live catalog is electronics. Healthcare fixtures remain in the frontend's separate demo. LLM extraction is disabled by default, also requires an explicit source-level permission, is capped at 30 calls/minute across workers and skips messages over 6000 characters. Deterministic processing remains available without a provider. The local OpenAI-compatible demo contract and failure scenarios are documented in [docs/openai-mock.md](docs/openai-mock.md).

Normal developer commands:

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/perso
python3 scripts/sync-openapi.py
```

`make lint` checks formatting without rewriting files; `make format` rewrites formatting. `make docker-down` preserves volumes. See [docs/operations.md](docs/operations.md) for acceptance tests and operations. The legacy `web/` dashboard is retained for reference and is not served by the parent deployment.

Review drafts require a qualifying interest and an identifiable seller. Sending is disabled by default (`OUTREACH_ENABLED=false`). Once enabled by the operator, each draft still requires UI approval; consent is evaluated by deterministic application policy. Typed intake supports price, condition, authenticity claims, manufacturer, warranty end, city and Tehran area; product expiry is an optional separate field. Unresolved fields are retained when the configured question budget ends. The outbox distinguishes queued, sent, failed and cancelled; “sent” means Telegram API acceptance, not a read receipt.

Shared workspace source ingestion, explicit administrator-managed buyer grants and migration 005 operations are documented in [shared sources](docs/shared-sources.md). Buyer marketplace access remains blocked until step 3B.
