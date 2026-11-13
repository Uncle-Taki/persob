package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/conversation"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/extraction"
	"github.com/perso/perso-mind/internal/llm"
	"github.com/perso/perso-mind/internal/matching"
	"github.com/perso/perso-mind/internal/nlp"
	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

type MessageProcessor struct {
	cfg             *config.Config
	messagesRepo    *mongodb.MessagesRepo
	extractionRepo  *mongodb.ExtractionRepo
	jobsRepo        *mongodb.JobsRepo
	extractor       *extraction.Extractor
	matcher         *matching.Matcher
	offerRepo       *postgres.OfferRepo
	productRepo     *postgres.ProductRepo
	interestRepo    *postgres.InterestRepo
	convManager     *conversation.Manager
	analyticsEngine *analytics.Engine
	llmProvider     llm.Provider
}

func NewMessageProcessor(
	cfg *config.Config,
	messagesRepo *mongodb.MessagesRepo,
	extractionRepo *mongodb.ExtractionRepo,
	jobsRepo *mongodb.JobsRepo,
	matcher *matching.Matcher,
	offerRepo *postgres.OfferRepo,
	productRepo *postgres.ProductRepo,
	interestRepo *postgres.InterestRepo,
	convManager *conversation.Manager,
	analyticsEngine *analytics.Engine,
	llmProvider llm.Provider,
) *MessageProcessor {
	return &MessageProcessor{
		cfg:             cfg,
		messagesRepo:    messagesRepo,
		extractionRepo:  extractionRepo,
		jobsRepo:        jobsRepo,
		extractor:       extraction.NewExtractor(),
		matcher:         matcher,
		offerRepo:       offerRepo,
		productRepo:     productRepo,
		interestRepo:    interestRepo,
		convManager:     convManager,
		analyticsEngine: analyticsEngine,
		llmProvider:     llmProvider,
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
	msg, err := p.messagesRepo.GetByChatAndMessageID(ctx, payload.TelegramChatID, payload.TelegramMessageID)
	if err != nil || msg == nil {
		msg = &domain.TelegramMessage{
			TelegramChatID:    payload.TelegramChatID,
			TelegramMessageID: payload.TelegramMessageID,
			ChatTitle:         payload.ChatTitle,
			ChatUsername:      payload.ChatUsername,
			SenderID:          payload.SenderID,
			SenderUsername:    payload.SenderUsername,
			SenderDisplayName: payload.SenderDisplayName,
			MessageText:       payload.MessageText,
			MessageDate:       payload.MessageDate,
			ReceivedAt:        payload.ReceivedAt,
			NormalizedText:    nlp.Normalize(payload.MessageText),
			ProcessingStatus:  domain.ProcessingStatusProcessing,
		}
	}

	// 2. Perform deterministic classification and extraction
	extracted := p.extractor.ExtractFromMessage(msg)

	// 3. Optional LLM enrichment if enabled & permitted
	if p.cfg.LLMEnabled && p.llmProvider != nil {
		slog.Debug("invoking llm extraction provider for message", "chat_id", msg.TelegramChatID, "msg_id", msg.TelegramMessageID)
		llmResp, llmErr := p.llmProvider.ExtractMarketplaceInfo(ctx, msg.MessageText)
		if llmErr != nil {
			slog.Warn("llm extraction failed, falling back to deterministic extraction", "error", llmErr)
		} else {
			extracted = llm.MergeDeterministicAndLLM(extracted, llmResp)
		}
	}

	// 4. Save extracted result to MongoDB
	if err := p.extractionRepo.SaveExtraction(ctx, extracted); err != nil {
		slog.Error("failed to save extracted message in mongodb", "error", err)
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

	// 6. Mark processing status as processed in MongoDB
	_ = p.messagesRepo.UpdateProcessingStatus(ctx, msg.TelegramChatID, msg.TelegramMessageID, domain.ProcessingStatusProcessed)

	return nil
}

func (p *MessageProcessor) handleSellerAdvertisement(
	ctx context.Context,
	msg *domain.TelegramMessage,
	ext *domain.ExtractedMessage,
) error {
	extProd := ext.ExtractedProducts[0]

	// Match against catalog products
	matchRes, err := p.matcher.MatchSellerProduct(ctx, &extProd)
	if err != nil {
		return fmt.Errorf("matching seller product: %w", err)
	}

	var productID *uuid.UUID
	if matchRes.Matched {
		productID = &matchRes.ProductID
		slog.Info("matched seller product to catalog",
			"product_name", matchRes.ProductName,
			"match_level", matchRes.Level,
			"confidence", matchRes.Confidence,
		)
	}

	// Determine price
	var priceAmount *int64
	priceCurrency := domain.CurrencyToman
	var priceRaw string
	var priceConfidence float64

	if len(ext.ExtractedPrices) > 0 {
		p := ext.ExtractedPrices[0]
		priceAmount = &p.Amount
		priceCurrency = p.Currency
		priceRaw = p.Raw
		priceConfidence = p.Confidence
	}

	// Location
	var city, area, locText string
	if ext.Location != nil {
		city = ext.Location.City
		area = ext.Location.Area
		locText = ext.Location.LocationText
	}

	sourceMsgID := fmt.Sprintf("%d_%d", msg.TelegramChatID, msg.TelegramMessageID)

	offer := &domain.Offer{
		ID:                   uuid.New(),
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
		SellerCity:           city,
		SellerArea:           area,
		SellerLocationText:   locText,
		ListingStatus:        domain.ListingStatusActive,
		ExtractionConfidence: ext.Confidence,
	}

	// Upsert offer into PostgreSQL
	if err := p.offerRepo.Create(ctx, offer); err != nil {
		return fmt.Errorf("create offer: %w", err)
	}

	slog.Info("persisted seller offer to postgresql", "offer_id", offer.ID, "source_id", sourceMsgID)

	// Detect missing fields for conversation management
	var missingFields []domain.MissingField
	if offer.PriceAmount == nil || *offer.PriceAmount == 0 {
		missingFields = append(missingFields, domain.FieldPrice)
	}
	if offer.SellerCity == "" {
		missingFields = append(missingFields, domain.FieldCity)
	}
	if offer.SellerCity == "تهران" && offer.SellerArea == "" {
		missingFields = append(missingFields, domain.FieldTehranArea)
	}

	// Initialize conversation if missing fields exist
	if len(missingFields) > 0 && p.convManager != nil {
		_, err := p.convManager.InitConversation(ctx, offer, missingFields)
		if err != nil {
			slog.Warn("failed to initialize conversation for offer", "offer_id", offer.ID, "error", err)
		}
	}

	// Recalculate price statistics for product if matched
	if productID != nil && p.analyticsEngine != nil {
		go func(pID uuid.UUID) {
			calcCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = p.analyticsEngine.CalculateProductStatistics(calcCtx, pID)
		}(*productID)
	}

	return nil
}
