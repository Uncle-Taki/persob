package telegram

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/google/uuid"
	"github.com/gotd/td/tg"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"log/slog"
	"strconv"
	"time"
)

func (c *Collector) SetMarketplaceRepo(db *postgres.DB) { c.marketDB = db }
func (c *Collector) cacheEntities(ctx context.Context, e tg.Entities) error {
	if c.marketDB == nil {
		return nil
	}
	for id, user := range e.Users {
		if user.AccessHash == 0 {
			continue
		}
		if _, err := c.marketDB.Pool.Exec(ctx, `INSERT INTO telegram_peer_cache(peer_id,access_hash) VALUES($1,$2) ON CONFLICT(peer_id) DO UPDATE SET access_hash=EXCLUDED.access_hash,updated_at=NOW()`, id, user.AccessHash); err != nil {
			return err
		}
	}
	return nil
}
func (c *Collector) runOutbox(ctx context.Context) {
	// Demo-auth and mock Telegram modes must never fall through to a real
	// MTProto client. A mock mode without its mock transport fails closed.
	if c.cfg.DemoAuthEnabled || (c.cfg.TelegramMockMode && c.mockClient == nil) {
		return
	}
	if c.marketDB == nil || !c.cfg.OutreachEnabled {
		return
	}
	interval := c.cfg.OutboundInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.dispatchOne(ctx); err != nil {
				slog.Warn("outbound dispatch", "error", err)
			}
		}
	}
}
func (c *Collector) dispatchOne(ctx context.Context) error {
	if c.cfg.DemoAuthEnabled || (c.cfg.TelegramMockMode && c.mockClient == nil) {
		return fmt.Errorf("outbound delivery is disabled in demo or unconfigured mock mode")
	}
	// Serialize eligibility and send against pause/opt-out transitions. The
	// transaction has a bounded lifetime; RandomID is stable across redelivery.
	if c.cfg.InQuietHours(time.Now()) {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := c.marketDB.Pool.Begin(callCtx)
	if err != nil {
		return err
	}
	defer tx.Rollback(callCtx)
	var id, conversation uuid.UUID
	var seller int64
	var text string
	err = tx.QueryRow(callCtx, `SELECT id,conversation_id,seller_id,text FROM outbound_messages WHERE status IN ('queued','failed') AND attempts<3 AND available_at<=NOW() ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &conversation, &seller, &text)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil
		}
		return err
	}
	var eligible bool
	err = tx.QueryRow(callCtx, `SELECT c.state IN ('AWAITING_CONSENT','WAITING_FOR_REPLY','ACTIVE') AND c.approved_at IS NOT NULL
 AND NOT COALESCE(sp.opted_out,false)
 AND EXISTS(SELECT 1 FROM users u WHERE u.id=c.owner_id AND u.active=true AND u.role='buyer')
 AND EXISTS(SELECT 1 FROM interests i WHERE i.user_id=c.owner_id AND i.product_id=c.product_id AND i.buy_enabled=true)
 AND EXISTS(
   SELECT 1 FROM offers o
   JOIN telegram_sources ts ON ts.id=o.source_id AND ts.workspace_id=o.workspace_id
   JOIN source_assignments sa ON sa.workspace_id=o.workspace_id AND sa.source_id=o.source_id AND sa.buyer_id=c.owner_id
   WHERE o.id=c.offer_id AND ts.is_active=true AND ts.deleted_at IS NULL AND ts.ingestion_status='active'
 )
 FROM conversations c LEFT JOIN seller_profiles sp ON sp.seller_id=c.seller_id WHERE c.id=$1 FOR UPDATE OF c`, conversation).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		_, err = tx.Exec(callCtx, `UPDATE outbound_messages SET status='cancelled',last_error='Outreach no longer eligible' WHERE id=$1`, id)
		if err != nil {
			return err
		}
		return tx.Commit(callCtx)
	}
	var telegramID int64
	if c.mockClient != nil {
		err = c.mockClient.SendMessage(callCtx, seller, text)
	} else {
		var hash int64
		err = tx.QueryRow(callCtx, `SELECT access_hash FROM telegram_peer_cache WHERE peer_id=$1`, seller).Scan(&hash)
		if err == nil {
			c.mu.Lock()
			client := c.tgClient
			c.mu.Unlock()
			if client == nil {
				return ErrResolverUnavailable
			}
			var updates tg.UpdatesClass
			updates, err = client.API().MessagesSendMessage(callCtx, &tg.MessagesSendMessageRequest{Peer: &tg.InputPeerUser{UserID: seller, AccessHash: hash}, Message: text, RandomID: int64(binary.BigEndian.Uint64(id[:8]) & 0x7fffffffffffffff)})
			if err == nil {
				switch u := updates.(type) {
				case *tg.UpdateShortSentMessage:
					telegramID = int64(u.ID)
				case *tg.Updates:
					for _, update := range u.Updates {
						if msg, ok := update.(*tg.UpdateMessageID); ok {
							telegramID = int64(msg.ID)
						}
					}
				}
			}
		}
	}
	if err != nil {
		// The RPC deadline may already have expired. Persist failure with a fresh
		// bounded context, so timeouts cannot reset attempts and spin forever.
		_ = tx.Rollback(context.WithoutCancel(ctx))
		recordCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer done()
		delay := 60
		if match := floodWaitRegex.FindStringSubmatch(err.Error()); len(match) == 2 {
			if seconds, parseErr := strconv.Atoi(match[1]); parseErr == nil && seconds > delay {
				delay = seconds
			}
		}
		_, recordErr := c.marketDB.Pool.Exec(recordCtx, `UPDATE outbound_messages SET status='failed',attempts=attempts+1,last_error='Telegram send failed; retry scheduled',available_at=NOW()+($2*INTERVAL '1 second') WHERE id=$1 AND status IN('queued','failed')`, id, delay)
		if recordErr != nil {
			return recordErr
		}
		return fmt.Errorf("outbound %s failed", id)
	}

	if _, err = tx.Exec(callCtx, `UPDATE outbound_messages SET status='sent',attempts=attempts+1,last_error=NULL,telegram_message_id=$2,sent_at=NOW() WHERE id=$1`, id, telegramID); err != nil {
		return err
	}
	return tx.Commit(callCtx)
}

func (c *Collector) maintainRetention(ctx context.Context) {
	if c.cfg.RetentionDays <= 0 {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.retentionHandler != nil {
				if err := c.retentionHandler(ctx); err != nil {
					slog.Error("message retention", "error", err)
				}
			}
			if c.marketDB != nil {
				cutoff := time.Now().Add(-time.Duration(c.cfg.RetentionDays) * 24 * time.Hour)
				for _, query := range []string{`DELETE FROM inbound_receipts WHERE processed_at<$1 AND conversation_id IN(SELECT id FROM conversations WHERE state IN('COMPLETED','DECLINED','OPTED_OUT'))`, `DELETE FROM outbound_messages WHERE created_at<$1 AND conversation_id IN(SELECT id FROM conversations WHERE state IN('COMPLETED','DECLINED','OPTED_OUT'))`, `DELETE FROM audit_logs WHERE created_at<$1`} {
					if _, err := c.marketDB.Pool.Exec(ctx, query, cutoff); err != nil {
						slog.Error("history retention", "error", err)
					}
				}
			}
		}
	}
}
