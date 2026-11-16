package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/nlp"
	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

var floodWaitRegex = regexp.MustCompile(`FLOOD_WAIT_(\d+)`)

type Collector struct {
	retentionHandler func(context.Context) error

	marketDB *postgres.DB

	cfg                *config.Config
	sessionStorage     *EncryptedSessionStorage
	watchSourceRepo    *postgres.WatchSourceRepo
	telegramSourceRepo *postgres.TelegramSourceRepo
	messagesRepo       *mongodb.MessagesRepo
	updatesRepo        *mongodb.UpdatesRepo
	producer           *queue.Producer
	mockClient         *MockClient
	tgClient           *telegram.Client
	resolver           *Resolver
	authorized         bool
	replyHandler       func(context.Context, *tg.Message) error
	running            bool
	mu                 sync.Mutex
}

func NewCollector(
	cfg *config.Config,
	watchSourceRepo *postgres.WatchSourceRepo,
	telegramSourceRepo *postgres.TelegramSourceRepo,
	messagesRepo *mongodb.MessagesRepo,
	updatesRepo *mongodb.UpdatesRepo,
	producer *queue.Producer,
) (*Collector, error) {
	sessionStorage, err := NewEncryptedSessionStorage(
		cfg.TelegramSessionDir,
		"perso_session.dat",
		cfg.TelegramSessionStorageKey,
	)
	if err != nil {
		return nil, fmt.Errorf("init session storage: %w", err)
	}

	c := &Collector{
		cfg:                cfg,
		sessionStorage:     sessionStorage,
		watchSourceRepo:    watchSourceRepo,
		telegramSourceRepo: telegramSourceRepo,
		messagesRepo:       messagesRepo,
		updatesRepo:        updatesRepo,
		producer:           producer,
	}

	if cfg.TelegramMockMode {
		slog.Info("initializing explicitly configured mock telegram client")
		c.mockClient = NewMockClient()
		c.mockClient.SetMessageHandler(c.processIncomingMessage)
		c.resolver = NewResolver(nil, true)
	} else {
		c.resolver = NewResolver(nil, false)
	}

	return c, nil
}

// Start runs the collector until ctx is canceled
func (c *Collector) Start(ctx context.Context) error {
	if c.mockClient == nil && c.cfg.TelegramAppID <= 0 {
		return fmt.Errorf("real Telegram ingestion requires credentials; mock mode must be explicit")
	}
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("collector is already running")
	}
	c.running = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		_ = c.sessionStorage.UnlockSession()
	}()

	// Lock session to prevent multiple collectors running on same session file
	if err := c.sessionStorage.LockSession(); err != nil {
		return fmt.Errorf("acquire session lock: %w", err)
	}

	if c.mockClient != nil {
		return c.runMockLoop(ctx)
	}

	go c.reconcileDispatch(ctx)
	return c.runMTProtoLoop(ctx)
}

func (c *Collector) runMockLoop(ctx context.Context) error {
	go c.reconcileDispatch(ctx)
	go c.runOutbox(ctx)
	slog.Info("starting telegram mock collector loop")
	_ = c.mockClient.Start(ctx)

	// Emit synthetic initial demo marketplace message for verification
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
			demoMsg := &domain.TelegramMessage{
				TelegramChatID:    -1001234567890,
				TelegramMessageID: 101,
				ChatTitle:         "بازار موبایل و لپتاپ پایتخت",
				ChatType:          "channel",
				SenderID:          987654321,
				SenderUsername:    "tehran_apple_store",
				SenderDisplayName: "فروشگاه اپل استور علاءالدین",
				MessageText:       "فروش فوری آیفون ۱۵ پرو ۲۵۶ گیگ رنگ نچرال تیتانیوم بسیار تمیز، قیمت ۹۸ میلیون تومان. علاءالدین طبقه ۲",
				MessageDate:       time.Now(),
				ReceivedAt:        time.Now(),
			}
			if c.cfg.TelegramMockHealthcare {
				demoMsg.ChatTitle = "بازار آزمایشی سلامت"
				demoMsg.SenderUsername = "perso_synthetic_supplier"
				demoMsg.SenderDisplayName = "فروشنده آزمایشی"
				demoMsg.MessageText = "فروش سرم نرمال سالین 500ml تعداد ۱۲ عدد، ۲۵۰۰۰ تومان، تهران پاستور"
			}
			slog.Info("injecting mock demo marketplace message...")
			if err := c.mockClient.InjectMessage(ctx, demoMsg); err != nil {
				slog.Error("failed to inject demo message", "error", err)
			}
		}
	}()

	<-ctx.Done()
	_ = c.mockClient.Stop()
	return nil
}

