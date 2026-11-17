package conversation

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/llm"
	"github.com/perso/perso-mind/internal/nlp"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"github.com/perso/perso-mind/internal/telegram"
	"regexp"
	"strings"
	"time"
)

var optOutRegex = regexp.MustCompile(`(?i)(مزاحم نشو|پیام نده|لغو|بلاک|نمیخوام|دیگه نپرس|استپ|\bstop\b)`)

type Manager struct {
	cfg            *config.Config
	pgConvRepo     *postgres.ConversationRepo
	mongoConvRepo  *mongodb.ConversationRepo
	auditRepo      *postgres.AuditRepo
	sellerRepo     *postgres.SellerProfileRepo
	telegramClient telegram.TelegramClient
	llmProvider    llm.Provider
}

func NewManager(c *config.Config, p *postgres.ConversationRepo, m *mongodb.ConversationRepo, a *postgres.AuditRepo, t telegram.TelegramClient, l llm.Provider) *Manager {
	return &Manager{cfg: c, pgConvRepo: p, mongoConvRepo: m, auditRepo: a, telegramClient: t, llmProvider: l}
}
func (m *Manager) SetSellerRepo(r *postgres.SellerProfileRepo) { m.sellerRepo = r }
func (m *Manager) InitConversation(ctx context.Context, o *domain.Offer, missing []domain.MissingField) (*domain.Conversation, error) {
	if len(missing) == 0 || o.SellerID <= 0 {
		return nil, nil
	}
	if m.sellerRepo != nil {
		p, err := m.sellerRepo.GetByID(ctx, o.SellerID)
		if err != nil {
			return nil, err
		}
		if p != nil && (p.OptedOut || (p.CooldownUntil != nil && p.CooldownUntil.After(time.Now()))) {
			return nil, nil
		}
	}
	owner := o.OwnerID
	if owner == nil {
		owner = m.cfg.OwnerID()
	}
	// One active seller-purpose draft per operator/account; related offers remain in matches.
	key := owner.String() + ":" + fmt.Sprint(o.SellerID) + ":listing_intake"
	c := &domain.Conversation{ID: uuid.New(), OwnerID: owner, DraftKey: key, OriginKey: owner.String() + ":" + o.ID.String() + ":" + fmt.Sprint(o.SourceRevisionAt.UnixNano()), OfferID: &o.ID, SellerID: o.SellerID, ProductID: o.ProductID, State: domain.ConvStateAwaitingApproval, MissingFields: missing, CollectedData: map[string]any{}, MaxQuestions: m.cfg.ConversationMaxQuestions, Notes: "Draft for a matching purchase interest"}
	if err := m.pgConvRepo.Create(ctx, c); err != nil {
		return nil, err
	}
	return m.pgConvRepo.GetByID(ctx, c.ID)
}
func (m *Manager) Approve(ctx context.Context, id, user uuid.UUID) error {
 return m.approve(ctx, id, user, false)
}

// ApproveAsAdmin preserves the buyer as owner while recording the approving administrator.
func (m *Manager) ApproveAsAdmin(ctx context.Context, id, admin uuid.UUID) error {
 return m.approve(ctx, id, admin, true)
}

