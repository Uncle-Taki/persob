package identity_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/identity"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func sourceFixture(t *testing.T, db *postgres.DB, owner uuid.UUID, peer int64) *domain.TelegramSource {
	t.Helper()
	s := &domain.TelegramSource{OwnerID: owner, TelegramPeerID: peer, Title: "shared source", SourceType: domain.TelegramSourceTypeBroadcastChannel, DetectedType: domain.TelegramSourceTypeBroadcastChannel, AccessType: domain.AccessTypePublic, IsActive: true, IngestionStatus: domain.IngestionStatusActive, ExternalLLMAllowed: true}
	if err := postgres.NewTelegramSourceRepo(db).Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	return s
}
func accountID(t *testing.T, b *browser, name, role string) uuid.UUID {
	t.Helper()
	body := b.request(t, "POST", "/api/v1/users", map[string]string{"username": name, "role": role, "password": password}, 201)
	var u identity.Account
	if err := json.Unmarshal(mustJSON(body), &u); err != nil {
		t.Fatal(err)
	}
	return u.ID
}
func grants(t *testing.T, b *browser, id uuid.UUID) identity.SourceAssignments {
	t.Helper()
	body := b.request(t, "GET", "/api/v1/sources/"+id.String()+"/assignments", nil, 200)
	var g identity.SourceAssignments
	if err := json.Unmarshal(mustJSON(body), &g); err != nil {
		t.Fatal(err)
	}
	return g
}
func TestSourceAssignmentAdministrationAndConcurrentReplacement(t *testing.T) {
	db, _, server, owner := setup(t)
	ctx := context.Background()
	buyerA := accountID(t, owner, "assignedbuyer", "buyer")
	buyerB := accountID(t, owner, "secondbuyer", "buyer")
	inactive := accountID(t, owner, "inactivebuyer", "buyer")
	owner.request(t, "PATCH", "/api/v1/users/"+inactive.String(), map[string]bool{"active": false}, 200)
	managerID := accountID(t, owner, "sourceadmin", "admin")
	manager := newBrowser(server.URL)
	manager.login(t, "sourceadmin", password)
	source := sourceFixture(t, db, managerID, -100071)
	linked := int64(-100072)
	if _, err := db.Pool.Exec(ctx, `UPDATE telegram_sources SET linked_discussion_peer_id=$1 WHERE id=$2`, linked, source.ID); err != nil {
		t.Fatal(err)
	}
	discussion := sourceFixture(t, db, managerID, linked)
	path := "/api/v1/sources/" + source.ID.String() + "/assignments"
	initial := grants(t, owner, source.ID)
	if initial.WorkspaceID != domain.WorkspaceID || initial.Version != 0 || len(initial.BuyerIDs) != 0 || initial.LinkedPeerPolicy != "explicit" {
		t.Fatalf("unexpected initial grants: %+v", initial)
	}
	manager.request(t, "PUT", path, map[string]any{"buyer_ids": []uuid.UUID{buyerA, buyerB}, "version": 0}, 200)
	if len(grants(t, owner, source.ID).BuyerIDs) != 2 || len(grants(t, owner, discussion.ID).BuyerIDs) != 0 {
		t.Fatal("linked peer inherited grants or replacement failed")
	}
	invalid := []any{
		map[string]any{"buyer_ids": []uuid.UUID{buyerA, buyerA}, "version": 1},
		map[string]any{"buyer_ids": []uuid.UUID{buyerA, inactive}, "version": 1},
		map[string]any{"buyer_ids": []uuid.UUID{managerID}, "version": 1},
		map[string]any{"buyer_ids": []uuid.UUID{uuid.New()}, "version": 1},
		map[string]any{"buyer_ids": []uuid.UUID{buyerA}},
		map[string]any{"buyer_ids": nil, "version": 1},
		map[string]any{"buyer_ids": []uuid.UUID{}, "version": 1, "workspace_id": uuid.New()},
	}
	for _, body := range invalid {
		owner.request(t, "PUT", path, body, 400)
	}
	if g := grants(t, owner, source.ID); g.Version != 1 || len(g.BuyerIDs) != 2 {
		t.Fatal("invalid replacement changed grants")
	}
	buyer := newBrowser(server.URL)
	buyer.login(t, "assignedbuyer", password)
	buyer.request(t, "GET", path, nil, 403)
	buyer.request(t, "GET", "/api/v1/sources", nil, 200)
	buyer.request(t, "GET", "/api/v1/sources/"+source.ID.String(), nil, 200)
	buyer.request(t, "GET", "/api/v1/offers", nil, 200)
	buyer.request(t, "GET", "/api/v1/watch-sources", nil, 403)
	buyer.request(t, "PUT", path, map[string]any{"buyer_ids": []uuid.UUID{buyerA}, "version": 1}, 403)
	req, _ := http.NewRequest("PUT", server.URL+path, strings.NewReader(`{"buyer_ids":[],"version":1}`))
	req.Header.Set("Content-Type", "application/json")
	response, err := owner.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("grant write bypassed CSRF")
	}
	// Both forms started from the same version. Exactly one replacement commits.
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, id := range []uuid.UUID{buyerA, buyerB} {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{"buyer_ids": []uuid.UUID{id}, "version": 1})
			req, _ := http.NewRequest("PUT", server.URL+path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", owner.csrf)
			res, err := owner.client.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			res.Body.Close()
			statuses <- res.StatusCode
		}(id)
	}
	wg.Wait()
	close(statuses)
	wins, conflicts := 0, 0
	for status := range statuses {
		if status == 200 {
			wins++
		}
		if status == 409 {
			conflicts++
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent replacements: success=%d conflict=%d", wins, conflicts)
	}
	if g := grants(t, owner, source.ID); g.Version != 2 || len(g.BuyerIDs) != 1 {
		t.Fatal("non-atomic grant replacement")
	}
	var audits int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='source.assignments.replace' AND entity_id=$1`, source.ID.String()).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audit count=%d err=%v", audits, err)
	}
	owner.request(t, "PUT", path, map[string]any{"buyer_ids": []uuid.UUID{}, "version": 2}, 200)
	if len(grants(t, owner, source.ID).BuyerIDs) != 0 {
		t.Fatal("explicit empty replacement did not clear grants")
	}
	owner.request(t, "PUT", path, map[string]any{"buyer_ids": []uuid.UUID{buyerA}, "version": 3}, 200)
	// Removal clears access but keeps source/listing history and prevents stale forms.
	offer := &domain.Offer{SourceID: &source.ID, SourceMessageID: "-100071_5", SellerID: 1, SourceRevisionAt: time.Now()}
	if err = postgres.NewOfferRepo(db).Create(ctx, offer); err != nil {
		t.Fatal(err)
	}
	if err = postgres.NewTelegramSourceRepo(db).DeleteWorkspace(ctx, source.ID, managerID); err != nil {
		t.Fatal(err)
	}
	owner.request(t, "GET", path, nil, 404)
	owner.request(t, "PUT", path, map[string]any{"buyer_ids": []uuid.UUID{buyerA}, "version": 4}, 404)
	var n int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM source_assignments WHERE source_id=$1`, source.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("removal retained grants", err)
	}
	preserved, err := postgres.NewOfferRepo(db).GetByID(ctx, offer.ID)
	if err != nil || preserved.SourceID == nil || *preserved.SourceID != source.ID {
		t.Fatal("removal destroyed provenance", err)
	}
}

