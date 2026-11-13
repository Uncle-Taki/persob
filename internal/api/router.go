package api

import (
	_ "embed"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/perso/perso-mind/internal/api/handlers"
)

//go:embed openapi.yaml
var openapiSpec []byte

type Handlers struct {
	HealthHandler       *handlers.HealthHandler
	InterestHandler     *handlers.InterestHandler
	WatchSourceHandler  *handlers.WatchSourceHandler
	ProductHandler      *handlers.ProductHandler
	OfferHandler        *handlers.OfferHandler
	ConversationHandler *handlers.ConversationHandler
	AnalyticsHandler    *handlers.AnalyticsHandler
}

func NewRouter(h *Handlers) http.Handler {
	r := chi.NewRouter()

	// Global Middlewares
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(LoggingMiddleware)
	r.Use(CORSMiddleware)
	r.Use(UserAuthMockMiddleware)

	// Health and Readiness
	r.Get("/health", h.HealthHandler.Health)
	r.Get("/ready", h.HealthHandler.Ready)

	// OpenAPI Specification
	r.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapiSpec)
	})

	// API v1 Routes
	r.Route("/api/v1", func(r chi.Router) {
		// Interests
		r.Route("/interests", func(r chi.Router) {
			r.Get("/", h.InterestHandler.List)
			r.Post("/", h.InterestHandler.Create)
			r.Delete("/{id}", h.InterestHandler.Delete)
		})

		// Watch Sources
		r.Route("/watch-sources", func(r chi.Router) {
			r.Get("/", h.WatchSourceHandler.List)
			r.Post("/", h.WatchSourceHandler.Create)
			r.Delete("/{id}", h.WatchSourceHandler.Delete)
		})

		// Products
		r.Route("/products", func(r chi.Router) {
			r.Get("/", h.ProductHandler.List)
			r.Get("/{id}", h.ProductHandler.GetByID)
			r.Get("/{id}/statistics", h.ProductHandler.GetStatistics)
		})

		// Offers
		r.Route("/offers", func(r chi.Router) {
			r.Get("/", h.OfferHandler.List)
			r.Get("/{id}", h.OfferHandler.GetByID)
		})

		// Conversations
		r.Route("/conversations", func(r chi.Router) {
			r.Get("/", h.ConversationHandler.List)
			r.Get("/{id}", h.ConversationHandler.GetByID)
			r.Post("/{id}/approve", h.ConversationHandler.Approve)
			r.Post("/{id}/pause", h.ConversationHandler.Pause)
			r.Post("/{id}/close", h.ConversationHandler.Close)
		})

		// Dashboard & Analytics
		r.Get("/dashboard/summary", h.AnalyticsHandler.DashboardSummary)
		r.Get("/analytics/prices", h.AnalyticsHandler.PriceAnalytics)
		r.Get("/analytics/offers", h.AnalyticsHandler.OfferAnalytics)
	})

	return r
}
