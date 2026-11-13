package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
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
	cfg             *config.Config
	sessionStorage  *EncryptedSessionStorage
	watchSourceRepo *postgres.WatchSourceRepo
	messagesRepo    *mongodb.MessagesRepo
	updatesRepo     *mongodb.UpdatesRepo
	producer        *queue.Producer
	mockClient      *MockClient
	tgClient        *telegram.Client
	running         bool
	mu              sync.Mutex
}

func NewCollector(
	cfg *config.Config,
	watchSourceRepo *postgres.WatchSourceRepo,
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
		cfg:             cfg,
		sessionStorage:  sessionStorage,
		watchSourceRepo: watchSourceRepo,
		messagesRepo:    messagesRepo,
		updatesRepo:     updatesRepo,
		producer:        producer,
	}

	if cfg.TelegramMockMode || cfg.TelegramAppID == 0 {
		slog.Info("initializing mock telegram client (mock mode or missing AppID)")
		c.mockClient = NewMockClient()
		c.mockClient.SetMessageHandler(c.processIncomingMessage)
	}

	return c, nil
}

// Start runs the collector until ctx is canceled
func (c *Collector) Start(ctx context.Context) error {
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

	return c.runMTProtoLoop(ctx)
}

func (c *Collector) runMockLoop(ctx context.Context) error {
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

	opts := telegram.Options{
		SessionStorage: c.sessionStorage,
		UpdateHandler:  dispatcher,
	}

	client := telegram.NewClient(c.cfg.TelegramAppID, c.cfg.TelegramAppHash, opts)
	c.tgClient = client

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

	chatID := extractPeerID(msg.PeerID)
	if chatID == 0 {
		return nil
	}

	// Compliance Check: verify chat is in configured watch sources
	monitored, err := c.watchSourceRepo.IsChatMonitored(ctx, chatID)
	if err != nil {
		slog.Error("error checking watch source monitoring status", "chat_id", chatID, "error", err)
		return nil
	}

	if !monitored {
		// Ignore unconfigured channels/groups
		return nil
	}

	// Extract sender info
	var senderID int64
	var senderUsername, senderDisplayName string

	if msg.FromID != nil {
		senderID = extractPeerID(msg.FromID)
		if u, found := e.Users[senderID]; found {
			senderUsername = u.Username
			senderDisplayName = strings.TrimSpace(u.FirstName + " " + u.LastName)
		}
	}

	var editDate *time.Time
	if isEdit && msg.EditDate != 0 {
		t := time.Unix(int64(msg.EditDate), 0)
		editDate = &t
	}

	telegramMsg := &domain.TelegramMessage{
		TelegramChatID:    chatID,
		TelegramMessageID: int64(msg.ID),
		ChatType:          extractPeerType(msg.PeerID),
		SenderID:          senderID,
		SenderUsername:    senderUsername,
		SenderDisplayName: senderDisplayName,
		MessageText:       msg.Message,
		MessageDate:       time.Unix(int64(msg.Date), 0),
		EditedDate:        editDate,
		ReceivedAt:        time.Now(),
		ProcessingStatus:  domain.ProcessingStatusPending,
		ProcessingVersion: 1,
	}

	return c.processIncomingMessage(ctx, telegramMsg)
}

func (c *Collector) processIncomingMessage(ctx context.Context, msg *domain.TelegramMessage) error {
	// 1. Double check monitored source
	monitored, err := c.watchSourceRepo.IsChatMonitored(ctx, msg.TelegramChatID)
	if err != nil {
		slog.Error("watch source check error", "chat_id", msg.TelegramChatID, "error", err)
	}
	if !monitored && c.mockClient == nil {
		slog.Debug("ignoring message from unmonitored chat", "chat_id", msg.TelegramChatID)
		return nil
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