func TestWorkspaceSourceIdentityAndExactLinkedPeerPolicy(t *testing.T) {
	db, _, _, owner := setup(t)
	ctx := context.Background()
	adminA := accountID(t, owner, "firstadmin", "admin")
	adminB := accountID(t, owner, "otheradmin", "admin")
	repo := postgres.NewTelegramSourceRepo(db)
	source := sourceFixture(t, db, adminA, -100080)
	second := sourceFixture(t, db, adminB, -100080)
	if second.ID != source.ID || second.OwnerID != adminA {
		t.Fatal("administrator created a second ingestion identity or replaced provenance")
	}
	list, err := repo.ListWorkspace(ctx)
	if err != nil || len(list) != 1 {
		t.Fatal("workspace registry duplicated peer", err)
	}
	linked := int64(-100081)
	if _, err = db.Pool.Exec(ctx, `UPDATE telegram_sources SET linked_discussion_peer_id=$1 WHERE id=$2`, linked, source.ID); err != nil {
		t.Fatal(err)
	}
	id, err := repo.SourceForPeer(ctx, linked)
	if err != nil || id != nil {
		t.Fatal("linked peer inherited source identity", err)
	}
	allowed, err := repo.AllowWorkspaceExternalProcessing(ctx, -100080)
	if err != nil || !allowed {
		t.Fatal("creator still controls workspace processing", err)
	}
	allowed, err = repo.AllowWorkspaceExternalProcessing(ctx, linked)
	if err != nil || allowed {
		t.Fatal("parent permission transmitted discussion text", err)
	}
	discussion := sourceFixture(t, db, adminB, linked)
	if err = repo.UpdateStatus(ctx, discussion.ID, domain.IngestionStatusPaused, nil); err != nil {
		t.Fatal(err)
	}
	configured, active, err := repo.PeerMonitoringStatus(ctx, linked)
	if err != nil || !configured || active {
		t.Fatal("parent bypassed explicitly paused discussion", err)
	}
	if err = repo.DeleteWorkspace(ctx, discussion.ID, adminB); err != nil {
		t.Fatal(err)
	}
	configured, active, err = repo.PeerMonitoringStatus(ctx, linked)
	if err != nil || !configured || active {
		t.Fatal("parent/legacy fallback reactivated removed discussion", err)
	}
}

