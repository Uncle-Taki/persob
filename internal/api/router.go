package api

import (
	_ "embed"
	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/identity"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/perso/perso-mind/internal/api/handlers"
)

//go:embed openapi.yaml
var openapiSpec []byte

type Handlers struct {
	Identity        *identity.Service
	OutreachEnabled bool

	APIToken       string
	OperatorUserID uuid.UUID
	GatewayHandler http.Handler

	// Demo identity is an explicit local-development opt-in, never authentication.
	AllowDemoAuth         bool
	HealthHandler         *handlers.HealthHandler
	InterestHandler       *handlers.InterestHandler
	WatchSourceHandler    *handlers.WatchSourceHandler
	TelegramSourceHandler *handlers.TelegramSourceHandler
	ProductHandler        *handlers.ProductHandler
	OfferHandler          *handlers.OfferHandler
	ConversationHandler   *handlers.ConversationHandler
	AnalyticsHandler      *handlers.AnalyticsHandler
}

func NewRouter(h *Handlers) http.Handler {
	r := chi.NewRouter()

	// Global Middlewares
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	// Do not trust client-supplied proxy IP headers for login rate limiting.
	r.Use(LoggingMiddleware)
	// The deployed frontend uses the same origin. No wildcard CORS credentials.
	r.Use(chimiddleware.Timeout(60 * 1000000000))
	r.Use(chimiddleware.Throttle(100))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			next.ServeHTTP(w, r)
		})
	})

	// Health and Readiness
	r.Get("/health", h.HealthHandler.Health)
	r.Get("/ready", h.HealthHandler.Ready)

	// OpenAPI Specification
	r.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapiSpec)
	})

	// The machine token can only read aggregate service status; it never authenticates a browser account.
	if h.Identity != nil {
		r.Post("/api/v1/auth/login", h.Identity.Login)
		r.With(TokenAuthenticationMiddleware(h.APIToken, h.OperatorUserID)).Get("/api/v1/operations/status", func(w http.ResponseWriter, r *http.Request) {
			if h.GatewayHandler == nil {
				httputil.RespondJSON(w, 200, map[string]any{"connected": false, "authorization_required": true})
				return
			}
			h.GatewayHandler.ServeHTTP(w, r)
		})
	}
	// API v1 Routes
	r.Route("/api/v1", func(r chi.Router) {
		if h.Identity != nil {
			r.Use(h.Identity.Middleware)
			r.Use(h.Identity.Marketplace)
		} else {
			r.Use(AuthenticationMiddleware(h.AllowDemoAuth))
		}
		if h.Identity != nil {
			r.Post("/auth/logout", h.Identity.Logout)
			r.Get("/me", h.Identity.Me)
			r.Patch("/me", h.Identity.Profile)
			r.Get("/me/preferences", h.Identity.Preferences)
			r.Patch("/me/preferences", h.Identity.Preferences)
			r.Post("/me/password", h.Identity.Password)
			r.Get("/users", h.Identity.ListUsers)
			r.Post("/users", h.Identity.CreateUser)
			r.Get("/users/{id}", h.Identity.GetUser)
			r.Patch("/users/{id}", h.Identity.UpdateUser)
			r.Post("/users/{id}/reset-password", h.Identity.ResetPassword)
		}
		r.Get("/session", func(w http.ResponseWriter, r *http.Request) {
			if h.Identity != nil {
				h.Identity.Session(w, r, h.OutreachEnabled)
				return
			}
			httputil.RespondJSON(w, 200, map[string]any{"user_id": httputil.GetUserIDFromContext(r.Context()), "catalog_domain": "electronics", "workspace_mode": "single_operator", "demo": h.AllowDemoAuth, "outreach_enabled": h.OutreachEnabled})
		})
		if h.GatewayHandler != nil {
			r.Mount("/telegram", h.GatewayHandler)
		}
		// Interests
		r.Route("/interests", func(r chi.Router) {
			r.Get("/", h.InterestHandler.List)
			r.Post("/", h.InterestHandler.Create)
			r.Delete("/{id}", h.InterestHandler.Delete)
		})

		// Legacy route names delegate to the authoritative normalized registry.
		if h.TelegramSourceHandler != nil {
			r.Route("/watch-sources", func(r chi.Router) {
				r.Get("/", h.TelegramSourceHandler.List)
				r.Post("/", h.TelegramSourceHandler.Create)
				r.Delete("/{id}", h.TelegramSourceHandler.Delete)
			})
		}

		// Telegram Sources (Type-Aware Normalized Registry)
		if h.TelegramSourceHandler != nil {
			r.Route("/sources", func(r chi.Router) {
				r.Post("/resolve", h.TelegramSourceHandler.Resolve)
				r.Get("/", h.TelegramSourceHandler.List)
				r.Post("/", h.TelegramSourceHandler.Create)
				r.Get("/{id}", h.TelegramSourceHandler.Get)
				r.Get("/{id}/capabilities", h.TelegramSourceHandler.GetCapabilities)
				if h.Identity != nil {
					r.Get("/{id}/assignments", h.Identity.SourceAssignments)
					r.Put("/{id}/assignments", h.Identity.SourceAssignments)
				}
				r.Put("/{id}/status", h.TelegramSourceHandler.UpdateStatus)
				r.Delete("/{id}", h.TelegramSourceHandler.Delete)
			})
		}

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

		// Matches
		r.Get("/matches", h.OfferHandler.ListMatches)

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
		r.Get("/analytics/comparisons/condition", h.AnalyticsHandler.CompareCondition)
		r.Get("/analytics/comparisons/city", h.AnalyticsHandler.CompareCity)
		r.Get("/analytics/comparisons/brand", h.AnalyticsHandler.CompareBrand)
	})

	return r
}
