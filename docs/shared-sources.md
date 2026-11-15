# Shared sources and assignments (step 3A)

This installation has one fixed workspace (`00000000-0000-4000-8000-000000000001`), one Telegram account/gateway and one normalized source per peer. `telegram_sources.owner_id` retains creator/legacy provenance; it no longer partitions ingestion between administrators. Sources and offers are visible operationally to authenticated administrators. Buyer marketplace routes remain denied until step 3B.

New listings have `workspace_id` and, when exact provenance is known, `source_id`; buyer interests do not duplicate the listing. Legacy `offers.owner_id` is retained as historical attribution. Unknown provenance is not implicitly shared with buyers. The worker resolves the exact message peer server-side; it does not trust a queued source ID. Source-level external LLM permission is checked on that exact peer independently of its creating administrator.

## Upgrade

Back up before migration 005 and build the updated images before running it:

```sh
docker compose build backend-api frontend
docker compose run --rm backend-api migrate
docker compose up -d --build backend-api frontend
```

Migrations 001–004 remain unchanged. If normalized sources contain duplicate Telegram peers, migration 005 stops and rolls back. Inspect and explicitly resolve the conflicting source records with backups/history preserved before retrying; the migration does not silently merge owners or permissions.

Legacy watch-only peers are promoted as paused/unverified registry rows while their legacy rows remain. Resolve and add each source through the connected Telegram account to refresh capabilities, then resume explicitly. For watch-only duplicates, earliest metadata supplies a paused canonical source; explicit grants are limited to documented legacy buyer owners. Newly created buyers receive no grants. Inactive legacy accounts retain dormant history/relationships, not access.

Recognizable exact peer/message offer keys are linked to normalized sources. Unknown/linked-only provenance stays unassigned. Startup does not adopt NULL historical offer/conversation ownership. Existing owned conversations get separate conversation_requests links; new operator drafts record only their explicit operator request. Buyer inquiry creation and shared seller-intake fan-out remain later work.

## Assignment API

Administrators and super admins use GET/PUT `/api/v1/sources/{id}/assignments`. GET returns `source_id`, `workspace_id`, `buyer_ids`, `version`, `linked_peer_policy: explicit`, and optional `linked_discussion_peer_id`. PUT is a complete replacement:

```json
{"buyer_ids":["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"],"version":0}
```

Use actual active buyer IDs and the current GET version. At most 200 unique IDs are accepted; inactive users, elevated roles, unknown/duplicate IDs, missing/null fields and extra properties are rejected. An empty array clears grants. A stale version returns 409 without overwriting another administrator's work. All mutations require the session CSRF token. The same transaction lock as account administration rechecks the actor and target account eligibility; replacements are audited.

Channel grants never inherit to discussions. Register and assign a linked discussion as a separate source. Its own pause/removal takes precedence over parent monitoring. Linked-only text has no exact source grant or external LLM permission.

Removing a source pauses/soft-deletes it, clears and audits grants and increments their version while retaining historical listing provenance. Re-adding the peer reuses its original source ID without restoring grants. The legacy allowlist cannot reactivate a removed/paused normalized source.

The client offers Assign buyers under Sources, with persisted selection, loading/error states, stale-form reload and Persian/English text. Its account picker lists at most 200 users; existing grants outside that list are retained unless explicitly removed. Buyers remain blocked even after assignment until step 3B implements consistent scoped APIs, matching jobs and marketplace views.