func (c *Collector) runMTProtoLoop(ctx context.Context) error {
	slog.Info("starting gotd MTProto telegram collector...")

	dispatcher := tg.NewUpdateDispatcher()

	dispatcher.OnAnyNewMessage(func(msgCtx context.Context, e tg.Entities, msg tg.MessageClass) error {
		return c.handleTelegramMessageClass(msgCtx, e, msg, false)
	})

	dispatcher.OnAnyEditMessage(func(msgCtx context.Context, e tg.Entities, msg tg.MessageClass) error {
		return c.handleTelegramMessageClass(msgCtx, e, msg, true)
	})

	dispatcher.OnDeleteChannelMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		return c.deleteMessages(ctx, -1000000000000-u.ChannelID, u.Messages)
	})
	opts := telegram.Options{
		SessionStorage: c.sessionStorage,
		UpdateHandler:  dispatcher,
	}

	client := telegram.NewClient(c.cfg.TelegramAppID, c.cfg.TelegramAppHash, opts)
	c.mu.Lock()
	c.tgClient = client
	c.resolver = NewResolver(client, false)
	c.mu.Unlock()

	return client.Run(ctx, func(runCtx context.Context) error {
		// Check authorization status
		status, err := client.Auth().Status(runCtx)
		if err != nil {
			return fmt.Errorf("check telegram auth status: %w", err)
		}

		if !status.Authorized {
			slog.Warn("telegram client is not authorized. MTProto session required via CLI auth.")
			// In production, user authorizes via separate login tool or CLI
			<-runCtx.Done()
			return runCtx.Err()
		}

		c.mu.Lock()
		c.authorized = true
		c.mu.Unlock()
		defer func() { c.mu.Lock(); c.authorized = false; c.mu.Unlock() }()
		go c.syncSources(runCtx)
		go c.maintainRetention(runCtx)
		go c.runOutbox(runCtx)
		slog.Info("telegram client authorized successfully and listening for updates")
		<-runCtx.Done()
		return runCtx.Err()
	})
}

