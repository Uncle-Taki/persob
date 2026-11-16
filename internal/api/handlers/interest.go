package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

type InterestHandler struct {
	interestRepo *postgres.InterestRepo
	productRepo  *postgres.ProductRepo
}

func NewInterestHandler(interestRepo *postgres.InterestRepo, productRepo *postgres.ProductRepo) *InterestHandler {
	return &InterestHandler{
		interestRepo: interestRepo,
		productRepo:  productRepo,
	}
}

func (h *InterestHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())

	list, err := h.interestRepo.ListByUserID(r.Context(), userID)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list interests")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, list)
}

func (h *InterestHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())

	var req domain.CreateInterestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ProductID == uuid.Nil {
		httputil.RespondError(w, http.StatusBadRequest, "product_id is required")
		return
	}
	if req.MaxPriceAmount != nil && *req.MaxPriceAmount < 0 {
		httputil.RespondError(w, http.StatusBadRequest, "max_price_amount must be nonnegative")
		return
	}
	if req.MinQuantity != nil && *req.MinQuantity <= 0 {
		httputil.RespondError(w, http.StatusBadRequest, "min_quantity must be positive")
		return
	}
	if req.RequiredAttributes != nil {
		var attrs map[string]any
		if json.Unmarshal(req.RequiredAttributes, &attrs) != nil {
			httputil.RespondError(w, http.StatusBadRequest, "required_attributes must be a JSON object")
			return
		}
	}

	// Verify product exists
	product, err := h.productRepo.GetByID(r.Context(), req.ProductID)
	if err != nil || product == nil {
		httputil.RespondError(w, http.StatusNotFound, "Product not found")
		return
	}

	interest := &domain.Interest{
		ID:                 uuid.New(),
		UserID:             userID,
		ProductID:          req.ProductID,
		SellEnabled:        false, // Phase 1: false
		BuyEnabled:         true,  // Phase 1: true
		MaxPriceAmount:     req.MaxPriceAmount,
		MinQuantity:        req.MinQuantity,
		PreferredCity:      req.PreferredCity,
		RequiredAttributes: req.RequiredAttributes,
	}

	if err := h.interestRepo.Create(r.Context(), interest); err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to create interest")
		return
	}

	interest.Product = product
	httputil.RespondJSON(w, http.StatusCreated, interest)
}

func (h *InterestHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid interest ID")
		return
	}
	var req domain.UpdateInterestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.MaxPriceAmount != nil && *req.MaxPriceAmount < 0 {
		httputil.RespondError(w, http.StatusBadRequest, "max_price_amount must be nonnegative")
		return
	}
	if req.MinQuantity != nil && *req.MinQuantity <= 0 {
		httputil.RespondError(w, http.StatusBadRequest, "min_quantity must be positive")
		return
	}
	if req.RequiredAttributes != nil {
		var attrs map[string]any
		if json.Unmarshal(req.RequiredAttributes, &attrs) != nil {
			httputil.RespondError(w, http.StatusBadRequest, "required_attributes must be a JSON object")
			return
		}
	}
	item, err := h.interestRepo.Update(r.Context(), id, userID, req)
	if err != nil {
		httputil.RespondError(w, http.StatusNotFound, "Interest not found")
		return
	}
	httputil.RespondJSON(w, http.StatusOK, item)
}

func (h *InterestHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid interest ID")
		return
	}

	if err := h.interestRepo.Delete(r.Context(), id, userID); err != nil {
		httputil.RespondError(w, http.StatusNotFound, err.Error())
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]string{"message": "Interest deleted successfully"})
}

func (h *InterestHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid interest ID")
		return
	}
	var req domain.UpdateInterestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.MaxPriceAmount != nil && *req.MaxPriceAmount < 0 {
		httputil.RespondError(w, http.StatusBadRequest, "max_price_amount must be nonnegative")
		return
	}
	if req.MinQuantity != nil && *req.MinQuantity <= 0 {
		httputil.RespondError(w, http.StatusBadRequest, "min_quantity must be positive")
		return
	}
	if req.RequiredAttributes != nil {
		var attrs map[string]any
		if json.Unmarshal(req.RequiredAttributes, &attrs) != nil {
			httputil.RespondError(w, http.StatusBadRequest, "required_attributes must be a JSON object")
			return
		}
	}
	item, err := h.interestRepo.Update(r.Context(), id, userID, req)
	if err != nil {
		httputil.RespondError(w, http.StatusNotFound, "Interest not found")
		return
	}
	httputil.RespondJSON(w, http.StatusOK, item)
}