func (m *Manager) approve(ctx context.Context, id, actor uuid.UUID, administrator bool) error {
	if !m.cfg.OutreachEnabled {
		return fmt.Errorf("outreach is disabled by configuration")
	}
	c, err := m.pgConvRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("conversation not found")
	}
	if c.OwnerID == nil || (!administrator && *c.OwnerID != actor) {
		return fmt.Errorf("conversation not authorized")
	}
	if administrator {
		allowed, err := m.pgConvRepo.HasCurrentBuyerGrant(ctx, id)
		if err != nil { return err }
		if !allowed { return fmt.Errorf("buyer account, interest, or source grant is no longer active") }
	}
	if c.State != domain.ConvStateAwaitingApproval {
		return fmt.Errorf("conversation is not awaiting approval")
	}
	if m.sellerRepo != nil {
		p, err := m.sellerRepo.GetByID(ctx, c.SellerID)
		if err != nil {
			return err
		}
		if p != nil && (p.OptedOut || (p.CooldownUntil != nil && p.CooldownUntil.After(time.Now()))) {
			return fmt.Errorf("seller is not eligible for outreach")
		}
	}
	now := time.Now().UTC()
	c.ApprovedAt = &now
	c.ApprovedByUserID = &actor
	c.State = domain.ConvStateAwaitingConsent
	return m.pgConvRepo.SaveProgress(ctx, c, "سلام! من دستیار خودکار پرسو هستم. برای تکمیل مشخصات آگهی شما مایل هستید چند سؤال کوتاه بپرسم؟")
}
func (m *Manager) Pause(ctx context.Context, id uuid.UUID) error {
	return m.setState(ctx, id, domain.ConvStatePaused)
}
func (m *Manager) Close(ctx context.Context, id uuid.UUID) error {
	return m.setState(ctx, id, domain.ConvStateCompleted)
}
func (m *Manager) setState(ctx context.Context, id uuid.UUID, state domain.ConversationState) error {
	c, err := m.pgConvRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("conversation not found")
	}
	if err = Transition(c.State, state); err != nil {
		return err
	}
	c.State = state
	return m.pgConvRepo.SaveProgress(ctx, c, "")
}
func (m *Manager) HandleIncomingSellerMessage(ctx context.Context, id uuid.UUID, text string) (string, error) {
	c, err := m.pgConvRepo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", fmt.Errorf("conversation not found")
	}
	if c.CollectedData == nil {
		c.CollectedData = map[string]any{}
	}
	if optOutRegex.MatchString(nlp.CleanSearchKey(text)) {
		c.State = domain.ConvStateOptedOut
		if m.sellerRepo != nil {
			if err = m.sellerRepo.SetOptOut(ctx, c.SellerID, true); err != nil {
				return "", err
			}
		}
		return "", m.pgConvRepo.SaveProgress(ctx, c, "")
	}
	reply := ""
	switch c.State {
	case domain.ConvStateAwaitingConsent:
		accepted, known := llm.ConsentDecision(text)
		if !known {
			if c.QuestionsAskedCount >= c.MaxQuestions {
				c.State = domain.ConvStateDeclined
				break
			}
			c.QuestionsAskedCount++
			reply = "آیا اجازه می‌دهید چند سؤال درباره همین آگهی بپرسم؟ لطفاً بله یا خیر بفرمایید."
			break
		}
		if !accepted {
			c.State = domain.ConvStateDeclined
			break
		}
		c.ConsentGranted = true
		reply = m.nextQuestion(c)
	case domain.ConvStateWaitingForReply:
		if !c.ConsentGranted {
			return "", fmt.Errorf("seller consent is required")
		}
		if len(c.MissingFields) == 0 {
			c.State = domain.ConvStateCompleted
			break
		}
		if !extractAnswer(text, c.MissingFields[0], c.CollectedData) {
			if c.QuestionsAskedCount >= c.MaxQuestions {
				c.State = domain.ConvStateCompleted
				c.Notes = "Question budget exhausted; unresolved fields retained"
				break
			}
			c.QuestionsAskedCount++
			reply = "پاسخ قابل ثبت نبود. " + fallbackQuestion(c.MissingFields[0], "کالا")
			break
		}
		c.MissingFields = c.MissingFields[1:]
		reply = m.nextQuestion(c)
	default:
		return "", nil
	}
	if err = m.pgConvRepo.SaveProgress(ctx, c, reply); err != nil {
		return "", err
	}
	if c.State == domain.ConvStateCompleted && m.sellerRepo != nil {
		if err = m.sellerRepo.SetCooldown(ctx, c.SellerID, time.Now().Add(m.cfg.ConversationCooldown)); err != nil {
			return "", err
		}
	}
	return reply, nil
}
func (m *Manager) nextQuestion(c *domain.Conversation) string {
	if len(c.MissingFields) == 0 || c.QuestionsAskedCount >= c.MaxQuestions {
		c.State = domain.ConvStateCompleted
		c.Notes = "Intake ended; unresolved fields remain visible"
		return ""
	}
	c.State = domain.ConvStateWaitingForReply
	c.QuestionsAskedCount++
	return fallbackQuestion(c.MissingFields[0], "کالا")
}
func extractAnswer(text string, f domain.MissingField, data map[string]any) bool {
	text = strings.TrimSpace(text)
	if len([]rune(text)) < 2 || len([]rune(text)) > 200 {
		return false
	}
	switch f {
	case domain.FieldPrice:
		prices := nlp.ParsePrices(text)
		if len(prices) == 0 {
			if match := explicitPriceAnswer.FindStringSubmatch(nlp.Normalize(text)); len(match) == 3 {
				amount, err := nlp.ParsePersianNumber(match[1])
				if err == nil {
					currency := domain.CurrencyToman
					if match[2] == "ریال" {
						currency = domain.CurrencyRial
					}
					prices = []nlp.ParsedPrice{{Amount: amount, Currency: currency, Raw: text}}
				}
			}
		}
		if len(prices) != 1 || prices[0].Amount <= 0 {
			return false
		}
		data["price_amount"] = prices[0].Amount
		data["price_currency"] = string(prices[0].Currency)
		data["price_raw"] = prices[0].Raw
	case domain.FieldCondition:
		norm := nlp.CleanSearchKey(text)
		choices := map[string]domain.ItemCondition{"آکبند": domain.ConditionNew, "نو": domain.ConditionNew, "new": domain.ConditionNew, "در حد نو": domain.ConditionLikeNew, "like new": domain.ConditionLikeNew, "کارکرده": domain.ConditionUsedFair, "used": domain.ConditionUsedFair, "کارکرده تمیز": domain.ConditionUsedGood}
		condition, ok := choices[norm]
		if !ok {
			return false
		}
		data["condition"] = string(condition)
	case domain.FieldBranded:
		norm := nlp.CleanSearchKey(text)
		switch norm {
		case "بله", "اصل", "اصلی", "اورجینال", "yes", "original":
			data["branded"] = true
		case "خیر", "نه", "اصل نیست", "غیراصل", "طرح", "کپی", "no":
			data["branded"] = false
		default:
			return false
		}
	case domain.FieldManufacturer:
		norm := nlp.CleanSearchKey(text)
		if norm == "نامشخص" || norm == "نمیدانم" || norm == "نمی دانم" || norm == "unknown" || !regexp.MustCompile(`[\p{L}]`).MatchString(text) {
			return false
		}
		data["manufacturer"] = text
	case domain.FieldWarranty, domain.FieldExpiration:
		if text == "ندارد" || strings.EqualFold(text, "none") {
			data[string(f)] = ""
			break
		}
		parsed, err := time.Parse("2006-01-02", nlp.Normalize(text))
		if err != nil {
			return false
		}
		data[string(f)] = parsed.Format("2006-01-02")
	case domain.FieldCity:
		result := extractionCity.FindString(nlp.Normalize(text))
		if result == "" {
			return false
		}
		data["seller_city"] = result
	case domain.FieldTehranArea:
		if strings.Contains(text, "نمی") || strings.Contains(text, "نامشخص") {
			return false
		}
		data["seller_tehran_area"] = text
	default:
		return false
	}
	return true
}

