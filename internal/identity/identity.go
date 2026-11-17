// Package identity owns password accounts and revocable browser sessions.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

const CookieName = "perso_session"
const sessionLife = 12 * time.Hour

type userKey struct{}
type sessionKey struct{}
type Service struct {
	Pool   *pgxpool.Pool
	Secure bool
	dummy  []byte
}
type Preferences struct {
	Language      string `json:"language"`
	PreferredCity string `json:"preferred_city"`
	PreferredArea string `json:"preferred_area"`
	DefaultSort   string `json:"default_sort"`
	NumberFormat  string `json:"number_format"`
}
type Account struct {
	domain.User
	Preferences  Preferences `json:"preferences"`
	PasswordHash string      `json:"-"`
}

func New(pool *pgxpool.Pool, secure bool) *Service {
	dummy, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-login-password"), bcrypt.DefaultCost)
	return &Service{Pool: pool, Secure: secure, dummy: dummy}
}
func Current(ctx context.Context) *Account { u, _ := ctx.Value(userKey{}).(*Account); return u }
func admin(role domain.UserRole) bool {
	return role == domain.RoleAdmin || role == domain.RoleSuperAdmin
}
func Permissions(u *Account) []string {
	p := []string{"account:self", "interests:self", "catalog:read"}
	if admin(u.Role) {
		p = append(p, "users:buyers", "marketplace:operator", "sources:manage", "outreach:approve")
	}
	if u.Role == domain.RoleSuperAdmin {
		p = append(p, "users:roles", "platform:configure")
	}
	return p
}
func validRole(r domain.UserRole) bool {
	return r == domain.RoleBuyer || r == domain.RoleAdmin || r == domain.RoleSuperAdmin
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{2,63}$`)

func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func newToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func csrf(raw string) string { return hash("perso-csrf:" + raw) }
func hashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 72 {
		return "", fmt.Errorf("Password must contain 12–72 UTF-8 bytes")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

const columns = `id,username,COALESCE(email,''),COALESCE(full_name,''),role,active,preferences,password_hash,created_at,updated_at`

func scan(row pgx.Row) (*Account, error) {
	var u Account
	var prefs []byte
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.FullName, &u.Role, &u.Active, &prefs, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(prefs, &u.Preferences); err != nil {
		return nil, err
	}
	return &u, nil
}
func (s *Service) get(ctx context.Context, id uuid.UUID) (*Account, error) {
	return scan(s.Pool.QueryRow(ctx, `SELECT `+columns+` FROM users WHERE id=$1`, id))
}
func respondError(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		httputil.RespondError(w, 409, "Username or email is already in use")
		return
	}
	httputil.RespondError(w, 503, "Account storage temporarily unavailable")
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		httputil.RespondError(w, 415, "Use application/json")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(&struct{}{}) != io.EOF {
		httputil.RespondError(w, 400, "Invalid request body")
		return false
	}
	return true
}
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return false
		}
	}
	return true
}
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		c, err := r.Cookie(CookieName)
		if err != nil || len(c.Value) != 64 {
			httputil.RespondError(w, 401, "Sign in required")
			return
		}
		var id uuid.UUID
		err = s.Pool.QueryRow(r.Context(), `SELECT user_id FROM user_sessions WHERE token_hash=$1 AND expires_at>NOW()`, hash(c.Value)).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			httputil.RespondError(w, 401, "Session expired or revoked")
			return
		}
		if err != nil {
			respondError(w, err)
			return
		}
		u, err := s.get(r.Context(), id)
		if err != nil {
			respondError(w, err)
			return
		}
		if !u.Active || !validRole(u.Role) {
			httputil.RespondError(w, 401, "Account inactive")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf(c.Value))) != 1 {
				httputil.RespondError(w, 403, "Invalid CSRF token or origin")
				return
			}
		}
		ctx := context.WithValue(r.Context(), userKey{}, u)
		ctx = context.WithValue(ctx, sessionKey{}, c.Value)
		ctx = context.WithValue(ctx, httputil.UserContextKey, u.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Buyer marketplace reads are admitted here and scoped by the owning
// repository using source_assignments. This middleware does not grant data
// access by itself.
func (s *Service) Marketplace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := Current(r.Context())
		if u == nil {
			httputil.RespondError(w, 401, "Sign in required")
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		adminOnly := strings.HasPrefix(path, "/api/v1/watch-sources") || strings.HasPrefix(path, "/api/v1/telegram")
		if adminOnly && !admin(u.Role) {
			httputil.RespondError(w, 403, "Administrator access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !sameOrigin(r) {
		httputil.RespondError(w, 403, "Cross-origin login rejected")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if !usernamePattern.MatchString(req.Username) || len(req.Password) > 72 {
		httputil.RespondError(w, 401, "Invalid username or password")
		return
	}
	// Use the actual socket peer; do not trust spoofable X-Forwarded-For headers.
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	for _, bucket := range []struct {
		key   string
		limit int
	}{{"user:" + req.Username, 20}, {"ip:" + ip, 50}} {
		var n int
		err := s.Pool.QueryRow(r.Context(), `INSERT INTO auth_rate_limits(bucket,window_start,attempts) VALUES($1,NOW(),1) ON CONFLICT(bucket) DO UPDATE SET attempts=CASE WHEN auth_rate_limits.window_start<NOW()-INTERVAL '15 minutes' THEN 1 ELSE auth_rate_limits.attempts+1 END,window_start=CASE WHEN auth_rate_limits.window_start<NOW()-INTERVAL '15 minutes' THEN NOW() ELSE auth_rate_limits.window_start END RETURNING attempts`, hash(bucket.key)).Scan(&n)
		if err != nil {
			respondError(w, err)
			return
		}
		if n > bucket.limit {
			w.Header().Set("Retry-After", "900")
			httputil.RespondError(w, 429, "Too many login attempts; retry later")
			return
		}
	}
	u, err := scan(s.Pool.QueryRow(r.Context(), `SELECT `+columns+` FROM users WHERE lower(username)=$1`, req.Username))
	candidate := s.dummy
	if err == nil && u.PasswordHash != "" {
		candidate = []byte(u.PasswordHash)
	}
	matched := bcrypt.CompareHashAndPassword(candidate, []byte(req.Password)) == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		respondError(w, err)
		return
	}
	if err != nil || !matched || !u.Active || !validRole(u.Role) {
		httputil.RespondError(w, 401, "Invalid username or password")
		return
	}
	raw, err := newToken()
	if err != nil {
		respondError(w, err)
		return
	}
	// Lock identity while issuing a session: a concurrent reset/deactivation cannot
	// leave behind a valid new session authenticated using the old password.
	tx, err := s.Pool.Begin(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var currentHash string
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT password_hash,active FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&currentHash, &active)
	if err != nil {
		respondError(w, err)
		return
	}
	if !active || currentHash != u.PasswordHash {
		httputil.RespondError(w, 401, "Credentials changed; sign in again")
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM user_sessions WHERE expires_at<NOW()`); err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO user_sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, hash(raw), u.ID, time.Now().Add(sessionLife))
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_rate_limits WHERE window_start<NOW()-INTERVAL '1 day'`)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if old, e := r.Cookie(CookieName); e == nil {
		_, _ = s.Pool.Exec(r.Context(), `DELETE FROM user_sessions WHERE token_hash=$1`, hash(old.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: raw, Path: "/api", HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionLife.Seconds()), Expires: time.Now().Add(sessionLife)})
	httputil.RespondJSON(w, 200, map[string]any{"user": u, "csrf_token": csrf(raw), "permissions": Permissions(u)})
}
func (s *Service) Session(w http.ResponseWriter, r *http.Request, outreach bool) {
	u := Current(r.Context())
	raw, _ := r.Context().Value(sessionKey{}).(string)
	httputil.RespondJSON(w, 200, map[string]any{"user_id": u.ID, "user": u, "csrf_token": csrf(raw), "permissions": Permissions(u), "catalog_domain": "healthcare", "workspace_mode": "multi_user_identity", "marketplace_access": true, "demo": false, "outreach_enabled": outreach && admin(u.Role)})
}
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	raw, _ := r.Context().Value(sessionKey{}).(string)
	if _, err := s.Pool.Exec(r.Context(), `DELETE FROM user_sessions WHERE token_hash=$1`, hash(raw)); err != nil {
		respondError(w, err)
		return
	}
	s.clearCookie(w)
	httputil.RespondJSON(w, 200, map[string]bool{"logged_out": true})
}
func (s *Service) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/api", HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}
func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	httputil.RespondJSON(w, 200, Current(r.Context()))
}
func validProfile(username, email, name string) bool {
	if !usernamePattern.MatchString(username) || len(name) > 200 || len(email) > 254 {
		return false
	}
	if email != "" {
		a, err := mail.ParseAddress(email)
		if err != nil || a.Address != email {
			return false
		}
	}
	return true
}

type Update struct {
	Username *string          `json:"username"`
	Email    *string          `json:"email"`
	FullName *string          `json:"full_name"`
	Role     *domain.UserRole `json:"role"`
	Active   *bool            `json:"active"`
}

func (s *Service) Profile(w http.ResponseWriter, r *http.Request)    { s.update(w, r, true) }
func (s *Service) UpdateUser(w http.ResponseWriter, r *http.Request) { s.update(w, r, false) }

// Account administration is serialized. The last-super-admin invariant and
// the acting administrator's permission are checked inside the same transaction.
func (s *Service) lock(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(344760767312)`); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
func audit(ctx context.Context, tx pgx.Tx, actor uuid.UUID, action string, target uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs(actor_id,action,entity_type,entity_id,payload) VALUES($1,$2,'user',$3,'{}')`, actor.String(), action, target.String())
	return err
}
func actorInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Account, error) {
	return scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM users WHERE id=$1 FOR UPDATE`, id))
}
func mayManage(a, t *Account) bool {
	return a.Active && (a.Role == domain.RoleSuperAdmin || (a.Role == domain.RoleAdmin && t.Role == domain.RoleBuyer))
}
func (s *Service) update(w http.ResponseWriter, r *http.Request, self bool) {
	var req Update
	if !decode(w, r, &req) {
		return
	}
	actor := Current(r.Context())
	id := actor.ID
	if !self {
		var err error
		id, err = uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			httputil.RespondError(w, 400, "Invalid user ID")
			return
		}
	}
	if self && (req.Role != nil || req.Active != nil) {
		httputil.RespondError(w, 403, "Profile updates cannot change role or account status")
		return
	}
	tx, err := s.lock(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	a, err := actorInTx(r.Context(), tx, actor.ID)
	if err != nil {
		respondError(w, err)
		return
	}
	u, err := actorInTx(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httputil.RespondError(w, 404, "User not found")
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if !a.Active || (!self && !mayManage(a, u)) {
		httputil.RespondError(w, 403, "Not permitted to manage this account")
		return
	}
	oldRole, oldActive := u.Role, u.Active
	if req.Username != nil {
		u.Username = strings.ToLower(strings.TrimSpace(*req.Username))
	}
	if req.Email != nil {
		u.Email = strings.TrimSpace(*req.Email)
	}
	if req.FullName != nil {
		u.FullName = strings.TrimSpace(*req.FullName)
	}
	if req.Role != nil {
		if a.Role != domain.RoleSuperAdmin && *req.Role != domain.RoleBuyer {
			httputil.RespondError(w, 403, "Only super_admin can assign elevated roles")
			return
		}
		u.Role = *req.Role
	}
	if req.Active != nil {
		u.Active = *req.Active
	}
	if !validRole(u.Role) || !validProfile(u.Username, u.Email, u.FullName) {
		httputil.RespondError(w, 400, "Invalid account fields")
		return
	}
	if oldRole == domain.RoleSuperAdmin && oldActive && (!u.Active || u.Role != domain.RoleSuperAdmin) {
		var count int
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE role='super_admin' AND active`).Scan(&count)
		if err != nil {
			respondError(w, err)
			return
		}
		if count <= 1 {
			httputil.RespondError(w, 409, "Keep at least one active super_admin")
			return
		}
	}
	if u.Active && u.PasswordHash == "" {
		httputil.RespondError(w, 409, "Set a password before activating this legacy account")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET username=$2,email=NULLIF($3,''),full_name=$4,role=$5,active=$6,updated_at=NOW() WHERE id=$1`, id, u.Username, u.Email, u.FullName, u.Role, u.Active)
	revoked := oldRole != u.Role || oldActive != u.Active
	if err == nil && revoked {
		_, err = tx.Exec(r.Context(), `DELETE FROM user_sessions WHERE user_id=$1`, id)
	}
	if err == nil {
		err = audit(r.Context(), tx, a.ID, "account_updated", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if revoked && id == a.ID {
		s.clearCookie(w)
	}
	u, err = s.get(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	httputil.RespondJSON(w, 200, u)
}
func (s *Service) Preferences(w http.ResponseWriter, r *http.Request) {
	u := Current(r.Context())
	if r.Method == "GET" {
		httputil.RespondJSON(w, 200, u.Preferences)
		return
	}
	// PATCH is partial; decode into the current values and reject unknown keys.
	prefs := u.Preferences
	if !decode(w, r, &prefs) {
		return
	}
	if (prefs.Language != "fa" && prefs.Language != "en") || (prefs.NumberFormat != "locale" && prefs.NumberFormat != "latin") || len(prefs.PreferredCity) > 100 || len(prefs.PreferredArea) > 100 {
		httputil.RespondError(w, 400, "Invalid preferences")
		return
	}
	switch prefs.DefaultSort {
	case "best_offer", "lowest_price", "highest_price", "newest":
	default:
		httputil.RespondError(w, 400, "Invalid default sort")
		return
	}
	b, _ := json.Marshal(prefs)
	if _, err := s.Pool.Exec(r.Context(), `UPDATE users SET preferences=$2,updated_at=NOW() WHERE id=$1 AND active`, u.ID, b); err != nil {
		respondError(w, err)
		return
	}
	httputil.RespondJSON(w, 200, prefs)
}
func (s *Service) ListUsers(w http.ResponseWriter, r *http.Request) {
	a := Current(r.Context())
	if !admin(a.Role) {
		httputil.RespondError(w, 403, "Administrative access required")
		return
	}
	query := `SELECT ` + columns + ` FROM users`
	if a.Role == domain.RoleAdmin {
		query += ` WHERE role='buyer'`
	}
	query += ` ORDER BY username LIMIT 200`
	rows, err := s.Pool.Query(r.Context(), query)
	if err != nil {
		respondError(w, err)
		return
	}
	defer rows.Close()
	users := []*Account{}
	for rows.Next() {
		u, e := scan(rows)
		if e != nil {
			respondError(w, e)
			return
		}
		users = append(users, u)
	}
	if err = rows.Err(); err != nil {
		respondError(w, err)
		return
	}
	httputil.RespondJSON(w, 200, users)
}
func (s *Service) GetUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.RespondError(w, 400, "Invalid user ID")
		return
	}
	u, err := s.get(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		httputil.RespondError(w, 404, "User not found")
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if !mayManage(Current(r.Context()), u) {
		httputil.RespondError(w, 403, "Not permitted to view this account")
		return
	}
	httputil.RespondJSON(w, 200, u)
}
func (s *Service) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string          `json:"username"`
		Email    string          `json:"email"`
		FullName string          `json:"full_name"`
		Password string          `json:"password"`
		Role     domain.UserRole `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if req.Role == "" {
		req.Role = domain.RoleBuyer
	}
	if !validProfile(req.Username, req.Email, req.FullName) || !validRole(req.Role) {
		httputil.RespondError(w, 400, "Invalid account fields")
		return
	}
	a := Current(r.Context())
	if !admin(a.Role) || (a.Role == domain.RoleAdmin && req.Role != domain.RoleBuyer) {
		httputil.RespondError(w, 403, "Not permitted to assign this role")
		return
	}
	password, err := hashPassword(req.Password)
	if err != nil {
		httputil.RespondError(w, 400, err.Error())
		return
	}
	tx, err := s.lock(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	a, err = actorInTx(r.Context(), tx, a.ID)
	if err != nil {
		respondError(w, err)
		return
	}
	if !a.Active || !admin(a.Role) || (a.Role == domain.RoleAdmin && req.Role != domain.RoleBuyer) {
		httputil.RespondError(w, 403, "Permission changed")
		return
	}
	id := uuid.New()
	_, err = tx.Exec(r.Context(), `INSERT INTO users(id,username,email,full_name,role,active,password_hash) VALUES($1,$2,NULLIF($3,''),$4,$5,true,$6)`, id, req.Username, req.Email, req.FullName, req.Role, password)
	if err == nil {
		err = audit(r.Context(), tx, a.ID, "account_created", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		respondError(w, err)
		return
	}
	u, err := s.get(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	httputil.RespondJSON(w, 201, u)
}
func (s *Service) Password(w http.ResponseWriter, r *http.Request)      { s.password(w, r, true) }
func (s *Service) ResetPassword(w http.ResponseWriter, r *http.Request) { s.password(w, r, false) }
func (s *Service) password(w http.ResponseWriter, r *http.Request, self bool) {
	var req struct {
		Current  string `json:"current_password"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	actor := Current(r.Context())
	id := actor.ID
	if !self {
		var err error
		id, err = uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			httputil.RespondError(w, 400, "Invalid user ID")
			return
		}
	}
	password, err := hashPassword(req.Password)
	if err != nil {
		httputil.RespondError(w, 400, err.Error())
		return
	}
	tx, err := s.lock(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	a, err := actorInTx(r.Context(), tx, actor.ID)
	if err != nil {
		respondError(w, err)
		return
	}
	u, err := actorInTx(r.Context(), tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httputil.RespondError(w, 404, "User not found")
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if !a.Active || (!self && !mayManage(a, u)) {
		httputil.RespondError(w, 403, "Not permitted to reset this account")
		return
	}
	if self && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Current)) != nil {
		httputil.RespondError(w, 400, "Current password is incorrect")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,updated_at=NOW() WHERE id=$1`, id, password)
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM user_sessions WHERE user_id=$1`, id)
	}
	if err == nil {
		err = audit(r.Context(), tx, a.ID, "password_changed", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if actor.ID == id {
		s.clearCookie(w)
	}
	httputil.RespondJSON(w, 200, map[string]bool{"sessions_revoked": true})
}

// Bootstrap is explicit, first-install-only, and never overwrites an existing
// password or re-promotes an account after the initial super admin exists.
func (s *Service) Bootstrap(ctx context.Context, id uuid.UUID, username, password string) error {
	if username == "" && password == "" {
		return nil
	}
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("invalid bootstrap username")
	}
	hashed, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("invalid bootstrap password length")
	}
	tx, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='super_admin' AND active`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO users(id,username,role,active,password_hash) VALUES($1,$2,'super_admin',true,$3) ON CONFLICT(id) DO UPDATE SET username=EXCLUDED.username,role='super_admin',active=true,password_hash=EXCLUDED.password_hash,updated_at=NOW() WHERE users.password_hash=''`, id, username, hashed)
	if err != nil {
		return err
	}
	var role string
	var active bool
	if err = tx.QueryRow(ctx, `SELECT role,active FROM users WHERE id=$1`, id).Scan(&role, &active); err != nil {
		return err
	}
	if role != "super_admin" || !active {
		return fmt.Errorf("bootstrap refuses an already configured identity")
	}
	if err = audit(ctx, tx, id, "installation_bootstrapped", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
