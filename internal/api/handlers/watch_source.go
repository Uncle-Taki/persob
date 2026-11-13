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

type WatchSourceHandler struct {
	watchSourceRepo *postgres.WatchSourceRepo
}

func NewWatchSourceHandler(watchSourceRepo *postgres.WatchSourceRepo) *WatchSourceHandler {
	return &WatchSourceHandler{
		watchSourceRepo: watchSourceRepo,
	}
}

func (h *WatchSourceHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())

	list, err := h.watchSourceRepo.ListByUserID(r.Context(), userID)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list watch sources: "+err.Error())
		return
	}

	httputil.RespondJSON(w, http.StatusOK, list)
}

func (h *WatchSourceHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())

	var req domain.CreateWatchSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.TelegramChatID == 0 {
		httputil.RespondError(w, http.StatusBadRequest, "telegram_chat_id is required")
		return
	}
	if req.Title == "" {
		httputil.RespondError(w, http.StatusBadRequest, "title is required")
		return
	}

	source := &domain.WatchSource{
		ID:               uuid.New(),
		UserID:           userID,
		TelegramChatID:   req.TelegramChatID,
		Username:         req.Username,
		Title:            req.Title,
		SourceType:       req.SourceType,
		SellEnabled:      false, // Phase 1: false
		BuyEnabled:       true,  // Phase 1: true
		MonitoringStatus: domain.MonitoringStatusActive,
	}

	if err := h.watchSourceRepo.Create(r.Context(), source); err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to create watch source: "+err.Error())
		return
	}

	httputil.RespondJSON(w, http.StatusCreated, source)
}

func (h *WatchSourceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid watch source ID")
		return
	}

	if err := h.watchSourceRepo.Delete(r.Context(), id, userID); err != nil {
		httputil.RespondError(w, http.StatusNotFound, err.Error())
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]string{"message": "Watch source deleted successfully"})
}