var extractionCity = regexp.MustCompile(`تهران|مشهد|اصفهان|شیراز|تبریز|کرج|اهواز|قم|رشت|ارومیه|یزد|ساری|کرمان|همدان|کیش`)

func fallbackQuestion(field domain.MissingField, name string) string {
	switch field {
	case domain.FieldPrice:
		return fmt.Sprintf("قیمت نهایی %s را با واحد تومان یا ریال بفرمایید.", name)
	case domain.FieldCondition:
		return "وضعیت کالا چیست؟ لطفاً نو، در حد نو یا کارکرده بفرمایید."
	case domain.FieldBranded:
		return "کالا اصل است؟ لطفاً بله یا خیر بفرمایید."
	case domain.FieldManufacturer:
		return "نام شرکت سازنده کالا چیست؟"
	case domain.FieldWarranty:
		return "گارانتی تا چه تاریخی معتبر است؟ تاریخ میلادی YYYY-MM-DD یا «ندارد» بفرمایید."
	case domain.FieldExpiration:
		return "تاریخ انقضای محصول را به میلادی YYYY-MM-DD یا «ندارد» بفرمایید."
	case domain.FieldCity:
		return "کالا در کدام شهر است؟"
	case domain.FieldTehranArea:
		return "در کدام محدوده یا بازار تهران امکان تحویل دارید؟"
	default:
		return "مشخصات تکمیلی آگهی را بفرمایید."
	}
}

var explicitPriceAnswer = regexp.MustCompile(`^(?:قیمت\s*[:=-]?\s*)?([0-9,،.]+)\s*(تومان|تومن|ریال)$`)
