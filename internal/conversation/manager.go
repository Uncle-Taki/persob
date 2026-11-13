package conversation

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/llm"
	"github.com/perso/perso-mind/internal/nlp"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"github.com/perso/perso-mind/internal/telegram"
)

var (
	botInquiryRegex = regexp.MustCompile(`(?i)(رباتی|ربات هستی|هوش مصنوعی|ai|بات هستی|آدمی|انسان هستی)`)
	optOutRegex     = regexp.MustCompile(`(?i)(مزاحم نشو|پیام نده|لغو|بلاک|نمیخوام|دیگه نپرس|استپ|stop)`)
)

type Manager struct {
	cfg            *config.Config
	pgConvRepo     *postgres.ConversationRepo
	mongoConvRepo  *mongodb.ConversationRepo
	auditRepo      *postgres.AuditRepo
	telegramClient telegram.TelegramClient
	llmProvider    llm.Provider
}

func NewManager(
	cfg *config.Config,
	pgConvRepo *postgres.ConversationRepo,
	mongoConvRepo *mongodb.ConversationRepo,
	auditRepo *postgres.AuditRepo,
	telegramClient telegram.TelegramClient,
	llmProvider llm.Provider,
) *Manager {
	return &Manager{
		cfg:            cfg,
		pgConvRepo:     pgConvRepo,
		mongoConvRepo:  mongoConvRepo,
		auditRepo:      auditRepo,
		telegramClient: telegramClient,
		llmProvider:    llmProvider,
	}
}

