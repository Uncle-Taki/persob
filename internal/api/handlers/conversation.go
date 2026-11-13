package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/conversation"
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

	list, total, err := h.convRepo.List(r.Context(), state, limit, offset)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to list conversations: "+err.Error())
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

	conv, err := h.convRepo.GetByID(r.Context(), id)
	if err != nil {
		httputil.RespondError(w, http.StatusInternalServerError, "Failed to get conversation: "+err.Error())
		return
	}
	if conv == nil {
		httputil.RespondError(w, http.StatusNotFound, "Conversation not found")
		return
	}

	// Fetch message history from MongoDB
	messages, err := h.mongoConvRepo.GetMessagesByConversationID(r.Context(), id.String())
	if err != nil {
		messages = nil
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]any{
		"conversation": conv,
		"messages":     messages,
	})
}

func (h *ConversationHandler) Approve(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Invalid conversation ID")
		return
	}

	userID := httputil.GetUserIDFromContext(r.Context())
	if err := h.convManager.Approve(r.Context(), id, userID); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Failed to approve conversation: "+err.Error())
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

	if err := h.convManager.Pause(r.Context(), id); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Failed to pause conversation: "+err.Error())
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

	if err := h.convManager.Close(r.Context(), id); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "Failed to close conversation: "+err.Error())
		return
	}

	httputil.RespondJSON(w, http.StatusOK, map[string]string{"message": "Conversation closed"})
}
