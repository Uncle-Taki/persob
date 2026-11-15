package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestCovWatchSourceCreateDefaultsAndConflict(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewWatchSourceRepo(db)
	covUser(t, db, covBuyerID, "buyer1")

	// Defaults: no id, source type, monitoring status; sell forced false.
	s := &domain.WatchSource{
		UserID: covBuyerID, TelegramChatID: -1001, Title: "Channel A",
		SellEnabled: true,
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.ID == uuid.Nil || s.CreatedAt.IsZero() {
		t.Fatalf("expected generated identity: %+v", s)
	}
	if s.SellEnabled || !s.BuyEnabled {
		t.Fatalf("invariants not enforced: %+v", s)
	}
	if s.SourceType != domain.SourceTypeChannel || s.MonitoringStatus != domain.MonitoringStatusActive {
		t.Fatalf("defaults not applied: %+v", s)
	}

	// Same (user, chat): conflict path must reactivate and update title.
	s2 := &domain.WatchSource{UserID: covBuyerID, TelegramChatID: -1001, Title: "Channel A renamed", MonitoringStatus: domain.MonitoringStatusPaused, SourceType: domain.SourceTypeSupergroup}
	if err := repo.Create(ctx, s2); err != nil {
		t.Fatalf("conflict create: %v", err)
	}

	got, err := repo.ListByUserID(ctx, covBuyerID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 source, got %d", len(got))
	}
	if got[0].Title != "Channel A renamed" || got[0].MonitoringStatus != domain.MonitoringStatusActive {
		t.Fatalf("conflict did not reactivate/update: %+v", got[0])
	}

	// FK violation for unknown user.
	if err := repo.Create(ctx, &domain.WatchSource{UserID: uuid.New(), TelegramChatID: -9, Title: "x"}); err == nil {
		t.Fatal("expected FK violation for unknown user")
	}
}

func TestCovWatchSourceListByUserIDUsernameNull(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewWatchSourceRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	if err := repo.Create(ctx, &domain.WatchSource{UserID: covBuyerID, TelegramChatID: -2001, Username: "handle", Title: "G"}); err != nil {
		t.Fatal(err)
	}
	// Second with NULL username.
	if _, err := db.Pool.Exec(ctx, `INSERT INTO watch_sources(id,user_id,telegram_chat_id,username,title) VALUES(gen_random_uuid(),$1,-2002,NULL,'NULL user')`, covBuyerID); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListByUserID(ctx, covBuyerID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	foundHandle, foundNull := false, false
	for _, s := range got {
		if s.Username == "handle" {
			foundHandle = true
		}
		if s.Username == "" {
			foundNull = true
		}
	}
	if !foundHandle || !foundNull {
		t.Fatalf("username null path not exercised: %v", got)
	}
}

func TestCovWatchSourceListByUserIDQueryError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE watch_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewWatchSourceRepo(db).ListByUserID(context.Background(), covBuyerID); err == nil {
		t.Fatal("expected list error")
	}
}

func TestCovWatchSourceDelete(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewWatchSourceRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	covUser(t, db, covOwnerID, "buyer2")
	if err := repo.Create(ctx, &domain.WatchSource{UserID: covBuyerID, TelegramChatID: -3001, Title: "D"}); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := db.Pool.QueryRow(ctx, "SELECT id FROM watch_sources WHERE telegram_chat_id=-3001").Scan(&id); err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(ctx, id, covOwnerID); err == nil || !strings.Contains(err.Error(), "not found or unauthorized") {
		t.Fatalf("expected unauthorized, got %v", err)
	}
	if err := repo.Delete(ctx, id, covBuyerID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM watch_sources").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("not deleted, %d remain", n)
	}
}

func TestCovWatchSourceListAllActiveChatIDsAndIsMonitored(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewWatchSourceRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	if err := repo.Create(ctx, &domain.WatchSource{UserID: covBuyerID, TelegramChatID: -4001, Title: "Active1"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, &domain.WatchSource{UserID: covBuyerID, TelegramChatID: -4002, Title: "Active2"}); err != nil {
		t.Fatal(err)
	}
	// Pause one.
	var id1 uuid.UUID
	if err := db.Pool.QueryRow(ctx, "SELECT id FROM watch_sources WHERE telegram_chat_id=-4001").Scan(&id1); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, id1, domain.MonitoringStatusPaused); err != nil {
		t.Fatalf("update status: %v", err)
	}

	chatIDs, err := repo.ListAllActiveChatIDs(ctx)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(chatIDs) != 1 || chatIDs[0] != -4002 {
		t.Fatalf("want only -4002, got %v", chatIDs)
	}

	if monitored, err := repo.IsChatMonitored(ctx, -4002); err != nil || !monitored {
		t.Fatalf("expected -4002 monitored: %v / %v", monitored, err)
	}
	if monitored, err := repo.IsChatMonitored(ctx, -4001); err != nil || monitored {
		t.Fatalf("paused chat must not be monitored: %v / %v", monitored, err)
	}
	if monitored, err := repo.IsChatMonitored(ctx, -9999); err != nil || monitored {
		t.Fatalf("unknown chat must not be monitored: %v / %v", monitored, err)
	}
}

func TestCovWatchSourceListAllActiveQueryError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE watch_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewWatchSourceRepo(db).ListAllActiveChatIDs(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := postgres.NewWatchSourceRepo(db).IsChatMonitored(context.Background(), -1); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovWatchSourceUpdateStatusNoRow(t *testing.T) {
	db := covDB(t, true)
	// Update on a non-existent id: no rows affected, returns nil error but we
	// assert it did not error (Exec has no not-found semantics here).
	if err := postgres.NewWatchSourceRepo(db).UpdateStatus(context.Background(), uuid.New(), domain.MonitoringStatusPaused); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
