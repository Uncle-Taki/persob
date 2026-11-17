package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/conversation"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

type ConversationHandler struct {
	convRepo      *postgres.ConversationRepo
	mongoConvRepo *mongodb.ConversationRepo
	convManager   *conversation.Manager
}

func NewConversationHandler(
	convRepo *postgres.ConversationRepo,
	mongoConvRepo *mongodb.ConversationRepo,
	convManager *conversation.Manager,
) *ConversationHandler {
	return &ConversationHandler{
		convRepo:      convRepo,
		mongoConvRepo: mongoConvRepo,
		convManager:   convManager,
	}
}

func (h *ConversationHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	if limit <= 0 {
		limit = 20
	}

	userID := httputil.GetUserIDFromContext(r.Context())
	if userID == uuid.Nil {
		httputil.RespondError(w, 401, "Authentication required")
		return
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		httputil.RespondError(w, 400, "Invalid offset")
		return
	}
	var list []domain.Conversation
	var total int64
	var err error
	if isWorkspaceAdmin(r) {
		list, total, err = h.convRepo.List(r.Context(), state, limit, offset)
	} else {
		list, total, err = h.convRepo.ListVisibleToUser(r.Context(), userID, state, limit, offset)
	}
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list conversations")
		return
	}

	httputil.RespondWithMeta(w, http.StatusOK, list, httputil.PaginationMeta{
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (h *ConversationHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid conversation ID")
		return
	}

	var conv *domain.Conversation
	if isWorkspaceAdmin(r) {
		conv, err = h.convRepo.GetByID(r.Context(), id)
	} else {
		conv, err = h.convRepo.GetByIDForUser(r.Context(), id, httputil.GetUserIDFromContext(r.Context()))
	}
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get conversation")
		return
	}
	if conv == nil || (!isWorkspaceAdmin(r) && (conv.OwnerID == nil || *conv.OwnerID != httputil.GetUserIDFromContext(r.Context()))) {
		httputil.RespondError(w, http.StatusNotFound, "Conversation not found")
		return
	}

	// Fetch message history from MongoDB
	messages, err := h.convRepo.QueueHistory(r.Context(), id)
	if err != nil {
		messages = nil
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]any{
		"conversation": conv,
		"messages":     messages,
	})
}

func (h *ConversationHandler) Approve(w http.ResponseWriter, r *http.Request) {
	if !isWorkspaceAdmin(r) {
		httputil.RespondError(w, http.StatusForbidden, "Administrator approval required")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid conversation ID")
		return
	}

	userID := httputil.GetUserIDFromContext(r.Context())
	if err := h.convManager.ApproveAsAdmin(r.Context(), id, userID); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Failed to approve conversation")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]string{"message": "Conversation approved successfully"})
}

func (h *ConversationHandler) Pause(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid conversation ID")
		return
	}

	if !h.authorized(w, r, id) {
		return
	}
	if err := h.convManager.Pause(r.Context(), id); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Failed to pause conversation")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]string{"message": "Conversation paused"})
}

func (h *ConversationHandler) Close(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid conversation ID")
		return
	}

	if !h.authorized(w, r, id) {
		return
	}
	if err := h.convManager.Close(r.Context(), id); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Failed to close conversation")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]string{"message": "Conversation closed"})
}

func (h *ConversationHandler) authorized(w http.ResponseWriter, r *http.Request, id uuid.UUID) bool {
	var c *domain.Conversation
	var err error
	if isWorkspaceAdmin(r) {
		c, err = h.convRepo.GetByID(r.Context(), id)
	} else {
		c, err = h.convRepo.GetByIDForUser(r.Context(), id, httputil.GetUserIDFromContext(r.Context()))
	}
	if err != nil {
		httputil.RespondError(w, 500, "Unable to load conversation")
		return false
	}
	if c == nil || (!isWorkspaceAdmin(r) && (c.OwnerID == nil || *c.OwnerID != httputil.GetUserIDFromContext(r.Context()))) {
		httputil.RespondError(w, 404, "Conversation not found")
		return false
	}
	return true
}
