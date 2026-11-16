package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/conversation"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
	"github.com/perso/perso-mind/internal/llm"
	"github.com/perso/perso-mind/internal/matching"

	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

type MessageProcessor struct {
	sourceRepo *postgres.TelegramSourceRepo

	cfg                *config.Config
	messagesRepo       *mongodb.MessagesRepo
	extractionRepo     *mongodb.ExtractionRepo
	llmExtractionsRepo *mongodb.LLMExtractionsRepo
	jobsRepo           *mongodb.JobsRepo
	extractor          *extraction.Extractor
	matcher            *matching.Matcher
	offerRepo          *postgres.OfferRepo
	productRepo        *postgres.ProductRepo
	interestRepo       *postgres.InterestRepo
	sellerProfileRepo  *postgres.SellerProfileRepo
	matchResultRepo    *postgres.MatchResultRepo
	convManager        *conversation.Manager
	analyticsEngine    *analytics.Engine
	llmProvider        llm.Provider
}

func NewMessageProcessor(
	cfg *config.Config,
	messagesRepo *mongodb.MessagesRepo,
	extractionRepo *mongodb.ExtractionRepo,
	llmExtractionsRepo *mongodb.LLMExtractionsRepo,
	jobsRepo *mongodb.JobsRepo,
	matcher *matching.Matcher,
	offerRepo *postgres.OfferRepo,
	productRepo *postgres.ProductRepo,
	interestRepo *postgres.InterestRepo,
	sellerProfileRepo *postgres.SellerProfileRepo,
	matchResultRepo *postgres.MatchResultRepo,
	convManager *conversation.Manager,
	analyticsEngine *analytics.Engine,
	llmProvider llm.Provider,
) *MessageProcessor {
	return &MessageProcessor{
		cfg:                cfg,
		messagesRepo:       messagesRepo,
		extractionRepo:     extractionRepo,
		llmExtractionsRepo: llmExtractionsRepo,
		jobsRepo:           jobsRepo,
		extractor:          extraction.NewExtractor(),
		matcher:            matcher,
		offerRepo:          offerRepo,
		productRepo:        productRepo,
		interestRepo:       interestRepo,
		sellerProfileRepo:  sellerProfileRepo,
		matchResultRepo:    matchResultRepo,
		convManager:        convManager,
		analyticsEngine:    analyticsEngine,
		llmProvider:        llmProvider,
	}
}

