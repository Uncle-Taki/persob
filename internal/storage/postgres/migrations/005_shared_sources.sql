-- One installation, one Telegram session and one ingestion identity per peer.
-- Conflicting legacy normalized rows require explicit operator resolution. The
-- migration transaction rolls back instead of merging histories or permissions.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM telegram_sources GROUP BY telegram_peer_id HAVING count(*)>1) THEN
  RAISE EXCEPTION 'duplicate normalized Telegram peers; resolve legacy source rows before migration 005';
 END IF;
END $$;
CREATE TABLE workspaces (
 id UUID PRIMARY KEY CHECK(id='00000000-0000-4000-8000-000000000001'),
 name TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO workspaces(id,name) VALUES('00000000-0000-4000-8000-000000000001','Perso');
ALTER TABLE telegram_sources ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-4000-8000-000000000001' REFERENCES workspaces(id);
ALTER TABLE telegram_sources ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE telegram_sources ADD COLUMN assignment_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE telegram_sources DROP CONSTRAINT uq_telegram_sources_owner_peer;
ALTER TABLE telegram_sources DROP CONSTRAINT telegram_sources_owner_id_fkey;
-- owner_id is retained as creator/legacy provenance, never an access grant.
ALTER TABLE telegram_sources ADD CONSTRAINT telegram_sources_owner_id_fkey FOREIGN KEY(owner_id) REFERENCES users(id) ON DELETE RESTRICT;
ALTER TABLE telegram_sources ADD CONSTRAINT uq_workspace_peer UNIQUE(workspace_id,telegram_peer_id);
ALTER TABLE telegram_sources ADD CONSTRAINT uq_workspace_source UNIQUE(workspace_id,id);

-- Retain the legacy allowlist/history. Promote only missing peers, paused until
-- an administrator resolves real capabilities and explicitly resumes ingestion.
INSERT INTO telegram_sources(owner_id,telegram_peer_id,username,title,source_type,detected_type,is_active,ingestion_status)
SELECT DISTINCT ON(w.telegram_chat_id) w.user_id,w.telegram_chat_id,w.username,w.title,
 CASE WHEN w.source_type='group' THEN 'basic_group' WHEN w.source_type='channel' THEN 'broadcast_channel' ELSE 'supergroup' END,
 CASE WHEN w.source_type='group' THEN 'basic_group' WHEN w.source_type='channel' THEN 'broadcast_channel' ELSE 'supergroup' END,false,'paused'
FROM watch_sources w WHERE NOT EXISTS(SELECT 1 FROM telegram_sources s WHERE s.telegram_peer_id=w.telegram_chat_id)
ORDER BY w.telegram_chat_id,w.created_at,w.id;

CREATE TABLE source_assignments (
 workspace_id UUID NOT NULL DEFAULT '00000000-0000-4000-8000-000000000001',
 source_id UUID NOT NULL, buyer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 assigned_by UUID REFERENCES users(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY(source_id,buyer_id),
 FOREIGN KEY(workspace_id,source_id) REFERENCES telegram_sources(workspace_id,id)
);
CREATE INDEX source_assignments_buyer ON source_assignments(buyer_id,source_id);
-- Only documented legacy owners receive a dormant/active grant, never new users.
INSERT INTO source_assignments(source_id,buyer_id)
SELECT s.id,s.owner_id FROM telegram_sources s JOIN users u ON u.id=s.owner_id WHERE u.role='buyer'
UNION SELECT s.id,w.user_id FROM watch_sources w JOIN telegram_sources s ON s.telegram_peer_id=w.telegram_chat_id JOIN users u ON u.id=w.user_id WHERE u.role='buyer'
ON CONFLICT DO NOTHING;

ALTER TABLE offers ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-4000-8000-000000000001' REFERENCES workspaces(id);
ALTER TABLE offers ADD COLUMN source_id UUID;
ALTER TABLE offers ADD CONSTRAINT offers_workspace_source FOREIGN KEY(workspace_id,source_id) REFERENCES telegram_sources(workspace_id,id);
-- Resolve the exact message peer only. Linked discussions never inherit their
-- parent channel's identity or grants. Unrecognized provenance stays NULL.
UPDATE offers o SET source_id=s.id FROM telegram_sources s
WHERE o.source_message_id ~ '^-?[0-9]+_[0-9]+$' AND split_part(o.source_message_id,'_',1)=s.telegram_peer_id::text;
CREATE INDEX offers_workspace_source ON offers(workspace_id,source_id);
ALTER TABLE conversations ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-4000-8000-000000000001' REFERENCES workspaces(id);
-- Buyer requests are separate from the shared seller-session record. Only
-- documented conversation ownership is backfilled; unknown ownership stays admin-only.
CREATE TABLE conversation_requests (
 conversation_id UUID NOT NULL REFERENCES conversations(id),
 user_id UUID NOT NULL REFERENCES users(id), offer_id UUID REFERENCES offers(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY(conversation_id,user_id)
);
INSERT INTO conversation_requests(conversation_id,user_id,offer_id,created_at)
SELECT id,owner_id,offer_id,created_at FROM conversations WHERE owner_id IS NOT NULL;

CREATE FUNCTION link_conversation_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.owner_id IS NOT NULL THEN
  INSERT INTO conversation_requests(conversation_id,user_id,offer_id) VALUES(NEW.id,NEW.owner_id,NEW.offer_id) ON CONFLICT DO NOTHING;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER conversation_request_on_insert AFTER INSERT ON conversations FOR EACH ROW EXECUTE FUNCTION link_conversation_request();
