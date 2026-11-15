package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"github.com/perso/perso-mind/internal/telegram"
)

type TelegramSourceHandler struct {
	sourceRepo *postgres.TelegramSourceRepo
	resolver   telegram.SourceResolver
}

func NewTelegramSourceHandler(
	sourceRepo *postgres.TelegramSourceRepo,
	resolver telegram.SourceResolver,
) *TelegramSourceHandler {
	return &TelegramSourceHandler{
		sourceRepo: sourceRepo,
		resolver:   resolver,
	}
}

// Resolve resolves a Telegram entity without persisting it
func (h *TelegramSourceHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	var req domain.ResolveSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if strings.TrimSpace(req.Address) == "" || !validExpectedType(req.ExpectedType) {
		httputil.RespondError(w, http.StatusBadRequest, "address is required (username, link, or ID)")
		return
	}

	resp, err := h.resolver.ResolveSource(r.Context(), req.Address, req.ExpectedType)
	if err != nil {
		respondResolutionError(w, err)
		return
	}

	httputil.RespondJSON(w, http.StatusOK, resp)
}

// Create always resolves the address; client-supplied entity metadata is untrusted.
func (h *TelegramSourceHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())
	var req domain.CreateTelegramSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.Address) == "" || !validExpectedType(req.ExpectedType) {
		httputil.RespondError(w, http.StatusBadRequest, "A Telegram address and a supported expected_type are required")
		return
	}
	resolved, err := h.resolver.ResolveSource(r.Context(), req.Address, req.ExpectedType)
	if err != nil {
		respondResolutionError(w, err)
		return
	}
	if !resolved.AccountHasAccess || !resolved.CanIngest || !resolved.Capabilities.CanReadHistory || resolved.PeerID == 0 {
		httputil.RespondError(w, http.StatusForbidden, "Connected Telegram account cannot read this source")
		return
	}
	if resolved.TypeMismatch {
		httputil.RespondError(w, http.StatusConflict, "Requested source type does not match the Telegram entity")
		return
	}
	source := &domain.TelegramSource{
		ID: uuid.New(), OwnerID: userID, ExternalLLMAllowed: req.ExternalLLMAllowed,
		TelegramPeerID: resolved.PeerID, TelegramAccessHash: resolved.AccessHash,
		Username: resolved.Username, Title: resolved.Title,
		SourceType: resolved.DetectedType, DetectedType: resolved.DetectedType,
		AccessType: resolved.AccessType, ExpectedType: req.ExpectedType,
		LinkedDiscussionPeerID: resolved.LinkedDiscussionPeerID, Capabilities: resolved.Capabilities,
		IsActive: true, IngestionStatus: domain.IngestionStatusActive,
	}
	if err := h.sourceRepo.Create(r.Context(), source); err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to create Telegram source")
		return
	}
	httputil.RespondJSON(w, http.StatusCreated, source)
}

func validExpectedType(t domain.TelegramSourceType) bool {
	switch t {
	case "", domain.TelegramSourceTypeBasicGroup, domain.TelegramSourceTypeSupergroup, domain.TelegramSourceTypeForum, domain.TelegramSourceTypeBroadcastChannel:
		return true
	}
	return false
}

func respondResolutionError(w http.ResponseWriter, err error) {
	if errors.Is(err, telegram.ErrResolverUnavailable) {
		httputil.RespondError(w, http.StatusServiceUnavailable, "Telegram resolver is unavailable")
		return
	}
	// Never echo private invite hashes or upstream error payloads into API responses.
	httputil.RespondError(w, http.StatusBadRequest, "Unable to resolve the Telegram source")
}

// List returns all configured Telegram sources for the active user
func (h *TelegramSourceHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())

	sources, err := h.sourceRepo.ListByOwner(r.Context(), userID)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list telegram sources")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, sources)
}

// Get returns details for a specific Telegram source
func (h *TelegramSourceHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid source ID")
		return
	}

	source, err := h.sourceRepo.GetByID(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, http.StatusNotFound, "Telegram source not found")
		return
	}

	userID := httputil.GetUserIDFromContext(r.Context())
	if source.OwnerID != userID {
		httputil.RespondError(w, http.StatusForbidden, "Access denied")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, source)
}

// GetCapabilities returns the capabilities JSON for a specific source
func (h *TelegramSourceHandler) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid source ID")
		return
	}

	source, err := h.sourceRepo.GetByID(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, http.StatusNotFound, "Telegram source not found")
		return
	}

	if source.OwnerID != httputil.GetUserIDFromContext(r.Context()) {
		httputil.RespondError(w, http.StatusForbidden, "Access denied")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"id":           source.ID,
		"source_type":  source.SourceType,
		"capabilities": source.Capabilities,
	})
}

// UpdateStatus updates the ingestion status and active flag of a source
func (h *TelegramSourceHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid source ID")
		return
	}

	source, err := h.sourceRepo.GetByID(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, http.StatusNotFound, "Source not found")
		return
	}
	if source.OwnerID != httputil.GetUserIDFromContext(r.Context()) {
		httputil.RespondError(w, http.StatusForbidden, "Access denied")
		return
	}

	var req domain.UpdateTelegramSourceStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	switch req.IngestionStatus {
	case domain.IngestionStatusActive, domain.IngestionStatusPaused, domain.IngestionStatusError, domain.IngestionStatusUnauthorized:
	default:
		httputil.RespondError(w, http.StatusBadRequest, "Invalid ingestion status")
		return
	}

	if err := h.sourceRepo.UpdateStatus(r.Context(), id, req.IngestionStatus, req.IsActive); err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to update source status")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]string{"message": "Source status updated successfully"})
}

// Delete removes a configured Telegram source
func (h *TelegramSourceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid source ID")
		return
	}

	if err := h.sourceRepo.Delete(r.Context(), id, userID); err != nil {
		httputil.RespondError(w, http.StatusNotFound, err.Error())
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]string{"message": "Telegram source deleted successfully"})
}
