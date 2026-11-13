package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
	"github.com/perso/perso-mind/internal/telegram"
)

type TelegramSourceHandler struct {
	sourceRepo *postgres.TelegramSourceRepo
	resolver   *telegram.Resolver
}

func NewTelegramSourceHandler(
	sourceRepo *postgres.TelegramSourceRepo,
	resolver *telegram.Resolver,
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

	if req.Address == "" {
		httputil.RespondError(w, http.StatusBadRequest, "address is required (username, link, or ID)")
		return
	}

	resp, err := h.resolver.ResolveSource(r.Context(), req.Address, req.ExpectedType)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Failed to resolve Telegram entity: "+err.Error())
		return
	}

	httputil.RespondJSON(w, http.StatusOK, resp)
}

// Create resolves and persists a verified Telegram source
func (h *TelegramSourceHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())

	var req domain.CreateTelegramSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Address == "" && req.TelegramPeerID == 0 {
		httputil.RespondError(w, http.StatusBadRequest, "address or telegram_peer_id is required")
		return
	}

	// 1. Resolve source if metadata was not fully supplied
	var resolved *domain.ResolveSourceResponse
	var err error

	if req.Address != "" {
		resolved, err = h.resolver.ResolveSource(r.Context(), req.Address, req.ExpectedType)
		if err != nil {
			httputil.RespondError(w, http.StatusBadRequest, "Failed to resolve Telegram entity: "+err.Error())
			return
		}
	}

	peerID := req.TelegramPeerID
	accessHash := req.TelegramAccessHash
	username := req.Username
	title := req.Title
	detectedType := req.SourceType
	accessType := req.AccessType
	linkedDiscussion := req.LinkedDiscussionPeerID
	var capabilities domain.SourceCapabilities

	if resolved != nil {
		peerID = resolved.PeerID
		accessHash = resolved.AccessHash
		if username == "" {
			username = resolved.Username
		}
		if title == "" {
			title = resolved.Title
		}
		detectedType = resolved.DetectedType
		accessType = resolved.AccessType
		if linkedDiscussion == nil {
			linkedDiscussion = resolved.LinkedDiscussionPeerID
		}
		capabilities = resolved.Capabilities

		// Authorization & Private access check: do not allow private source if account cannot access
		if !resolved.CanIngest {
			httputil.RespondError(w, http.StatusForbidden, "Connected Telegram account does not have access to this private source")
			return
		}
	} else if req.Capabilities != nil {
		capabilities = *req.Capabilities
	}

	sourceType := detectedType
	if sourceType == "" {
		sourceType = domain.TelegramSourceTypeBroadcastChannel
	}
	if accessType == "" {
		accessType = domain.AccessTypePublic
	}

	source := &domain.TelegramSource{
		ID:                     uuid.New(),
		OwnerID:                userID,
		TelegramPeerID:         peerID,
		TelegramAccessHash:     accessHash,
		Username:               username,
		Title:                  title,
		SourceType:             sourceType,
		AccessType:             accessType,
		ExpectedType:           req.ExpectedType,
		DetectedType:           sourceType,
		LinkedDiscussionPeerID: linkedDiscussion,
		Capabilities:           capabilities,
		IsActive:               true,
		IngestionStatus:        domain.IngestionStatusActive,
		LastMessageID:          0,
	}

	if err := h.sourceRepo.Create(r.Context(), source); err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to create telegram source: "+err.Error())
		return
	}

	httputil.RespondJSON(w, http.StatusCreated, source)
}

// List returns all configured Telegram sources for the active user
func (h *TelegramSourceHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httputil.GetUserIDFromContext(r.Context())

	sources, err := h.sourceRepo.ListByOwner(r.Context(), userID)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list telegram sources: "+err.Error())
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

	var req domain.UpdateTelegramSourceStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.sourceRepo.UpdateStatus(r.Context(), id, req.IngestionStatus, req.IsActive); err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to update source status: "+err.Error())
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