func (c *Collector) handleTelegramMessageClass(ctx context.Context, e tg.Entities, msgClass tg.MessageClass, isEdit bool) error {
	msg, ok := msgClass.(*tg.Message)
	if !ok || msg.Out {
		// Ignore non-standard messages or outgoing messages
		return nil
	}

	if err := c.cacheEntities(ctx, e); err != nil {
		return err
	}
	if _, private := msg.PeerID.(*tg.PeerUser); private {
		if c.replyHandler != nil {
			return c.replyHandler(ctx, msg)
		}
		return nil
	}
	peerID := extractPeerID(msg.PeerID)
	if peerID == 0 {
		return nil
	}

	// 1. Check normalized telegram_sources registry
	var matchedSource *domain.TelegramSource
	if c.telegramSourceRepo != nil {
		configured, active, err := c.telegramSourceRepo.PeerMonitoringStatus(ctx, peerID)
		if err != nil {
			return fmt.Errorf("check normalized source: %w", err)
		}
		if configured && !active {
			return nil
		}
		activeSources, err := c.telegramSourceRepo.ListActiveSources(ctx)
		if err == nil {
			for _, s := range activeSources {
				if s.TelegramPeerID == peerID {
					matchedSource = s
					break
				}
				if s.LinkedDiscussionPeerID != nil && *s.LinkedDiscussionPeerID == peerID {
					matchedSource = s
				}
			}
		}
	}

	// 2. Fallback check to legacy watch sources
	if matchedSource == nil && c.watchSourceRepo != nil {
		monitored, err := c.watchSourceRepo.IsChatMonitored(ctx, peerID)
		if err != nil {
			slog.Error("error checking watch source monitoring status", "chat_id", peerID, "error", err)
			return nil
		}
		if !monitored && c.mockClient == nil {
			return nil
		}
	}

	// 3. Adapt message using type-specific adapter
	isDiscussion := false
	sourceType := domain.TelegramSourceTypeSupergroup
	if matchedSource != nil {
		sourceType = matchedSource.SourceType
		if matchedSource.LinkedDiscussionPeerID != nil && *matchedSource.LinkedDiscussionPeerID == peerID {
			isDiscussion = true
		}
	}

	adapter := GetIngestionAdapter(sourceType, isDiscussion)
	telegramMsg, err := adapter.AdaptMessage(ctx, e, msg, matchedSource)
	if err != nil {
		slog.Error("failed to adapt telegram message", "error", err)
		return nil
	}

	if isDiscussion && c.marketDB != nil {
		if telegramMsg.LinkedPostID != nil {
			if _, err = c.marketDB.Pool.Exec(ctx, `INSERT INTO discussion_post_links VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, peerID, msg.ID, matchedSource.TelegramPeerID, *telegramMsg.LinkedPostID); err != nil {
				return err
			}
		} else if header, ok := msg.ReplyTo.(*tg.MessageReplyHeader); ok {
			root := header.ReplyToTopID
			if root == 0 {
				root = header.ReplyToMsgID
			}
			var original int64
			lookupErr := c.marketDB.Pool.QueryRow(ctx, `SELECT channel_message_id FROM discussion_post_links WHERE discussion_peer_id=$1 AND discussion_message_id=$2 AND channel_peer_id=$3`, peerID, root, matchedSource.TelegramPeerID).Scan(&original)
			if lookupErr == nil {
				telegramMsg.LinkedPostID = &original
			}
		}
	}
	if isEdit && msg.EditDate != 0 {
		t := time.Unix(int64(msg.EditDate), 0)
		telegramMsg.EditedDate = &t
	}

	if err := c.processIncomingMessage(ctx, telegramMsg); err != nil {
		return err
	}

	if matchedSource != nil && c.telegramSourceRepo != nil && !isDiscussion {
		return c.telegramSourceRepo.UpdateSyncCheckpoint(ctx, matchedSource.ID, int64(msg.ID), time.Now().UTC())
	}

	return nil
}

func (c *Collector) processIncomingMessage(ctx context.Context, msg *domain.TelegramMessage) error {

	// The normalized registry takes precedence, including pauses. Fall back only
	// for peers which have no normalized source configuration.
	var configured, monitored bool
	var err error
	if c.telegramSourceRepo != nil {
		configured, monitored, err = c.telegramSourceRepo.PeerMonitoringStatus(ctx, msg.TelegramChatID)
		if err != nil {
			return fmt.Errorf("check normalized source: %w", err)
		}
	}
	if !configured && c.watchSourceRepo != nil {
		monitored, err = c.watchSourceRepo.IsChatMonitored(ctx, msg.TelegramChatID)
		if err != nil {
			return fmt.Errorf("check legacy source: %w", err)
		}
	}
	if !monitored && (configured || c.mockClient == nil) {
		return nil
	}

	// Bind raw provenance to the exact registered peer. Parent channel grants
	// must never leak linked-discussion text. Derive identity server-side even for
	// mock/legacy payloads, rather than trusting the supplied source_id.
	msg.SourceID = ""
	if c.telegramSourceRepo != nil {
		sourceID, err := c.telegramSourceRepo.SourceForPeer(ctx, msg.TelegramChatID)
		if err != nil {
			return err
		}
		if sourceID != nil {
			msg.SourceID = sourceID.String()
		}
	}
	// 2. Pre-normalize text for search indexing
	msg.NormalizedText = nlp.Normalize(msg.MessageText)

	// 3. Upsert into MongoDB raw messages collection (idempotent, original text preserved)
	if err := c.messagesRepo.UpsertMessage(ctx, msg); err != nil {
		slog.Error("failed to upsert raw telegram message into mongodb", "error", err)
		return err
	}

	// 4. Publish to Redis Stream for worker processing
	streamID, err := c.producer.PublishMessage(ctx, msg)
	if err != nil {
		slog.Error("failed to publish message to redis stream", "error", err)
		return err
	}

	if err := c.messagesRepo.MarkDispatched(ctx, msg); err != nil {
		return err
	}
	slog.Info("ingested telegram message successfully",
		"chat_id", msg.TelegramChatID,
		"msg_id", msg.TelegramMessageID,
		"stream_id", streamID,
	)

	return nil
}

func extractPeerID(peer tg.PeerClass) int64 {
	switch p := peer.(type) {
	case *tg.PeerChannel:
		return -1000000000000 - p.ChannelID
	case *tg.PeerChat:
		return -p.ChatID
	case *tg.PeerUser:
		return p.UserID
	default:
		return 0
	}
}

func extractPeerType(peer tg.PeerClass) string {
	switch peer.(type) {
	case *tg.PeerChannel:
		return "channel"
	case *tg.PeerChat:
		return "group"
	case *tg.PeerUser:
		return "user"
	default:
		return "unknown"
	}
}

// HandleFloodWait checks if an error is a Telegram flood wait error and waits if applicable
func HandleFloodWait(ctx context.Context, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	m := floodWaitRegex.FindStringSubmatch(err.Error())
	if len(m) > 1 {
		seconds, _ := strconv.Atoi(m[1])
		slog.Warn("telegram flood wait encountered", "wait_seconds", seconds)
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-time.After(time.Duration(seconds) * time.Second):
			return true, nil
		}
	}
	return false, nil
}

func (c *Collector) MockClient() *MockClient {
	return c.mockClient
}

func (c *Collector) Resolver() *Resolver {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resolver
}

func (c *Collector) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authorized || c.mockClient != nil
}
func (c *Collector) SetReplyHandler(h func(context.Context, *tg.Message) error) { c.replyHandler = h }
func (c *Collector) reconcileDispatch(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msgs, err := c.messagesRepo.GetUndispatched(ctx)
			if err != nil {
				slog.Error("dispatch reconciliation", "error", err)
				continue
			}
			for i := range msgs {
				if _, err = c.producer.PublishMessage(ctx, &msgs[i]); err != nil {
					break
				}
				if err = c.messagesRepo.MarkDispatched(ctx, &msgs[i]); err != nil {
					slog.Error("dispatch marker", "error", err)
				}
			}
		}
	}
}

func (c *Collector) deleteMessages(ctx context.Context, peer int64, ids []int) error {
	for _, id := range ids {
		msg, err := c.messagesRepo.GetByChatAndMessageID(ctx, peer, int64(id))
		if err != nil {
			return err
		}
		if msg == nil {
			continue
		}
		now := time.Now().UTC()
		msg.Deleted = true
		msg.EditedDate = &now
		if err = c.processIncomingMessage(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func (c *Collector) SetRetentionHandler(h func(context.Context) error) { c.retentionHandler = h }