// ProcessStreamMessage is the callback executed for each incoming message from Redis Stream
func (p *MessageProcessor) ProcessStreamMessage(ctx context.Context, msgID string, payload *queue.StreamPayload) error {
	slog.Info("processing message from stream",
		"stream_msg_id", msgID,
		"chat_id", payload.TelegramChatID,
		"msg_id", payload.TelegramMessageID,
	)

	// 1. Fetch or construct TelegramMessage domain object
	msg := payload.Message
	if msg == nil {
		var err error
		msg, err = p.messagesRepo.GetByChatAndMessageID(ctx, payload.TelegramChatID, payload.TelegramMessageID)
		if err != nil {
			return fmt.Errorf("load legacy queued message: %w", err)
		}
		if msg == nil {
			return fmt.Errorf("legacy queued message is missing")
		}
	}
	if msg.Deleted {
		revision := msg.MessageDate
		if msg.EditedDate != nil {
			revision = *msg.EditedDate
		}
		if err := p.offerRepo.RetireMissing(ctx, fmt.Sprintf("%d_%d", msg.TelegramChatID, msg.TelegramMessageID), revision, []string{}); err != nil {
			return err
		}
		return p.messagesRepo.UpdateProcessingStatus(ctx, msg.TelegramChatID, msg.TelegramMessageID, domain.ProcessingStatusProcessed)
	}

	// 2. Perform deterministic classification and extraction
	extracted := p.extractor.ExtractFromMessage(msg)

	// 3. Optional LLM enrichment if enabled & permitted
	allowed := false
	if p.cfg.LLMEnabled && p.sourceRepo != nil && len([]rune(msg.MessageText)) <= 6000 {
		var permissionErr error
		allowed, permissionErr = p.sourceRepo.AllowWorkspaceExternalProcessing(ctx, msg.TelegramChatID)
		if permissionErr != nil {
			return permissionErr
		}
		if allowed {
			allowed, permissionErr = p.sourceRepo.ReserveLLMRequest(ctx, 30)
			if permissionErr != nil {
				return permissionErr
			}
		}
	}
	if allowed && p.llmProvider != nil {
		slog.Debug("invoking llm extraction provider for message", "chat_id", msg.TelegramChatID, "msg_id", msg.TelegramMessageID)
		llmResp, llmErr := p.llmProvider.ExtractMarketplaceInfo(ctx, msg.MessageText)
		if llmErr != nil {
			slog.Warn("llm extraction failed, falling back to deterministic extraction", "error", llmErr)
		} else {
			extracted = llm.MergeDeterministicAndLLM(extracted, llmResp)

			// Persist raw LLM extraction in MongoDB
			if p.llmExtractionsRepo != nil {
				llmExt := &domain.LLMExtraction{
					MessageID:   fmt.Sprintf("%d_%d", msg.TelegramChatID, msg.TelegramMessageID),
					RawPrompt:   msg.MessageText,
					RawResponse: llmResp.RawAnalysis,
					ModelName:   p.llmProvider.Name(),
					CreatedAt:   time.Now().UTC(),
				}
				for _, prod := range llmResp.Products {
					llmExt.ExtractedProducts = append(llmExt.ExtractedProducts, domain.ExtractedProduct{
						Name:         prod.Name,
						Brand:        prod.Brand,
						Manufacturer: prod.Manufacturer,
						Model:        prod.Model,
						Variant:      prod.Variant,
						Condition:    prod.Condition,
						Quantity:     prod.Quantity,
						Confidence:   prod.Confidence,
						EvidenceSpan: prod.EvidenceSpan,
					})
				}
				for _, pr := range llmResp.Prices {
					llmExt.ExtractedPrices = append(llmExt.ExtractedPrices, domain.ExtractedPrice{
						Amount:       pr.Amount,
						Currency:     pr.Currency,
						Raw:          pr.Raw,
						Negotiable:   pr.Negotiable,
						Confidence:   pr.Confidence,
						EvidenceSpan: pr.EvidenceSpan,
					})
				}
				_ = p.llmExtractionsRepo.Save(ctx, llmExt)
			}
		}
	}

	extracted.SourceRevisionAt = msg.MessageDate
	if msg.EditedDate != nil {
		extracted.SourceRevisionAt = *msg.EditedDate
	}
	// 4. Save extracted result to MongoDB
	if err := p.extractionRepo.SaveExtraction(ctx, extracted); err != nil {
		return fmt.Errorf("save extracted message: %w", err)
	}

	// 5. Handle Seller advertisement workflow
	if extracted.Classification == domain.ClassificationSeller && len(extracted.ExtractedProducts) > 0 {
		if err := p.handleSellerAdvertisement(ctx, msg, extracted); err != nil {
			slog.Error("error handling seller advertisement", "error", err)
			_ = p.messagesRepo.UpdateProcessingStatus(ctx, msg.TelegramChatID, msg.TelegramMessageID, domain.ProcessingStatusFailed)
			return err
		}
	} else if extracted.Classification == domain.ClassificationBuyer {
		slog.Info("classified message as buyer request, storing without creating offer",
			"chat_id", msg.TelegramChatID, "msg_id", msg.TelegramMessageID)
	}

	if extracted.Classification != domain.ClassificationSeller {
		revision := msg.MessageDate
		if msg.EditedDate != nil {
			revision = *msg.EditedDate
		}
		if err := p.offerRepo.RetireMissing(ctx, fmt.Sprintf("%d_%d", msg.TelegramChatID, msg.TelegramMessageID), revision, []string{}); err != nil {
			return err
		}
	}
	// 6. Mark processing status as processed in MongoDB
	return p.messagesRepo.UpdateProcessingStatus(ctx, msg.TelegramChatID, msg.TelegramMessageID, domain.ProcessingStatusProcessed)
}

