package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

type AnalyticsHandler struct {
	analyticsRepo   *postgres.AnalyticsRepo
	offerRepo       *postgres.OfferRepo
	productRepo     *postgres.ProductRepo
	messagesRepo    *mongodb.MessagesRepo
	analyticsEngine *analytics.Engine
}

func NewAnalyticsHandler(
	analyticsRepo *postgres.AnalyticsRepo,
	offerRepo *postgres.OfferRepo,
	productRepo *postgres.ProductRepo,
	messagesRepo *mongodb.MessagesRepo,
	analyticsEngine *analytics.Engine,
) *AnalyticsHandler {
	return &AnalyticsHandler{
		analyticsRepo:   analyticsRepo,
		offerRepo:       offerRepo,
		productRepo:     productRepo,
		messagesRepo:    messagesRepo,
		analyticsEngine: analyticsEngine,
	}
}

func (h *AnalyticsHandler) DashboardSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.analyticsRepo.GetDashboardSummary(r.Context())
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get dashboard summary")
		return
	}

	// Add processed messages in last 24h from MongoDB
	since := time.Now().Add(-24 * time.Hour)
	count24h, _ := h.messagesRepo.CountRecentProcessed(r.Context(), since)
	summary.MessagesProcessed24h = count24h

	httputil.RespondJSON(w, http.StatusOK, summary)
}

func (h *AnalyticsHandler) PriceAnalytics(w http.ResponseWriter, r *http.Request) {
	pIDStr := r.URL.Query().Get("product_id")
	if pIDStr == "" {
		httputil.RespondError(w, http.StatusBadRequest, "product_id query param is required")
		return
	}

	productID, err := uuid.Parse(pIDStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid product_id")
		return
	}

	stats, err := h.analyticsEngine.CalculateProductStatistics(r.Context(), productID)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to calculate product statistics")
		return
	}

	// Also fetch recent historical observations (last 30 days)
	historySince := time.Now().Add(-30 * 24 * time.Hour)
	history, _ := h.analyticsRepo.GetHistoricalPrices(r.Context(), productID, historySince)

	httputil.RespondJSON(w, http.StatusOK, map[string]any{
		"statistics": stats,
		"history":    history,
	})
}

func (h *AnalyticsHandler) OfferAnalytics(w http.ResponseWriter, r *http.Request) {
	// Returns top scored deals across the marketplace
	offers, _, err := h.offerRepo.List(r.Context(), postgres.OfferFilter{
		ListingStatus: "active",
		Limit:         50,
		Offset:        0,
		SortBy:        domain.SortByBestOffer,
	})
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list offers")
		return
	}

	scored := h.analyticsEngine.ScoreAndRankOffers(offers, 0, domain.SortByBestOffer)
	if len(scored) > 20 {
		scored = scored[:20]
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]any{
		"top_deals":   scored,
		"analyzed_at": time.Now().UTC(),
	})
}

func (h *AnalyticsHandler) CompareCondition(w http.ResponseWriter, r *http.Request) {
	var pID *uuid.UUID
	if pIDStr := r.URL.Query().Get("product_id"); pIDStr != "" {
		if id, err := uuid.Parse(pIDStr); err == nil {
			pID = &id
		}
	}

	comparisons, err := h.analyticsRepo.GetConditionComparison(r.Context(), pID)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get condition comparison")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]any{
		"product_id":  pID,
		"comparisons": comparisons,
	})
}

func (h *AnalyticsHandler) CompareCity(w http.ResponseWriter, r *http.Request) {
	var pID *uuid.UUID
	if pIDStr := r.URL.Query().Get("product_id"); pIDStr != "" {
		if id, err := uuid.Parse(pIDStr); err == nil {
			pID = &id
		}
	}

	comparisons, err := h.analyticsRepo.GetCityComparison(r.Context(), pID)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get city comparison")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]any{
		"product_id":  pID,
		"comparisons": comparisons,
	})
}

func (h *AnalyticsHandler) CompareBrand(w http.ResponseWriter, r *http.Request) {
	comparisons, err := h.analyticsRepo.GetBrandComparison(r.Context())
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get brand comparison")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]any{
		"comparisons": comparisons,
	})
}
