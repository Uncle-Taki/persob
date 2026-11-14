package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/api/httputil"
)

// LoggingMiddleware logs incoming HTTP requests with method, path, and duration
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"duration", duration,
		)
	})
}

// CORSMiddleware enables cross-origin resource sharing for web frontend
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-User-ID")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// AuthenticationMiddleware fails closed until a real identity provider is wired.
// Demo access must be explicitly enabled by the development composition root.
func AuthenticationMiddleware(allowDemo bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if allowDemo {
			return UserAuthMockMiddleware(next)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httputil.RespondError(w, http.StatusUnauthorized, "Authentication is not configured")
		})
	}
}

// UserAuthMockMiddleware provides only the fixed demo identity. Caller-supplied
// X-User-ID is never an authenticated identity, even in local demo mode.
func UserAuthMockMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

		ctx := context.WithValue(r.Context(), httputil.UserContextKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
