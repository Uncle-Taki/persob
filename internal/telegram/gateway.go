package telegram

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/perso/perso-mind/internal/domain"
	"io"
	"net/http"
	"strings"
	"time"
)

// SourceResolver keeps HTTP handlers independent of the session-owning process.
type SourceResolver interface {
	ResolveSource(context.Context, string, domain.TelegramSourceType) (*domain.ResolveSourceResponse, error)
}
type GatewayClient struct {
	URL, Token string
	HTTP       *http.Client
}

func NewGatewayClient(url, token string) *GatewayClient {
	return &GatewayClient{URL: strings.TrimRight(url, "/"), Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}
func (g *GatewayClient) request(ctx context.Context, path string, body any, out any) error {
	var input io.Reader
	method := http.MethodGet
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(b)
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, g.URL+path, input)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Content-Type", "application/json")
	response, err := g.HTTP.Do(req)
	if err != nil {
		return ErrResolverUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return ErrResolverUnavailable
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
}
func (g *GatewayClient) ResolveSource(ctx context.Context, address string, expected domain.TelegramSourceType) (*domain.ResolveSourceResponse, error) {
	var out domain.ResolveSourceResponse
	err := g.request(ctx, "/internal/resolve", domain.ResolveSourceRequest{Address: address, ExpectedType: expected}, &out)
	return &out, err
}
func (g *GatewayClient) Status(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := g.request(ctx, "/internal/status", nil, &out)
	return out, err
}
func (g *GatewayClient) StatusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		status, err := g.Status(r.Context())
		if err != nil {
			http.Error(w, "Telegram gateway unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": status})
	})
}
func (c *Collector) GatewayHandler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"connected": c.Connected(), "mock": c.mockClient != nil, "authorization_required": !c.Connected()})
	})
	mux.HandleFunc("POST /internal/resolve", func(w http.ResponseWriter, r *http.Request) {
		if !c.Connected() {
			http.Error(w, "Telegram not authorized", 503)
			return
		}
		var req domain.ResolveSourceRequest
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		resolved, err := c.Resolver().ResolveSource(r.Context(), req.Address, req.ExpectedType)
		if err != nil {
			http.Error(w, "Unable to resolve Telegram source", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resolved)
	})
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		supplied := r.Header.Get("Authorization")
		actual := sha256.Sum256([]byte(strings.TrimPrefix(supplied, "Bearer ")))
		if token == "" || !strings.HasPrefix(supplied, "Bearer ") || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (c *Collector) RunGateway(ctx context.Context) error {
	if c.cfg.GatewayToken == "" {
		return fmt.Errorf("TELEGRAM_GATEWAY_TOKEN is required")
	}
	srv := &http.Server{Addr: c.cfg.GatewayAddr, Handler: c.GatewayHandler(c.cfg.GatewayToken), ReadTimeout: 5 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	go func() { errs <- c.Start(runCtx) }()
	go func() { errs <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err := <-errs:
		cancel()
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = srv.Shutdown(shutdown)
		return err
	}
	shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	return srv.Shutdown(shutdown)
}