func priorSchema(t *testing.T) *postgres.DB {
	t.Helper()
	db := database(t, false)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,checksum TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob("../storage/postgres/migrations/00[1-4]*.sql")
	if err != nil || len(paths) != 4 {
		t.Fatal("prior migration files", err)
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Pool.Exec(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Pool.Exec(ctx, `INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)`, "migrations/"+filepath.Base(path), fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestSharedSourcePopulatedUpgradePreservesOwnershipAndHistory(t *testing.T) {
	db := priorSchema(t)
	ctx := context.Background()
	a, b, newBuyer := uuid.New(), uuid.New(), uuid.New()
	for i, id := range []uuid.UUID{a, b, newBuyer} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,$2)`, id, fmt.Sprintf("legacy%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var source, offer, conv uuid.UUID
	if err := db.Pool.QueryRow(ctx, `INSERT INTO telegram_sources(owner_id,telegram_peer_id,title,source_type,detected_type,linked_discussion_peer_id) VALUES($1,-123,'legacy','broadcast_channel','broadcast_channel',-124) RETURNING id`, a).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO watch_sources(user_id,telegram_chat_id,title) VALUES($1,-123,'documented second owner'),($1,-125,'watch only')`, b); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `INSERT INTO offers(owner_id,source_message_id,seller_id,price_amount) VALUES($1,'-123_1',9,100) RETURNING id`, a).Scan(&offer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO offers(source_message_id,seller_id) VALUES('-124_1',9),('unknown_2',9)`); err != nil {
		t.Fatal(err)
	}
	var product uuid.UUID
	if err := db.Pool.QueryRow(ctx, `INSERT INTO products(name,normalized_name,brand,model,category) VALUES('history','history','legacy','legacy','legacy') RETURNING id`).Scan(&product); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE offers SET product_id=$1 WHERE id=$2`, product, offer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO price_observations(product_id,offer_id,price_amount) VALUES($1,$2,100)`, product, offer); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `INSERT INTO conversations(owner_id,offer_id,seller_id) VALUES($1,$2,9) RETURNING id`, a, offer).Scan(&conv); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var mapped, owner uuid.UUID
	var price int64
	if err := db.Pool.QueryRow(ctx, `SELECT source_id,owner_id,price_amount FROM offers WHERE id=$1`, offer).Scan(&mapped, &owner, &price); err != nil || mapped != source || owner != a || price != 100 {
		t.Fatal("lost listing provenance/history", err)
	}
	var n int
	for _, check := range []struct {
		sql  string
		want int
	}{
		{`SELECT count(*) FROM source_assignments WHERE source_id='` + source.String() + `'`, 2},
		{`SELECT count(*) FROM source_assignments WHERE buyer_id='` + newBuyer.String() + `'`, 0},
		{`SELECT count(*) FROM offers WHERE source_id IS NULL AND owner_id IS NULL`, 2},
		{`SELECT count(*) FROM conversation_requests WHERE conversation_id='` + conv.String() + `' AND user_id='` + a.String() + `'`, 1},
		{`SELECT count(*) FROM telegram_sources WHERE telegram_peer_id=-125 AND NOT is_active AND ingestion_status='paused'`, 1},
		{`SELECT count(*) FROM watch_sources`, 2},
		{`SELECT count(*) FROM price_observations WHERE price_amount=100`, 1},
		{`SELECT count(*) FROM schema_migrations`, 6},
	} {
		if err := db.Pool.QueryRow(ctx, check.sql).Scan(&n); err != nil || n != check.want {
			t.Fatalf("upgrade check %s: n=%d want=%d err=%v", check.sql, n, check.want, err)
		}
	}
}

func TestAmbiguousLegacySourcesRollBackMigration(t *testing.T) {
	db := priorSchema(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		var u uuid.UUID
		if err := db.Pool.QueryRow(ctx, `INSERT INTO users(username) VALUES($1) RETURNING id`, fmt.Sprintf("owner%d", i)).Scan(&u); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO telegram_sources(owner_id,telegram_peer_id,title,source_type,detected_type) VALUES($1,-999,'duplicate','supergroup','supergroup')`, u); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "duplicate normalized Telegram peers") {
		t.Fatalf("ambiguous peers merged silently: %v", err)
	}
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM telegram_sources WHERE telegram_peer_id=-999`).Scan(&n); err != nil || n != 2 {
		t.Fatal("failed migration changed history", err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != 4 {
		t.Fatal("failed migration changed ledger", err)
	}
}

func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
