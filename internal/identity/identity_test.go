package identity_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/perso/perso-mind/internal/identity"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

const password = "initial-test-password-123"

type browser struct {
	client     *http.Client
	base, csrf string
}

func database(t *testing.T, migrate bool) *postgres.DB {
	t.Helper()
	dsn := os.Getenv("PERSO_IDENTITY_TEST_DSN")
	if dsn == "" {
		t.Skip("Set PERSO_IDENTITY_TEST_DSN to a disposable local database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" {
		t.Fatal("Identity tests refuse a non-local database")
	}
	admin, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	schema := "identity_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	db := &postgres.DB{Pool: pool}
	if migrate {
		if err = db.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func router(s *identity.Service) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/auth/login", s.Login)
	r.Group(func(r chi.Router) {
		r.Use(s.Middleware)
		r.Use(s.Marketplace)
		r.Get("/api/v1/session", func(w http.ResponseWriter, r *http.Request) { s.Session(w, r, false) })
		r.Post("/api/v1/auth/logout", s.Logout)
		r.Get("/api/v1/me", s.Me)
		r.Patch("/api/v1/me", s.Profile)
		r.Get("/api/v1/me/preferences", s.Preferences)
		r.Patch("/api/v1/me/preferences", s.Preferences)
		r.Post("/api/v1/me/password", s.Password)
		r.Get("/api/v1/users", s.ListUsers)
		r.Post("/api/v1/users", s.CreateUser)
		r.Get("/api/v1/users/{id}", s.GetUser)
		r.Patch("/api/v1/users/{id}", s.UpdateUser)
		r.Post("/api/v1/users/{id}/reset-password", s.ResetPassword)
		r.Get("/api/v1/offers", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
		r.Get("/api/v1/watch-sources", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
		r.Get("/api/v1/products/{id}/statistics", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	})
	return r
}
func setup(t *testing.T) (*postgres.DB, *identity.Service, *httptest.Server, *browser) {
	t.Helper()
	db := database(t, true)
	s := identity.New(db.Pool, false)
	if err := s.Bootstrap(context.Background(), uuid.MustParse("11111111-1111-1111-1111-111111111111"), "owner", password); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router(s))
	t.Cleanup(server.Close)
	b := newBrowser(server.URL)
	b.login(t, "owner", password)
	return db, s, server, b
}
func newBrowser(base string) *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{client: &http.Client{Jar: jar}, base: base}
}
func (b *browser) request(t *testing.T, method, path string, body any, want int) map[string]json.RawMessage {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, b.base+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if b.csrf != "" {
		req.Header.Set("X-CSRF-Token", b.csrf)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result struct {
		Success bool                       `json:"success"`
		Error   string                     `json:"error"`
		Data    map[string]json.RawMessage `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d want %d; %s", method, path, resp.StatusCode, want, result.Error)
	}
	return result.Data
}
func (b *browser) login(t *testing.T, name, pw string) {
	data := b.request(t, "POST", "/api/v1/auth/login", map[string]string{"username": name, "password": pw}, 200)
	_ = json.Unmarshal(data["csrf_token"], &b.csrf)
}
func create(t *testing.T, b *browser, name, role string) string {
	data := b.request(t, "POST", "/api/v1/users", map[string]string{"username": name, "password": password, "role": role}, 201)
	var id string
	_ = json.Unmarshal(data["id"], &id)
	return id
}

func TestIdentityLifecycle(t *testing.T) {
	db, service, server, owner := setup(t)
	// Restart/reconstruct the service: sessions are in PostgreSQL, not API memory.
	another := identity.New(db.Pool, false)
	if another == service {
		t.Fatal("expected separate instance")
	}
	data := owner.request(t, "GET", "/api/v1/session", nil, 200)
	var account struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	_ = json.Unmarshal(data["user"], &account)
	if account.Role != "super_admin" {
		t.Fatal("bootstrap role")
	}
	user := create(t, owner, "buyerone", "buyer")
	adminID := create(t, owner, "manager", "admin")
	buyer := newBrowser(server.URL)
	buyer.login(t, "buyerone", password)
	manager := newBrowser(server.URL)
	manager.login(t, "manager", password)
	buyer.request(t, "PATCH", "/api/v1/me", map[string]string{"role": "super_admin"}, 403)
	buyer.request(t, "POST", "/api/v1/users", map[string]string{"username": "illegal", "password": password, "role": "buyer"}, 403)
	buyer.request(t, "GET", "/api/v1/users", nil, 403)
	buyer.request(t, "GET", "/api/v1/offers", nil, 403)
	buyer.request(t, "GET", "/api/v1/products/"+uuid.NewString()+"/statistics", nil, 403)
	buyer.request(t, "GET", "/api/v1/watch-sources", nil, 403)
	buyer.request(t, "PATCH", "/api/v1/me/preferences", map[string]string{"language": "en", "preferred_city": "تهران", "number_format": "latin"}, 200)
	buyer.request(t, "PATCH", "/api/v1/me", map[string]string{"full_name": "Buyer One", "email": "one@example.test"}, 200)
	// A separately constructed service sees both persisted session and preferences.
	replica := httptest.NewServer(router(another))
	defer replica.Close()
	oldBase := buyer.base
	buyer.base = replica.URL
	persisted := buyer.request(t, "GET", "/api/v1/me/preferences", nil, 200)
	if string(persisted["language"]) != "\"en\"" {
		t.Fatal("preferences not persisted")
	}
	buyer.base = oldBase
	saved := buyer.csrf
	buyer.csrf = ""
	buyer.request(t, "PATCH", "/api/v1/me", map[string]string{"full_name": "forged"}, 403)
	buyer.csrf = saved
	manager.request(t, "PATCH", "/api/v1/users/"+user, map[string]string{"role": "super_admin"}, 403)
	manager.request(t, "PATCH", "/api/v1/users/"+account.ID, map[string]bool{"active": false}, 403)
	create(t, manager, "buyertwo", "buyer")
	owner.request(t, "PATCH", "/api/v1/users/"+account.ID, map[string]bool{"active": false}, 409)
	owner.request(t, "PATCH", "/api/v1/users/"+user, map[string]bool{"active": false}, 200)
	buyer.request(t, "GET", "/api/v1/session", nil, 401)
	owner.request(t, "PATCH", "/api/v1/users/"+user, map[string]bool{"active": true}, 200)
	buyer.login(t, "buyerone", password)
	manager.request(t, "POST", "/api/v1/users/"+user+"/reset-password", map[string]string{"password": "reset-test-password-456"}, 200)
	buyer.request(t, "GET", "/api/v1/session", nil, 401)
	buyer.request(t, "POST", "/api/v1/auth/login", map[string]string{"username": "buyerone", "password": password}, 401)
	buyer.login(t, "buyerone", "reset-test-password-456")
	buyer.request(t, "POST", "/api/v1/me/password", map[string]string{"current_password": "wrong", "password": "new-test-password-789"}, 400)
	buyer.request(t, "POST", "/api/v1/me/password", map[string]string{"current_password": "reset-test-password-456", "password": "new-test-password-789"}, 200)
	buyer.request(t, "GET", "/api/v1/session", nil, 401)
	buyer.login(t, "buyerone", "new-test-password-789")
	owner.request(t, "PATCH", "/api/v1/users/"+adminID, map[string]string{"role": "buyer"}, 200)
	manager.request(t, "GET", "/api/v1/session", nil, 401)
	buyer.request(t, "POST", "/api/v1/auth/logout", map[string]string{}, 200)
	buyer.request(t, "GET", "/api/v1/session", nil, 401)
	var count int
	_ = db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action IN('account_created','password_changed','account_updated')`).Scan(&count)
	if count < 6 {
		t.Fatal("missing identity audit trail")
	}
}

func TestIdentitySecurityBoundary(t *testing.T) {
	db, _, server, owner := setup(t)
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/session", nil)
	req.Header.Set("Authorization", "Bearer old-shared-token-with-more-than-32-characters")
	req.Header.Set("X-User-ID", uuid.NewString())
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("legacy token must not authenticate a user")
	}
	req, _ = http.NewRequest("POST", server.URL+"/api/v1/auth/login", strings.NewReader(`{"username":"owner","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://attacker.test")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-origin login")
	}
	for _, cookie := range owner.client.Jar.Cookies(mustURL(t, server.URL+"/api/v1/session")) {
		if strings.Contains(cookie.Value, password) {
			t.Fatal("password in cookie")
		}
	}
	// Validate production attributes using an HTTPS test server.
	secure := identity.New(db.Pool, true)
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(secure.Login))
	defer tlsServer.Close()
	req, _ = http.NewRequest("POST", tlsServer.URL, strings.NewReader(`{"username":"owner","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	response, err = tlsServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	cookies := response.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	var stored int
	_ = db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM user_sessions WHERE token_hash=$1`, cookies[0].Value).Scan(&stored)
	if stored != 0 {
		t.Fatal("raw session token persisted")
	}
	for i := 0; i < 21; i++ {
		want := 401
		if i == 20 {
			want = 429
		}
		newBrowser(server.URL).request(t, "POST", "/api/v1/auth/login", map[string]string{"username": "unknown", "password": password}, want)
	}
	// Expiry is enforced by the database on each request.
	if _, err = db.Pool.Exec(context.Background(), `UPDATE user_sessions SET expires_at=NOW()-INTERVAL '1 second'`); err != nil {
		t.Fatal(err)
	}
	owner.request(t, "GET", "/api/v1/session", nil, 401)
}

func TestLastSuperAdminConcurrentUpdates(t *testing.T) {
	db, _, server, owner := setup(t)
	secondID := create(t, owner, "secondowner", "super_admin")
	second := newBrowser(server.URL)
	second.login(t, "secondowner", password)
	var firstID string
	_ = db.Pool.QueryRow(context.Background(), `SELECT id::text FROM users WHERE username='owner'`).Scan(&firstID)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, item := range []struct {
		b  *browser
		id string
	}{{owner, secondID}, {second, firstID}} {
		wg.Add(1)
		go func(b *browser, id string) {
			defer wg.Done()
			req, _ := http.NewRequest("PATCH", server.URL+"/api/v1/users/"+id, strings.NewReader(`{"active":false}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", b.csrf)
			resp, err := b.client.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			defer resp.Body.Close()
			statuses <- resp.StatusCode
		}(item.b, item.id)
	}
	wg.Wait()
	close(statuses)
	successes := 0
	for status := range statuses {
		if status == 200 {
			successes++
		} else if status != 403 && status != 401 && status != 409 {
			t.Fatalf("unexpected concurrent result %d", status)
		}
	}
	var count int
	_ = db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE role='super_admin' AND active`).Scan(&count)
	if successes != 1 || count != 1 {
		t.Fatalf("lost last super admin: successes %d active %d", successes, count)
	}
}

func TestLegacyIdentityMigration(t *testing.T) {
	db := database(t, false)
	ctx := context.Background()
	_, err := db.Pool.Exec(ctx, `CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,checksum TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob("../storage/postgres/migrations/00[1-3]*.sql")
	if err != nil || len(paths) != 3 {
		t.Fatal("baseline migrations")
	}
	for _, path := range paths {
		body, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Pool.Exec(ctx, string(body)); e != nil {
			t.Fatal(e)
		}
		sum := fmt.Sprintf("%x", sha256.Sum256(body))
		if _, e = db.Pool.Exec(ctx, `INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)`, "migrations/"+filepath.Base(path), sum); e != nil {
			t.Fatal(e)
		}
	}
	owner := uuid.New()
	_, err = db.Pool.Exec(ctx, `INSERT INTO users(id,username,role) VALUES($1,'legacyowner','operator'),(gen_random_uuid(),'legacybuyer','user'),(gen_random_uuid(),'unknownrole','unexpected')`, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='buyer' AND NOT active AND password_hash=''`).Scan(&count)
	if count != 3 {
		t.Fatal("unsafe legacy role migration")
	}
	s := identity.New(db.Pool, false)
	if err = s.Bootstrap(ctx, owner, "legacyowner", password); err != nil {
		t.Fatal(err)
	}
	if err = s.Bootstrap(ctx, owner, "replacement", "different-bootstrap-pass"); err != nil {
		t.Fatal(err)
	}
	var username string
	_ = db.Pool.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, owner).Scan(&username)
	if username != "legacyowner" {
		t.Fatal("bootstrap overwrote configured account")
	}
}

// Keep a URL helper local to the test; no production cookie attributes are inferred from the jar.
func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
