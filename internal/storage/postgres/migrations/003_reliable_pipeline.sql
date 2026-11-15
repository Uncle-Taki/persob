-- Preserve existing rows while giving each listing item its own replay identity.
ALTER TABLE offers DROP CONSTRAINT IF EXISTS uq_source_message;
ALTER TABLE offers ADD COLUMN IF NOT EXISTS source_item_key TEXT NOT NULL DEFAULT '';
ALTER TABLE offers ADD COLUMN IF NOT EXISTS source_revision_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch';
ALTER TABLE offers ADD COLUMN IF NOT EXISTS source_published_at TIMESTAMPTZ;
ALTER TABLE offers ADD COLUMN IF NOT EXISTS content_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE offers ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES users(id);
ALTER TABLE offers ADD COLUMN IF NOT EXISTS branded_known BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE offers ADD COLUMN IF NOT EXISTS listing_expires_at TIMESTAMPTZ;
ALTER TABLE offers ADD COLUMN IF NOT EXISTS warranty_until TIMESTAMPTZ;
UPDATE offers SET source_item_key=COALESCE(product_id::text,'legacy') WHERE source_item_key='';
CREATE UNIQUE INDEX IF NOT EXISTS uq_offer_source_item ON offers(source_message_id,source_item_key);
CREATE INDEX IF NOT EXISTS idx_offers_owner ON offers(owner_id);
ALTER TABLE price_observations ADD COLUMN IF NOT EXISTS revision_hash TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS uq_price_observation_revision ON price_observations(offer_id,revision_hash) WHERE revision_hash IS NOT NULL;
-- Retain historical data, but collapse duplicate match links before enforcing replay safety.
DELETE FROM offer_match_results a USING offer_match_results b
 WHERE a.offer_id=b.offer_id AND a.user_id=b.user_id AND a.interest_id IS NOT DISTINCT FROM b.interest_id
 AND (a.created_at,a.id)>(b.created_at,b.id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_match_offer_user ON offer_match_results(offer_id,user_id);
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES users(id);
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS draft_key TEXT;
UPDATE conversations SET draft_key=offer_id::text WHERE draft_key IS NULL AND offer_id IS NOT NULL;
-- Preserve old duplicate histories as separate records; only the first owns the dedupe key.
UPDATE conversations c SET draft_key=NULL WHERE EXISTS (
 SELECT 1 FROM conversations first WHERE first.draft_key=c.draft_key AND (first.created_at,first.id)<(c.created_at,c.id));
CREATE UNIQUE INDEX IF NOT EXISTS uq_conversation_draft ON conversations(draft_key) WHERE draft_key IS NOT NULL;
CREATE TABLE IF NOT EXISTS outbound_messages (
 id UUID PRIMARY KEY, conversation_id UUID NOT NULL REFERENCES conversations(id),
 seller_id BIGINT NOT NULL, text TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
 telegram_message_id BIGINT, attempts INT NOT NULL DEFAULT 0, last_error TEXT,
 available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 sent_at TIMESTAMPTZ, UNIQUE(conversation_id,id),
 CHECK(status IN ('queued','sending','sent','failed','cancelled')));
CREATE INDEX IF NOT EXISTS idx_outbound_pending ON outbound_messages(status,available_at);
CREATE TABLE IF NOT EXISTS inbound_receipts (
 peer_id BIGINT NOT NULL, message_id BIGINT NOT NULL, conversation_id UUID NOT NULL REFERENCES conversations(id),
 text TEXT NOT NULL DEFAULT '',processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY(peer_id,message_id));

CREATE TABLE IF NOT EXISTS telegram_peer_cache(peer_id BIGINT PRIMARY KEY, access_hash BIGINT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
-- History recovery advances independently of the highest live message seen.
ALTER TABLE telegram_sources ADD COLUMN IF NOT EXISTS history_message_id BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS discussion_post_links(discussion_peer_id BIGINT NOT NULL,discussion_message_id BIGINT NOT NULL,channel_peer_id BIGINT NOT NULL,channel_message_id BIGINT NOT NULL,PRIMARY KEY(discussion_peer_id,discussion_message_id));
ALTER TABLE telegram_sources ADD COLUMN IF NOT EXISTS external_llm_allowed BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS llm_usage_buckets(minute TIMESTAMPTZ PRIMARY KEY,requests INT NOT NULL DEFAULT 0);
ALTER TABLE offers ADD CONSTRAINT offer_valid_price CHECK(price_amount IS NULL OR price_amount>=0) NOT VALID;
ALTER TABLE offers ADD CONSTRAINT offer_valid_quantity CHECK(quantity>0) NOT VALID;
ALTER TABLE offers ADD CONSTRAINT offer_valid_currency CHECK(price_currency IN('IRR_RIAL','IRR_TOMAN')) NOT VALID;
ALTER TABLE offers ADD CONSTRAINT offer_valid_confidence CHECK(price_confidence BETWEEN 0 AND 1 AND extraction_confidence BETWEEN 0 AND 1) NOT VALID;

ALTER TABLE offers ADD COLUMN IF NOT EXISTS intake_updated_at TIMESTAMPTZ;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS origin_key TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS uq_conversation_origin ON conversations(origin_key) WHERE origin_key IS NOT NULL;
UPDATE conversations SET draft_key=NULL WHERE state IN('COMPLETED','DECLINED','OPTED_OUT');
ALTER TABLE offers ADD COLUMN IF NOT EXISTS warranty_known BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE offers ADD COLUMN IF NOT EXISTS variant_match BOOLEAN;
