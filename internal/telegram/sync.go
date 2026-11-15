package telegram

import (
	"context"
	"fmt"
	"github.com/gotd/td/tg"
	"github.com/perso/perso-mind/internal/domain"
	"log/slog"
	"time"
)

// syncSources bounds each scan, preserves overlap for edits and resumes only from
// a fully processed history page. The cursor is not advanced by live callbacks.
func (c *Collector) syncSources(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if c.cfg.TelegramBackfillLimit > 0 && c.telegramSourceRepo != nil {
			sources, err := c.telegramSourceRepo.ListActiveSources(ctx)
			if err != nil {
				slog.Error("source sync list", "error", err)
			} else {
				for _, s := range sources {
					if s.OwnerID != *c.cfg.OwnerID() {
						continue
					}
					if err = c.syncSource(ctx, s); err != nil {
						slog.Warn("source sync failed", "source_id", s.ID, "error", err)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (c *Collector) syncSource(ctx context.Context, s *domain.TelegramSource) error {
	c.mu.Lock()
	client := c.tgClient
	c.mu.Unlock()
	if client == nil {
		return ErrResolverUnavailable
	}
	var peer tg.InputPeerClass
	if s.SourceType == domain.TelegramSourceTypeBasicGroup {
		peer = &tg.InputPeerChat{ChatID: -s.TelegramPeerID}
	} else {
		peer = &tg.InputPeerChannel{ChannelID: -s.TelegramPeerID - 1000000000000, AccessHash: s.TelegramAccessHash}
	}
	checkpoint, err := c.telegramSourceRepo.HistoryCheckpoint(ctx, s.ID)
	if err != nil {
		return err
	}
	offset := 0
	seen := 0
	newest := checkpoint
	complete := false
	for seen < c.cfg.TelegramBackfillLimit {
		batch, err := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, OffsetID: offset, Limit: min(100, c.cfg.TelegramBackfillLimit-seen)})
		if err != nil {
			if handled, waitErr := HandleFloodWait(ctx, err); handled && waitErr == nil {
				continue
			}
			return fmt.Errorf("history unavailable")
		}
		data, ok := batch.(interface {
			GetMessages() []tg.MessageClass
			GetUsers() []tg.UserClass
			GetChats() []tg.ChatClass
		})
		if !ok {
			return fmt.Errorf("unexpected history response")
		}
		entities := tg.Entities{Users: map[int64]*tg.User{}, Chats: map[int64]*tg.Chat{}, Channels: map[int64]*tg.Channel{}}
		for _, u := range data.GetUsers() {
			if v, ok := u.(*tg.User); ok {
				entities.Users[v.ID] = v
			}
		}
		for _, chat := range data.GetChats() {
			switch v := chat.(type) {
			case *tg.Chat:
				entities.Chats[v.ID] = v
			case *tg.Channel:
				entities.Channels[v.ID] = v
			}
		}
		list := data.GetMessages()
		if len(list) == 0 {
			complete = true
			break
		}
		for i := len(list) - 1; i >= 0; i-- {
			if msg, ok := list[i].(*tg.Message); ok {
				if int64(msg.ID) > newest {
					newest = int64(msg.ID)
				}
				if err = c.handleTelegramMessageClass(ctx, entities, msg, msg.EditDate != 0); err != nil {
					return err
				}
			}
		}
		for _, v := range list {
			if msg, ok := v.(*tg.Message); ok {
				if offset == 0 || msg.ID < offset {
					offset = msg.ID
				}
			}
		}
		seen += len(list)
		if len(list) < 100 || (checkpoint > 0 && int64(offset) <= checkpoint) {
			complete = true
			break
		}
	}
	if !complete && checkpoint > 0 {
		return fmt.Errorf("history gap exceeds configured backfill limit; cursor preserved")
	}
	return c.telegramSourceRepo.CompleteHistoryScan(ctx, s.ID, newest)
}
