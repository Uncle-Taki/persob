package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/analytics"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/identity"
	"github.com/perso/perso-mind/internal/nlp"
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

func catalogAdmin(r *http.Request) bool {
	u := identity.Current(r.Context())
	return u != nil && u.Active && (u.Role == domain.RoleAdmin || u.Role == domain.RoleSuperAdmin)
}

func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !catalogAdmin(r) {
		httputil.RespondError(w, http.StatusForbidden, "Administrator access required")
		return
	}
	var req domain.CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.Name, req.Category = strings.TrimSpace(req.Name), strings.TrimSpace(req.Category)
	if req.Name == "" || req.Category == "" {
		httputil.RespondError(w, http.StatusBadRequest, "name and category are required")
		return
	}
	if req.NormalizedName == "" {
		req.NormalizedName = nlp.CleanSearchKey(req.Name)
	}
	if len(req.Attributes) == 0 {
		req.Attributes = []byte("{}")
	}
	var attrs map[string]any
	if json.Unmarshal(req.Attributes, &attrs) != nil {
		httputil.RespondError(w, http.StatusBadRequest, "attributes must be a JSON object")
		return
	}
	p := &domain.Product{Name: req.Name, NormalizedName: req.NormalizedName, Brand: strings.TrimSpace(req.Brand), Manufacturer: strings.TrimSpace(req.Manufacturer), Model: strings.TrimSpace(req.Model), Category: req.Category, Attributes: req.Attributes, Unit: strings.TrimSpace(req.Unit), PackSize: req.PackSize, PrescriptionRequired: req.PrescriptionRequired, ExpiryRequired: req.ExpiryRequired}
	if err := h.productRepo.Create(r.Context(), p); err != nil {
		httputil.RespondError(w, http.StatusConflict, "Product could not be created")
		return
	}
	httputil.RespondJSON(w, http.StatusCreated, p)
}

func (h *ProductHandler) Update(w http.ResponseWriter, r *http.Request) {
	if !catalogAdmin(r) {
		httputil.RespondError(w, http.StatusForbidden, "Administrator access required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid product ID")
		return
	}
	var req domain.UpdateProductRequest
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Attributes != nil {
		var attrs map[string]any
		if json.Unmarshal(req.Attributes, &attrs) != nil {
			httputil.RespondError(w, http.StatusBadRequest, "attributes must be a JSON object")
			return
		}
	}
	p, err := h.productRepo.Update(r.Context(), id, req)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to update product")
		return
	}
	if p == nil {
		httputil.RespondError(w, http.StatusNotFound, "Product not found")
		return
	}
	httputil.RespondJSON(w, http.StatusOK, p)
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

	if offset < 0 {
		httputil.RespondError(w, 400, "offset must be nonnegative")
		return
	}
	products, total, err := h.productRepo.List(r.Context(), limit, offset, search, category)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list products")
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
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get product")
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
	product, err := h.productRepo.GetByID(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get product")
		return
	}
	if product == nil {
		httputil.RespondError(w, http.StatusNotFound, "Product not found")
		return
	}

	var stats *domain.PriceStatistics
	if isWorkspaceAdmin(r) {
		stats, err = h.analyticsEngine.CalculateProductStatistics(r.Context(), id)
	} else {
		stats, err = h.analyticsEngine.CalculateProductStatisticsForUser(r.Context(), id, httputil.GetUserIDFromContext(r.Context()))
	}
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to calculate statistics")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, stats)
}