// InitConversation creates a conversation for an offer with missing fields
func (m *Manager) InitConversation(
	ctx context.Context,
	offer *domain.Offer,
	missingFields []domain.MissingField,
) (*domain.Conversation, error) {
	if len(missingFields) == 0 {
		return nil, nil // Nothing to ask
	}

	initialState := domain.ConvStateAwaitingApproval
	if m.cfg.ConversationAutoApprove {
		initialState = domain.ConvStateAwaitingConsent
	}

	conv := &domain.Conversation{
		ID:                  uuid.New(),
		OfferID:             &offer.ID,
		SellerID:            offer.SellerID,
		ProductID:           offer.ProductID,
		State:               initialState,
		ConsentGranted:      false,
		QuestionsAskedCount: 0,
		MaxQuestions:        m.cfg.ConversationMaxQuestions,
		MissingFields:       missingFields,
		CollectedData:       make(map[string]any),
		Notes:               "Created for missing fields extraction",
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	if err := m.pgConvRepo.Create(ctx, conv); err != nil {
		return nil, fmt.Errorf("create conversation in postgres: %w", err)
	}

	_ = m.auditRepo.Log(ctx, "system", "conversation_created", "conversation", conv.ID.String(), conv, "")

	slog.Info("initialized seller conversation",
		"conversation_id", conv.ID,
		"seller_id", conv.SellerID,
		"state", conv.State,
		"missing_fields", conv.MissingFields,
	)

	// If auto-approved, send consent question right away
	if conv.State == domain.ConvStateAwaitingConsent && m.telegramClient != nil {
		_ = m.sendConsentRequest(ctx, conv, offer)
	}

	return conv, nil
}

// Approve transitions an outreach draft from AWAITING_APPROVAL to ACTIVE / AWAITING_CONSENT
func (m *Manager) Approve(ctx context.Context, convID uuid.UUID, userID uuid.UUID) error {
	conv, err := m.pgConvRepo.GetByID(ctx, convID)
	if err != nil {
		return fmt.Errorf("get conversation: %w", err)
	}
	if conv == nil {
		return fmt.Errorf("conversation not found: %s", convID)
	}

	if err := Transition(conv.State, domain.ConvStateActive); err != nil {
		return err
	}

	if err := m.pgConvRepo.Approve(ctx, convID, userID); err != nil {
		return fmt.Errorf("approve conversation: %w", err)
	}

	_ = m.auditRepo.Log(ctx, userID.String(), "conversation_approved", "conversation", convID.String(), nil, "")

	slog.Info("conversation approved by user", "conversation_id", convID, "user_id", userID)

	// Send consent or first question
	if m.telegramClient != nil && !conv.ConsentGranted {
		_ = m.sendConsentRequest(ctx, conv, conv.Offer)
	}

	return nil
}

// Pause transitions a conversation to PAUSED
func (m *Manager) Pause(ctx context.Context, convID uuid.UUID) error {
	conv, err := m.pgConvRepo.GetByID(ctx, convID)
	if err != nil || conv == nil {
		return fmt.Errorf("conversation not found")
	}

	if err := Transition(conv.State, domain.ConvStatePaused); err != nil {
		return err
	}

	return m.pgConvRepo.UpdateState(ctx, convID, domain.ConvStatePaused, "Paused by user")
}

// Close transitions a conversation to COMPLETED
func (m *Manager) Close(ctx context.Context, convID uuid.UUID) error {
	conv, err := m.pgConvRepo.GetByID(ctx, convID)
	if err != nil || conv == nil {
		return fmt.Errorf("conversation not found")
	}

	if err := Transition(conv.State, domain.ConvStateCompleted); err != nil {
		return err
	}

	return m.pgConvRepo.UpdateState(ctx, convID, domain.ConvStateCompleted, "Closed by user")
}

// HandleIncomingSellerMessage processes a message from the seller
func (m *Manager) HandleIncomingSellerMessage(ctx context.Context, convID uuid.UUID, text string) (string, error) {
	conv, err := m.pgConvRepo.GetByID(ctx, convID)
	if err != nil || conv == nil {
		return "", fmt.Errorf("conversation not found")
	}

	// 1. Record incoming seller message in MongoDB history
	_ = m.mongoConvRepo.SaveMessage(ctx, &domain.ConversationMessage{
		ConversationID: convID.String(),
		Sender:         domain.SenderSeller,
		Text:           text,
		SentAt:         time.Now(),
		Delivered:      true,
	})

	norm := nlp.CleanSearchKey(text)

	// 2. Check for Opt-Out / Refusal
	if optOutRegex.MatchString(norm) {
		_ = m.pgConvRepo.UpdateState(ctx, convID, domain.ConvStateOptedOut, "Seller opted out")
		reply := "درخواست شما ثبت شد. دیگر پیامی از طرف پرسو برای شما ارسال نخواهد شد. با احترام."
		m.sendAssistantMessage(ctx, conv, reply)
		return reply, nil
	}

	// 3. Check for Bot Inquiry (Honest AI identity disclosure)
	if botInquiryRegex.MatchString(norm) {
		reply := "بله، من دستیار هوشمند و خودکار پلتفرم پرسو هستم که برای تکمیل مشخصات آگهی‌های بازار الکترونیک به خریداران و فروشندگان کمک می‌کنم."
		m.sendAssistantMessage(ctx, conv, reply)
		return reply, nil
	}

	// 4. Handle State: Awaiting Consent
	if conv.State == domain.ConvStateAwaitingConsent {
		consented, _ := m.llmProvider.ValidateConsentResponse(ctx, text)
		if consented {
			conv.ConsentGranted = true
			_ = m.pgConvRepo.UpdateState(ctx, convID, domain.ConvStateActive, "Seller consented")
			return m.askNextQuestion(ctx, conv)
		} else {
			_ = m.pgConvRepo.UpdateState(ctx, convID, domain.ConvStateDeclined, "Seller declined consent")
			reply := "متشکرم، وقت شما به‌خیر."
			m.sendAssistantMessage(ctx, conv, reply)
			return reply, nil
		}
	}

	// 5. Handle State: Waiting for Reply
	if conv.State == domain.ConvStateWaitingForReply {
		// Parse answer for current missing field
		if len(conv.MissingFields) > 0 {
			target := conv.MissingFields[0]
			m.extractFieldFromAnswer(text, target, conv.CollectedData)
			// Remove from missing fields
			conv.MissingFields = conv.MissingFields[1:]
			_ = m.pgConvRepo.UpdateCollectedData(ctx, convID, conv.CollectedData, conv.MissingFields)
		}

		// Check if done
		if len(conv.MissingFields) == 0 || conv.QuestionsAskedCount >= conv.MaxQuestions {
			_ = m.pgConvRepo.UpdateState(ctx, convID, domain.ConvStateCompleted, "All fields collected or max questions reached")
			reply := "خیلی ممنون از پاسخ‌های دقیق شما! اطلاعات آگهی شما در سامانه پرسو تکمیل شد."
			m.sendAssistantMessage(ctx, conv, reply)
			return reply, nil
		}

		// Continue with next question
		return m.askNextQuestion(ctx, conv)
	}

	return "", nil
}

func (m *Manager) sendConsentRequest(ctx context.Context, conv *domain.Conversation, offer *domain.Offer) error {
	productName := "کالای شما"
	if conv.Product != nil {
		productName = conv.Product.Name
	}

	msg := fmt.Sprintf(
		"سلام! من دستیار خودکار پرسو هستم. برای تکمیل اطلاعات آگهی شما درباره %s و نمایش بهتر به خریداران، مایل هستید چند سؤال کوتاه بپرسم؟",
		productName,
	)

	m.sendAssistantMessage(ctx, conv, msg)
	return m.pgConvRepo.UpdateState(ctx, conv.ID, domain.ConvStateAwaitingConsent, "Sent consent request")
}

func (m *Manager) askNextQuestion(ctx context.Context, conv *domain.Conversation) (string, error) {
	if len(conv.MissingFields) == 0 || conv.QuestionsAskedCount >= conv.MaxQuestions {
		_ = m.pgConvRepo.UpdateState(ctx, conv.ID, domain.ConvStateCompleted, "Finished questions")
		return "", nil
	}

	targetField := conv.MissingFields[0]
	productName := "کالا"
	if conv.Product != nil {
		productName = conv.Product.Name
	}

	// Load history
	hist, _ := m.mongoConvRepo.GetMessagesByConversationID(ctx, conv.ID.String())

	question, err := m.llmProvider.GenerateConversationQuestion(ctx, productName, targetField, hist)
	if err != nil || question == "" {
		question = fallbackQuestion(targetField, productName)
	}

	m.sendAssistantMessage(ctx, conv, question)
	_ = m.pgConvRepo.IncrementQuestions(ctx, conv.ID)
	_ = m.pgConvRepo.UpdateState(ctx, conv.ID, domain.ConvStateWaitingForReply, "Waiting for reply on: "+string(targetField))

	return question, nil
}

func (m *Manager) sendAssistantMessage(ctx context.Context, conv *domain.Conversation, text string) {
	// 1. Send via Telegram client if active
	if m.telegramClient != nil {
		_ = m.telegramClient.SendMessage(ctx, conv.SellerID, text)
	}

	// 2. Persist in MongoDB conversation messages
	_ = m.mongoConvRepo.SaveMessage(ctx, &domain.ConversationMessage{
		ConversationID: conv.ID.String(),
		Sender:         domain.SenderAssistant,
		Text:           text,
		SentAt:         time.Now(),
		Delivered:      true,
	})
}

func (m *Manager) extractFieldFromAnswer(text string, field domain.MissingField, collected map[string]any) {
	switch field {
	case domain.FieldPrice:
		prices := nlp.ParsePrices(text)
		if len(prices) > 0 {
			collected["price_amount"] = prices[0].Amount
			collected["price_raw"] = prices[0].Raw
		} else {
			collected["price_text"] = text
		}
	case domain.FieldCity:
		collected["seller_city"] = text
	case domain.FieldTehranArea:
		collected["seller_tehran_area"] = text
	case domain.FieldBranded:
		if strings.Contains(text, "اصل") || strings.Contains(text, "اورجینال") || strings.Contains(text, "بله") {
			collected["branded"] = true
		} else {
			collected["branded"] = false
		}
	case domain.FieldManufacturer:
		collected["manufacturer"] = text
	case domain.FieldExpiration:
		collected["warranty_info"] = text
	default:
		collected[string(field)] = text
	}
}

func fallbackQuestion(field domain.MissingField, productName string) string {
	switch field {
	case domain.FieldPrice:
		return fmt.Sprintf("قیمت نهایی و پیشنهادی شما برای %s چقدر است؟", productName)
	case domain.FieldBranded:
		return fmt.Sprintf("دستگاه %s اصلی و تولید شرکت مربوطه است؟", productName)
	case domain.FieldManufacturer:
		return fmt.Sprintf("شرکت سازنده و مشخصات دقیق %s را بفرمایید.", productName)
	case domain.FieldCity:
		return "دستگاه در کدام شهر است؟"
	case domain.FieldTehranArea:
		return "برای تحویل در تهران، در کدام محدوده یا بازار (علاءالدین، بازار و ...) امکان تحویل دارید؟"
	case domain.FieldExpiration:
		return "مهلت تست یا گارانتی تا چه تاریخی دارد؟"
	default:
		return fmt.Sprintf("اطلاعات تکمیلی %s را بفرمایید.", productName)
	}
}
