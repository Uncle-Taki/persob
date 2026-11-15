package httputil

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/google/uuid"
)

type ContextKey string

const (
	UserContextKey ContextKey = "user_id"
)

type APIResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
	Meta    any    `json:"meta,omitempty"`
}

type PaginationMeta struct {
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

func RespondJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Data:    normalizeList(data),
	})
}

func RespondWithMeta(w http.ResponseWriter, statusCode int, data any, meta any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Data:    normalizeList(data),
		Meta:    meta,
	})
}

func RespondError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Error:   message,
	})
}

func GetUserIDFromContext(ctx context.Context) uuid.UUID {
	if val, ok := ctx.Value(UserContextKey).(uuid.UUID); ok {
		return val
	}
	return uuid.Nil
}

func normalizeList(value any) any {
	if value == nil {
		return value
	}
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Slice && v.IsNil() {
		return []any{}
	}
	return value
}