func (p *MessageProcessor) handleSellerAdvertisement(
	ctx context.Context,
	msg *domain.TelegramMessage,
	ext *domain.ExtractedMessage,
) error {
	// Location
	var city, area, locText string
	if ext.Location != nil {
		city = ext.Location.City
		area = ext.Location.Area
		locText = ext.Location.LocationText
	}

	// 1. Upsert Seller Profile into PostgreSQL
	isOptedOut := false
	if p.sellerProfileRepo != nil && msg.SenderID != 0 {
		sellerProfile := &domain.SellerProfile{
			SellerID:    msg.SenderID,
			Username:    msg.SenderUsername,
			DisplayName: msg.SenderDisplayName,
			City:        city,
			TehranArea:  area,
			TotalOffers: len(ext.ExtractedProducts),
		}
		if err := p.sellerProfileRepo.Upsert(ctx, sellerProfile); err != nil {
			return fmt.Errorf("upsert seller profile: %w", err)
		}

		if prof, err := p.sellerProfileRepo.GetByID(ctx, msg.SenderID); err == nil && prof != nil {
			isOptedOut = prof.OptedOut
		}
	}

	sourceMsgID := fmt.Sprintf("%d_%d", msg.TelegramChatID, msg.TelegramMessageID)

	// 2. Process every extracted product (Multi-product support)
	items := extraction.ListingItems(msg, ext)
	keys := []string{}
	revision := msg.MessageDate
	if msg.EditedDate != nil {
		revision = *msg.EditedDate
	}
	if revision.IsZero() {
		revision = msg.ReceivedAt
	}
	var sourceID *uuid.UUID
	if p.sourceRepo != nil {
		var err error
		sourceID, err = p.sourceRepo.SourceForPeer(ctx, msg.TelegramChatID)
		if err != nil {
			return fmt.Errorf("resolve shared source: %w", err)
		}
	}
	for _, item := range items {
		extProd := item.Product
		keys = append(keys, item.Key)
		// Match against catalog products
		matchRes, err := p.matcher.MatchSellerProduct(ctx, &extProd)
		if err != nil {
			return fmt.Errorf("match catalog: %w", err)
		}

		var productID *uuid.UUID
		if matchRes != nil && matchRes.Matched && matchRes.VariantMatched {
			productID = &matchRes.ProductID
			slog.Info("matched seller product to catalog",
				"product_name", matchRes.ProductName,
				"match_level", matchRes.Level,
				"confidence", matchRes.Confidence,
			)
		}

		// Determine price for this item
		var priceAmount *int64
		priceCurrency := domain.CurrencyToman
		var priceRaw string
		var priceConfidence float64

		if item.Price != nil {
			pr := item.Price
			priceAmount = &pr.Amount
			priceCurrency = pr.Currency
			priceRaw = pr.Raw
			priceConfidence = pr.Confidence
		}

		offer := &domain.Offer{
			ID:            uuid.New(),
			SourceItemKey: item.Key, SourceRevisionAt: revision, SourcePublishedAt: &msg.MessageDate, SourceID: sourceID,
			ProductID:            productID,
			SourceMessageID:      sourceMsgID,
			SellerID:             msg.SenderID,
			Brand:                extProd.Brand,
			Manufacturer:         extProd.Manufacturer,
			Model:                extProd.Model,
			Condition:            extProd.Condition,
			Quantity:             extProd.Quantity,
			PriceAmount:          priceAmount,
			PriceCurrency:        priceCurrency,
			PriceRaw:             priceRaw,
			PriceConfidence:      priceConfidence,
			Branded:              ext.Branded,
			BrandedKnown:         ext.BrandedKnown,
			SellerCity:           city,
			SellerArea:           area,
			SellerLocationText:   locText,
			ListingStatus:        domain.ListingStatusActive,
			ExtractionConfidence: ext.Confidence,
		}

		if matchRes != nil && extProd.Variant != "" {
			value := matchRes.VariantMatched
			offer.VariantMatch = &value
		}
		// Upsert offer into PostgreSQL
		if err := p.offerRepo.Create(ctx, offer); err != nil {
			return fmt.Errorf("persist seller offer: %w", err)
		}

		if !offer.Applied {
			continue
		}
		slog.Info("persisted seller offer to postgresql", "offer_id", offer.ID, "source_id", sourceMsgID, "product", extProd.Name)

		// 3. Match against buyer interests and store offer_match_results
		if productID != nil && sourceID != nil && p.matchResultRepo != nil && p.interestRepo != nil {
			interests, err := p.interestRepo.FindAssignedInterestsByProductID(ctx, *productID, *sourceID)
			if err != nil {
				return fmt.Errorf("load purchase interests: %w", err)
			}
			{
				for _, interest := range interests {
					if !interestMatchesOffer(interest, offer) {
						continue
					}
					score := 85.0
					if offer.Condition == domain.ConditionNew || offer.Condition == domain.ConditionLikeNew {
						score += 10.0
					}
					if offer.PriceAmount != nil && *offer.PriceAmount > 0 {
						score += 5.0
					}

					matchResult := &domain.OfferMatchResult{
						ID:               uuid.New(),
						OfferID:          offer.ID,
						InterestID:       &interest.ID,
						UserID:           interest.UserID,
						ProductID:        *productID,
						MatchScore:       score,
						MatchExplanation: fmt.Sprintf("Matched %s (%s) against user interest", extProd.Name, extProd.Condition),
						MatchedAttributes: map[string]interface{}{
							"brand":     offer.Brand,
							"model":     offer.Model,
							"condition": string(offer.Condition),
						},
						UnmatchedAttributes: map[string]interface{}{},
						CreatedAt:           time.Now().UTC(),
					}
					if err := p.matchResultRepo.Create(ctx, matchResult); err != nil {
						return fmt.Errorf("persist offer match: %w", err)
					}
				}
			}
		}

		// 5. Detect missing fields for conversation management (if not opted out)
		if !isOptedOut && productID != nil && p.interestRepo != nil {
			eligible, err := p.interestRepo.FindInterestedUsers(ctx, *productID)
			if err != nil {
				return err
			}
			hasOwner := false
			for _, user := range eligible {
				if user == *p.cfg.OwnerID() {
					hasOwner = true
				}
			}
			if !hasOwner {
				continue
			}
			var missingFields []domain.MissingField
			fields := p.cfg.ConversationFields
			if len(fields) == 0 {
				fields = []domain.MissingField{domain.FieldPrice, domain.FieldCity, domain.FieldTehranArea}
			}
			for _, field := range fields {
				missing := false
				switch field {
				case domain.FieldPrice:
					missing = offer.PriceAmount == nil || *offer.PriceAmount == 0
				case domain.FieldCondition:
					missing = offer.Condition == domain.ConditionUnknown
				case domain.FieldBranded:
					missing = !offer.BrandedKnown
				case domain.FieldManufacturer:
					missing = offer.Manufacturer == ""
				case domain.FieldWarranty:
					missing = !offer.WarrantyKnown
				case domain.FieldExpiration:
					missing = offer.ExpirationDate == nil
				case domain.FieldCity:
					missing = offer.SellerCity == ""
				case domain.FieldTehranArea:
					missing = offer.SellerCity == "تهران" && offer.SellerArea == ""
				}
				if missing {
					missingFields = append(missingFields, field)
				}
			}

			// Initialize conversation if missing fields exist
			if len(missingFields) > 0 && p.convManager != nil && msg.SenderID != 0 {
				_, err := p.convManager.InitConversation(ctx, intakeOffer(offer, p.cfg.OwnerID()), missingFields)
				if err != nil {
					return fmt.Errorf("initialize conversation: %w", err)
				}
			}
		}

		// 6. Recalculate price statistics for product if matched
		if productID != nil && p.analyticsEngine != nil {
			go func(pID uuid.UUID) {
				calcCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, _ = p.analyticsEngine.CalculateProductStatistics(calcCtx, pID)
			}(*productID)
		}
	}

	return p.offerRepo.RetireMissing(ctx, sourceMsgID, revision, keys)
}

func interestMatchesOffer(interest domain.Interest, offer *domain.Offer) bool {
	if interest.MaxPriceAmount != nil && offer.PriceAmount != nil {
		price := *offer.PriceAmount
		if offer.PriceCurrency == domain.CurrencyRial {
			price /= 10
		}
		if price > *interest.MaxPriceAmount {
			return false
		}
	}
	if interest.MinQuantity != nil && offer.Quantity < *interest.MinQuantity {
		return false
	}
	if interest.PreferredCity != "" && !strings.EqualFold(strings.TrimSpace(interest.PreferredCity), strings.TrimSpace(offer.SellerCity)) {
		return false
	}
	return true
}

func (p *MessageProcessor) SetSourceRepo(r *postgres.TelegramSourceRepo) { p.sourceRepo = r }

// A shared listing has no buyer owner. The existing supervised operator intake
// creates an explicitly attributed request; buyer fan-out belongs to step 3B/7.
func intakeOffer(offer *domain.Offer, owner *uuid.UUID) *domain.Offer {
	copy := *offer
	copy.OwnerID = owner
	return &copy
}
