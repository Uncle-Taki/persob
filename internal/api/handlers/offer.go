package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/identity"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

type OfferHandler struct {
	offerRepo       *postgres.OfferRepo
	matchResultRepo *postgres.MatchResultRepo
	analyticsEngine *analytics.Engine
}

func NewOfferHandler(
	offerRepo *postgres.OfferRepo,
	matchResultRepo *postgres.MatchResultRepo,
	analyticsEngine *analytics.Engine,
) *OfferHandler {
	return &OfferHandler{
		offerRepo:       offerRepo,
		matchResultRepo: matchResultRepo,
		analyticsEngine: analyticsEngine,
	}
}

func (h *OfferHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	filter := postgres.OfferFilter{
		Brand:         q.Get("brand"),
		Model:         q.Get("model"),
		Condition:     q.Get("condition"),
		ListingStatus: q.Get("listing_status"),
		SortBy:        domain.SortBy(q.Get("sort_by")),
		Limit:         limit,
		Offset:        offset,
	}

	if pIDStr := q.Get("product_id"); pIDStr != "" {
		if pID, err := uuid.Parse(pIDStr); err == nil {
			filter.ProductID = &pID
		} else {
			httputil.RespondError(w, 400, "Invalid product_id")
			return
		}
	}
	if sIDStr := q.Get("seller_id"); sIDStr != "" {
		if sID, err := strconv.ParseInt(sIDStr, 10, 64); err == nil {
			filter.SellerID = &sID
		}
	}
	if minPStr := q.Get("min_price"); minPStr != "" {
		if minP, err := strconv.ParseInt(minPStr, 10, 64); err == nil {
			filter.MinPrice = &minP
		}
	}
	if maxPStr := q.Get("max_price"); maxPStr != "" {
		if maxP, err := strconv.ParseInt(maxPStr, 10, 64); err == nil {
			filter.MaxPrice = &maxP
		}
	}

	if offset < 0 {
		httputil.RespondError(w, 400, "offset must be nonnegative")
		return
	}
	if filter.ListingStatus == "" {
		filter.ListingStatus = "active"
	}
	userID := httputil.GetUserIDFromContext(r.Context())
	if isWorkspaceAdmin(r) {
		filter.WorkspaceID = &domain.WorkspaceID
	} else if userID != uuid.Nil {
		filter.OwnerID = &userID
	}
	withScores := q.Get("with_scores") == "true" || filter.SortBy == domain.SortByBestOffer || filter.SortBy == ""
	filter.Unbounded = withScores
	offers, total, err := h.offerRepo.List(r.Context(), filter)
	if err != nil {
		httputil.RespondError(w, 500, "Unable to list offers")
		return
	}
	if withScores {
		// Compute each product's baseline from its entire eligible population before paging.
		baselines := map[uuid.UUID]int64{}
		for _, o := range offers {
			if o.ProductID != nil {
				if _, ok := baselines[*o.ProductID]; !ok {
					stats, err := h.analyticsEngine.CalculateProductStatistics(r.Context(), *o.ProductID)
					if err != nil {
						httputil.RespondError(w, 500, "Unable to calculate offer scores")
						return
					}
					baselines[*o.ProductID] = stats.MedianPrice
				}
			}
		}
		scored := make([]domain.ScoredOffer, 0, len(offers))
		for _, o := range offers {
			median := int64(0)
			if o.ProductID != nil {
				median = baselines[*o.ProductID]
			}
			scored = append(scored, domain.ScoredOffer{Offer: o, ScoreBreakdown: analytics.ScoreOffer(o, median)})
		}
		analytics.SortScoredOffers(scored, filter.SortBy)
		start := min(offset, len(scored))
		end := min(start+limit, len(scored))
		httputil.RespondWithMeta(w, 200, scored[start:end], httputil.PaginationMeta{Total: total, Limit: limit, Offset: offset})
		return
	}
	httputil.RespondWithMeta(w, 200, offers, httputil.PaginationMeta{Total: total, Limit: limit, Offset: offset})
}

func (h *OfferHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid offer ID")
		return
	}

	offer, err := h.offerRepo.GetByID(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get offer")
		return
	}
	userID := httputil.GetUserIDFromContext(r.Context())
	if offer == nil || (isWorkspaceAdmin(r) && offer.WorkspaceID != domain.WorkspaceID) || (!isWorkspaceAdmin(r) && userID != uuid.Nil && (offer.OwnerID == nil || *offer.OwnerID != userID)) {
		httputil.RespondError(w, http.StatusNotFound, "Offer not found")
		return
	}

	// Include score breakdown
	var medianPrice int64 = 0
	if offer.ProductID != nil {
		stats, _ := h.analyticsEngine.CalculateProductStatistics(r.Context(), *offer.ProductID)
		if stats != nil {
			medianPrice = stats.MedianPrice
		}
	}
	score := analytics.ScoreOffer(*offer, medianPrice)

	httputil.RespondJSON(w, http.StatusOK, map[string]any{
		"offer":           offer,
		"score_breakdown": score,
	})
}

func (h *OfferHandler) ListMatches(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	if limit <= 0 {
		limit = 50
	}

	if h.matchResultRepo == nil {
		httputil.RespondJSON(w, http.StatusOK, []domain.OfferMatchResult{})
		return
	}

	matches, err := h.matchResultRepo.ListByUserID(r.Context(), userID, limit, offset)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list matches")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, matches)
}

func isWorkspaceAdmin(r *http.Request) bool {
	u := identity.Current(r.Context())
	return u != nil && u.Active && (u.Role == domain.RoleAdmin || u.Role == domain.RoleSuperAdmin)
}
