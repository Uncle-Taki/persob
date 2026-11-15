package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/queue"
	"github.com/perso/perso-mind/internal/storage/mongodb"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

type HealthHandler struct {
	pgDB    *postgres.DB
	mongoDB *mongodb.DB
	redis   *queue.Client
}

func NewHealthHandler(pgDB *postgres.DB, mongoDB *mongodb.DB, redis *queue.Client) *HealthHandler {
	return &HealthHandler{
		pgDB:    pgDB,
		mongoDB: mongoDB,
		redis:   redis,
	}
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	httputil.RespondJSON(w, http.StatusOK, map[string]any{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "perso-backend",
	})
}

func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := make(map[string]string)
	allReady := true

	// Check Postgres
	if h.pgDB != nil {
		if err := h.pgDB.Ping(ctx); err != nil {
			checks["postgres"] = "down"
			allReady = false
		} else {
			checks["postgres"] = "up"
		}
	}

	// Check MongoDB
	if h.mongoDB != nil && h.mongoDB.Client != nil {
		if err := h.mongoDB.Client.Ping(ctx, nil); err != nil {
			checks["mongodb"] = "down"
			allReady = false
		} else {
			checks["mongodb"] = "up"
		}
	}

	// Check Redis
	if h.redis != nil && h.redis.Rdb != nil {
		if err := h.redis.Rdb.Ping(ctx).Err(); err != nil {
			checks["redis"] = "down"
			allReady = false
		} else {
			checks["redis"] = "up"
		}
	}

	status := http.StatusOK
	if !allReady {
		status = http.StatusServiceUnavailable
	}

	httputil.RespondJSON(w, status, map[string]any{
		"ready":     allReady,
		"checks":    checks,
		"timestamp": time.Now().UTC(),
	})
}
