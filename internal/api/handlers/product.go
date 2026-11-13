package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

type ProductHandler struct {
	productRepo     *postgres.ProductRepo
	analyticsEngine *analytics.Engine
}

func NewProductHandler(productRepo *postgres.ProductRepo, analyticsEngine *analytics.Engine) *ProductHandler {
	return &ProductHandler{
		productRepo:     productRepo,
		analyticsEngine: analyticsEngine,
	}
}

func (h *ProductHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	products, total, err := h.productRepo.List(r.Context(), limit, offset, search, category)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list products: "+err.Error())
		return
	}

	httputil.RespondWithMeta(w, http.StatusOK, products, httputil.PaginationMeta{
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (h *ProductHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid product ID")
		return
	}

	product, err := h.productRepo.GetByID(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get product: "+err.Error())
		return
	}
	if product == nil {
		httputil.RespondError(w, http.StatusNotFound, "Product not found")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, product)
}

func (h *ProductHandler) GetStatistics(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid product ID")
		return
	}

	stats, err := h.analyticsEngine.CalculateProductStatistics(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to calculate statistics: "+err.Error())
		return
	}

	httputil.RespondJSON(w, http.StatusOK, stats)
}
