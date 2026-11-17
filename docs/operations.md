# Operations and verification

## Release model

One workspace and one Telegram account, with session-authenticated super_admin/admin/buyer accounts. See [identity and upgrade](identity.md) before moving an existing installation to migration 004. Shared-source buyer access remains disabled until step 3; the catalog remains electronics until the healthcare slice. Historical offers/conversations keep their configured operator ownership. Take backups before upgrading.

The parent `compose.yaml` is the deployment entry point. `compose.demo.yaml` adds sample first-boot data to a separate `perso-demo` project. Never attach that override to production volumes.

## Database migrations

Migrations are embedded from `internal/storage/postgres/migrations/*.sql`, sorted, serialized by advisory transaction lock, and recorded in `schema_migrations` with SHA-256 checksums. Existing 001/002 schema installations are adopted without deleting rows. Do not edit migration files after deployment; add a new numbered file. The parent runs the dedicated migration role. Direct CLI starts also verify the migration ledger.

Migration 003 preserves existing single-item rows and their original message IDs. It adds stable item identities, revisions, ownership, outbound/inbound records and a separate history cursor. Duplicate historical conversation records are preserved; only the earliest retains their draft deduplication key. Duplicate match links are collapsed before adding uniqueness. Old price history is retained; new writes deduplicate by revision. Back up before this migration. Items previously overwritten by the old one-offer-per-message schema require reprocessing retained raw messages; they cannot be reconstructed from the old offer row alone.

Price comparisons use integer Toman, truncating Rial remainders toward zero. Original price units remain on offers/history. Product expiry (`expiration_date`), listing validity (`listing_expires_at`) and warranty end (`warranty_until`) are distinct fields. Active APIs exclude expired product/listing validity. Full-population ranking currently runs in the Go API before pagination; benchmark and move it to a maintained scoring projection before a high-volume rollout.

## Disposable acceptance tests

From the parent, start only isolated test services:

```sh
docker compose -p perso-audit -f compose.test.yaml up -d
cd persob
PERSO_AUDIT_DATABASE_URL='postgres://postgres@127.0.0.1:55439/perso_audit?sslmode=disable' \
PERSO_AUDIT_REDIS_ADDR='127.0.0.1:56389' \
PERSO_AUDIT_MONGO_URI='mongodb://127.0.0.1:57039' \
go test -tags=audit -count=1 -v ./internal/audit ./internal/telegram
```

The tests guard endpoint identities and use disposable schemas/databases/stream names. Do not point them at production. CI runs the same suites. The domain expectation was updated deliberately: an unsupported healthcare product must not silently create an electronics offer. This does not remove the price, idempotency, ranking, queue or source-status acceptance checks.

## Account lifecycle

Only the gateway owns the encrypted session and its file lock. Stop it before interactive `login`; enter phone code and optional 2FA password at the terminal. Password input is hidden. The login command never creates a new Telegram account. Use an existing account and your own app credentials.

The API calls the authenticated internal gateway for source resolution. Private invite links are checked without auto-joining. A newly unauthorized session shows a gateway status requiring authorization; re-run login. Source callbacks cache verified user access hashes for outbound peer resolution. Broadcast channel IDs never become personal sellers.

History scans run every 30 seconds. `last_message_id` is the greatest observed message; `history_message_id` only advances after a successful bounded scan. Initial history is intentionally bounded. If a recovery gap exceeds `TELEGRAM_BACKFILL_LIMIT`, the history cursor remains unchanged and logs report the gap. Increase the limit deliberately (maximum 1000) or perform an operator-managed deeper import with `go run ./cmd/reextract`. That command scans retained raw messages in a stable `(received_at, chat_id, message_id)` order, writes a 0600 keyset checkpoint only after each successful page, and requires `--enqueue` plus a checkpoint before publishing to the normal Redis stream. It never authorizes Telegram or sends outbound messages. Edits within the scanned overlap and channel deletions update derived offers. Discussion replies are linked to channel posts only when a verified forwarded root or stored mapping establishes that relationship.

## Queue and outbound diagnosis

Redis pending entries are reclaimed after idle leases; active handlers renew their leases. Each worker process uses unique consumer IDs and bounded concurrency. After repeated failure, an event is atomically moved to `<stream>:dead` and acknowledged on the original group. A malformed payload is retained there. Inspect the dead stream, correct the cause, then replay the original payload through the producer; never clear the pending list to mask failures. Default stream retention is unbounded to avoid dropping unprocessed work; monitor disk usage and archive/trim only entries below every consumer group's acknowledged boundary.

Mongo `dispatch_pending`/`dispatch_hash` markers close the raw-write/Redis-publish gap by reconciling undispatched snapshots. Redelivery is safe for PostgreSQL effects. Worker keys under `perso:telegram:messages:workers:*` expire after 15 seconds when heartbeats stop. Monitor pending count/age, dead-letter growth, Redis disk use, gateway authorization and structured error/request logs. API request logs include status and request ID. `/ready` covers dependency connectivity; gateway status is separately exposed under the authenticated `/api/v1/telegram/status` endpoint.

Outbound commands are committed with conversation state. The gateway rechecks approval, state, opt-out and active interest before sending. A stable Telegram `random_id` is reused on retry. It does not promise exactly-once external delivery after ambiguous failures. Failed attempts remain visible; the default maximum is three, with 60-second delay. The queue records API acceptance, not read receipts. Live sends were not executed during implementation validation.

## Retention and external models

`RETENTION_DAYS=0` keeps stored data. A positive value explicitly enables expiry for raw/extracted/model/conversation Mongo records and cleanup of terminal conversation history/audit logs. Existing older records can expire after enabling this setting. Pending or processing raw messages, and messages still marked for dispatch reconciliation, are excluded from expiry until they become recoverable completed history. TTL cleanup is asynchronous; it is not a guaranteed immediate deletion service. Backups have their own retention policy. Retention does not delete the canonical catalog or price aggregates automatically.

External LLM calls require both `LLM_ENABLED=true` and `external_llm_allowed=true` on the source's create request. Unknown/legacy sources cannot be sent externally. Request count is limited to 30/minute across workers, and inputs over 6000 characters skip enrichment. Raw provider prompts and responses follow retention policy. Configure permission only for sources whose data you are authorized to process. No model can grant consent, approve a conversation or override established deterministic money facts. For the local demo, set `LLM_PROVIDER=openai_compatible`, `LLM_BASE_URL=http://llm-mock:8091/v1`, and `LLM_MODEL=perso-healthcare-mock`; see [the step 5 mock contract](openai-mock.md). Keep `LLM_API_KEY` server-side.

## Backup and restore

Back up PostgreSQL, MongoDB and the encrypted Telegram session volume before upgrading. Keep the session encryption key separately in your secret store. Restrict backup file permissions, encrypt backups at rest and test restoration into an isolated project.

```sh
mkdir -m 700 backups
umask 077
docker compose exec -T postgres pg_dump -U perso -d perso_db -Fc > backups/postgres.dump
docker compose exec -T mongodb sh -c 'exec mongodump --username perso_root --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --db perso_raw --archive' > backups/mongodb.archive
docker compose stop telegram-gateway
docker compose run --rm --entrypoint tar -T telegram-gateway -C /data/sessions -czf - . > backups/sessions.tar.gz
docker compose up -d telegram-gateway
```

Verify the commands' exit codes and file sizes; a redirection can create an empty file on failure. Restore into a new, isolated project configured with the original session encryption key. Use `pg_restore` against its empty database, `mongorestore --archive` with its administrative credential, and unpack session files with owner UID/GID 10001. Then run migrations, `/ready`, authenticated source reads, offer counts and an operator-authorized account check. Do not run a restored session concurrently with the original. A full PostgreSQL/MongoDB/session backup-and-restore rehearsal remains an operator staging check; it was not executed during this implementation.

## Production gate

Run unit/race/vet/build, disposable acceptance tests, API generation consistency, frontend typecheck/build/browser tests and image/dependency scans. Run the ranking benchmark and retain its output with the release evidence. Use `scripts/container-rehearsal.py` plus the queue reclaim/dead-letter tests for recovery evidence, and run `cmd/reextract` in dry-run mode before any bounded replay. Check fresh install and upgrade preservation. Configure HTTPS and random tokens. Live account authorization, real source classification/history, reconnect/FloodWait handling and one approved conversation must be validated by the operator with a test account before enabling production outreach. Those actions require real credentials and were not simulated as proof of live success.
