package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func covTelegramSource(t *testing.T, db *postgres.DB, id, owner uuid.UUID, peer int64, title string) *domain.TelegramSource {
	t.Helper()
	covUser(t, db, owner, "owner")
	s := &domain.TelegramSource{
		ID: id, OwnerID: owner, TelegramPeerID: peer, TelegramAccessHash: 123,
		Username: "handle", Title: title,
		SourceType: domain.TelegramSourceTypeSupergroup, AccessType: domain.AccessTypePublic,
		ExpectedType: domain.TelegramSourceTypeSupergroup, DetectedType: domain.TelegramSourceTypeSupergroup,
		Capabilities: domain.SourceCapabilities{CanReadHistory: true, HasTopics: true},
		IsActive:     true, IngestionStatus: domain.IngestionStatusActive,
		LastMessageID: 42,
	}
	if err := postgres.NewTelegramSourceRepo(db).Create(context.Background(), s); err != nil {
		t.Fatalf("create telegram source: %v", err)
	}
	return s
}

func TestCovTelegramSourceCreateDefaultsAndConflict(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)
	covUser(t, db, covBuyerID, "buyer1")

	// Defaults: no id, no expected type.
	s := &domain.TelegramSource{
		OwnerID: covBuyerID, TelegramPeerID: -1001, Title: "Source A",
		SourceType: "channel", AccessType: domain.AccessTypePublic,
		DetectedType: "channel",
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.ID == uuid.Nil || s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() {
		t.Fatalf("identity/timestamps missing: %+v", s)
	}

	// Conflict on (owner, peer): updates fields, returns same id.
	linked := int64(-1009)
	s2 := &domain.TelegramSource{
		OwnerID: covBuyerID, TelegramPeerID: -1001, Title: "Source A v2",
		Username:   "renamed",
		SourceType: "channel", AccessType: domain.AccessTypePrivate,
		DetectedType: domain.TelegramSourceTypeSupergroup, LinkedDiscussionPeerID: &linked,
		ExternalLLMAllowed: true,
		Capabilities:       domain.SourceCapabilities{HasTopics: true, SenderMetadataAvailable: true},
	}
	if err := repo.Create(ctx, s2); err != nil {
		t.Fatalf("conflict create: %v", err)
	}
	if s2.ID != s.ID {
		t.Fatalf("conflict should reuse id: %v vs %v", s2.ID, s.ID)
	}
	got, err := repo.GetByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Source A v2" || got.Username != "renamed" || !got.ExternalLLMAllowed || got.AccessType != domain.AccessTypePrivate {
		t.Fatalf("conflict update mismatch: %+v", got)
	}
	if got.LinkedDiscussionPeerID == nil || *got.LinkedDiscussionPeerID != -1009 {
		t.Fatalf("linked peer mismatch: %+v", got)
	}
	if !got.Capabilities.HasTopics || !got.Capabilities.SenderMetadataAvailable || got.Capabilities.CanReadHistory {
		t.Fatalf("capabilities should be overwritten by conflict: %+v", got.Capabilities)
	}

	// FK violation: unknown owner.
	if err := repo.Create(ctx, &domain.TelegramSource{OwnerID: uuid.New(), TelegramPeerID: -7, Title: "x", SourceType: "channel", DetectedType: "channel"}); err == nil {
		t.Fatal("expected FK violation for unknown owner")
	}
}

func TestCovTelegramSourceGetByID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	covUser(t, db, covBuyerID, "buyer1")
	s := covTelegramSource(t, db, uuid.New(), covBuyerID, -2001, "Fetch me")

	got, err := postgres.NewTelegramSourceRepo(db).GetByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != s.ID || got.TelegramPeerID != -2001 || got.Title != "Fetch me" || got.Username != "handle" {
		t.Fatalf("fields mismatch: %+v", got)
	}
	if got.ExpectedType != domain.TelegramSourceTypeSupergroup {
		t.Fatalf("expected type mismatch: %+v", got.ExpectedType)
	}
	if !got.Capabilities.CanReadHistory {
		t.Fatalf("capabilities mismatch: %+v", got.Capabilities)
	}

	// No expected type stored: NULL branch.
	plain := &domain.TelegramSource{OwnerID: covBuyerID, TelegramPeerID: -2002, Title: "Plain", SourceType: "channel", DetectedType: "channel"}
	if err := postgres.NewTelegramSourceRepo(db).Create(ctx, plain); err != nil {
		t.Fatal(err)
	}
	plainGot, err := postgres.NewTelegramSourceRepo(db).GetByID(ctx, plain.ID)
	if err != nil {
		t.Fatalf("get plain: %v", err)
	}
	if plainGot.ExpectedType != "" {
		t.Fatalf("expected empty expected type, got %q", plainGot.ExpectedType)
	}

	_, err = postgres.NewTelegramSourceRepo(db).GetByID(ctx, uuid.New())
	if err == nil || err.Error() != "source not found" {
		t.Fatalf("expected source not found, got %v", err)
	}
}

func TestCovTelegramSourceGetByIDError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewTelegramSourceRepo(db).GetByID(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourceGetByPeerID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	covUser(t, db, covBuyerID, "buyer1")
	s := covTelegramSource(t, db, uuid.New(), covBuyerID, -3001, "Peer fetch")

	got, err := postgres.NewTelegramSourceRepo(db).GetByPeerID(ctx, covBuyerID, -3001)
	if err != nil {
		t.Fatalf("get by peer: %v", err)
	}
	if got.ID != s.ID || got.Title != "Peer fetch" {
		t.Fatalf("fields mismatch: %+v", got)
	}
	if got.OwnerID != covBuyerID || got.TelegramAccessHash != 123 {
		t.Fatalf("owner/hash mismatch: %+v", got)
	}

	_, err = postgres.NewTelegramSourceRepo(db).GetByPeerID(ctx, covBuyerID, -3002)
	if err == nil || err.Error() != "source not found" {
		t.Fatalf("expected not found, got %v", err)
	}
	// Wrong owner.
	_, err = postgres.NewTelegramSourceRepo(db).GetByPeerID(ctx, covOwnerID, -3001)
	if err == nil || err.Error() != "source not found" {
		t.Fatalf("expected not found for wrong owner, got %v", err)
	}
}

func TestCovTelegramSourceGetByPeerError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewTelegramSourceRepo(db).GetByPeerID(context.Background(), covBuyerID, 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourceListByOwner(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	covTelegramSource(t, db, uuid.New(), covBuyerID, -4001, "A")
	covTelegramSource(t, db, uuid.New(), covBuyerID, -4002, "B")

	got, err := repo.ListByOwner(ctx, covBuyerID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	for _, s := range got {
		if s.OwnerID != covBuyerID {
			t.Fatalf("owner mismatch: %+v", s)
		}
	}

	empty, err := repo.ListByOwner(ctx, uuid.New())
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected none: %v / %v", empty, err)
	}
}

func TestCovTelegramSourceListByOwnerError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewTelegramSourceRepo(db).ListByOwner(context.Background(), covBuyerID); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourceListActiveSources(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)
	covUser(t, db, covBuyerID, "buyer1")
	active := covTelegramSource(t, db, uuid.New(), covBuyerID, -5001, "Active")
	paused := &domain.TelegramSource{OwnerID: covBuyerID, TelegramPeerID: -5002, Title: "Paused", SourceType: "channel", DetectedType: "channel", IsActive: true, IngestionStatus: domain.IngestionStatusPaused}
	if err := repo.Create(ctx, paused); err != nil {
		t.Fatal(err)
	}
	// Inactive source: excluded.
	inactive := &domain.TelegramSource{OwnerID: covBuyerID, TelegramPeerID: -5003, Title: "Inactive", SourceType: "channel", DetectedType: "channel", IsActive: false, IngestionStatus: domain.IngestionStatusActive}
	if err := repo.Create(ctx, inactive); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListActiveSources(ctx)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(got) != 1 || got[0].ID != active.ID {
		t.Fatalf("expected only the active source, got %+v", got)
	}
}

func TestCovTelegramSourceListActiveError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewTelegramSourceRepo(db).ListActiveSources(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourceUpdateStatus(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)
	s := covTelegramSource(t, db, uuid.New(), covBuyerID, -6001, "Status")

	// Pause, keep is_active via NULL.
	if err := repo.UpdateStatus(ctx, s.ID, domain.IngestionStatusPaused, nil); err != nil {
		t.Fatalf("update status: %v", err)
	}
	got, err := repo.GetByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IngestionStatus != domain.IngestionStatusPaused || !got.IsActive {
		t.Fatalf("status mismatch: %+v", got)
	}
	// Deactivate with explicit bool.
	f := false
	if err := repo.UpdateStatus(ctx, s.ID, domain.IngestionStatusError, &f); err != nil {
		t.Fatalf("update status 2: %v", err)
	}
	got, err = repo.GetByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IngestionStatus != domain.IngestionStatusError || got.IsActive {
		t.Fatalf("deactivate mismatch: %+v", got)
	}

	// Unknown id.
	err = repo.UpdateStatus(ctx, uuid.New(), domain.IngestionStatusActive, nil)
	if err == nil || err.Error() != "source not found" {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestCovTelegramSourceUpdateStatusError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewTelegramSourceRepo(db).UpdateStatus(context.Background(), uuid.New(), domain.IngestionStatusActive, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourceUpdateSyncCheckpoint(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)
	s := covTelegramSource(t, db, uuid.New(), covBuyerID, -7001, "Sync") // last_message_id=42

	synced := covNow
	if err := repo.UpdateSyncCheckpoint(ctx, s.ID, 40, synced); err != nil {
		t.Fatalf("checkpoint lower: %v", err)
	}
	got, err := repo.GetByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastMessageID != 42 || got.LastSyncedAt == nil || !got.LastSyncedAt.Equal(synced) {
		t.Fatalf("lower checkpoint should not regress: %+v", got)
	}
	if err := repo.UpdateSyncCheckpoint(ctx, s.ID, 100, covNow.Add(time.Hour)); err != nil {
		t.Fatalf("checkpoint higher: %v", err)
	}
	got, err = repo.GetByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastMessageID != 100 {
		t.Fatalf("checkpoint not advanced: %+v", got)
	}
}

func TestCovTelegramSourceUpdateSyncCheckpointError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewTelegramSourceRepo(db).UpdateSyncCheckpoint(context.Background(), uuid.New(), 1, covNow); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourceDelete(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)
	s := covTelegramSource(t, db, uuid.New(), covBuyerID, -8001, "Delete me")

	if err := repo.Delete(ctx, s.ID, covOwnerID); err == nil || err.Error() != "source not found" {
		t.Fatalf("expected unauthorized delete, got %v", err)
	}
	if err := repo.Delete(ctx, s.ID, covBuyerID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, s.ID); err == nil || err.Error() != "source not found" {
		t.Fatalf("row must be gone, got %v", err)
	}
}

func TestCovTelegramSourceDeleteError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewTelegramSourceRepo(db).Delete(context.Background(), uuid.New(), covBuyerID); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourcePeerMonitoringStatus(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)
	covUser(t, db, covBuyerID, "buyer1")

	// Unknown peer.
	configured, active, err := repo.PeerMonitoringStatus(ctx, -9001)
	if err != nil {
		t.Fatal(err)
	}
	if configured || active {
		t.Fatalf("unknown peer must be unconfigured: %v / %v", configured, active)
	}

	// Configured but paused: active=false.
	covTelegramSource(t, db, uuid.New(), covBuyerID, -9002, "Paused peer")
	if _, err := db.Pool.Exec(ctx, `UPDATE telegram_sources SET ingestion_status='paused' WHERE telegram_peer_id=-9002`); err != nil {
		t.Fatal(err)
	}
	configured, active, err = repo.PeerMonitoringStatus(ctx, -9002)
	if err != nil {
		t.Fatal(err)
	}
	if !configured || active {
		t.Fatalf("paused peer: configured=%v active=%v", configured, active)
	}

	// Configured and active.
	if _, err := db.Pool.Exec(ctx, `UPDATE telegram_sources SET ingestion_status='active' WHERE telegram_peer_id=-9002`); err != nil {
		t.Fatal(err)
	}
	configured, active, err = repo.PeerMonitoringStatus(ctx, -9002)
	if err != nil {
		t.Fatal(err)
	}
	if !configured || !active {
		t.Fatalf("active peer: configured=%v active=%v", configured, active)
	}

	// Match via linked_discussion_peer_id.
	linked := int64(-9003)
	linkedSrc := &domain.TelegramSource{OwnerID: covBuyerID, TelegramPeerID: -9004, Title: "Channel with discussion", SourceType: "channel", DetectedType: "channel", LinkedDiscussionPeerID: &linked, IsActive: true, IngestionStatus: domain.IngestionStatusActive}
	if err := repo.Create(ctx, linkedSrc); err != nil {
		t.Fatal(err)
	}
	configured, active, err = repo.PeerMonitoringStatus(ctx, -9003)
	if err != nil {
		t.Fatal(err)
	}
	if !configured || !active {
		t.Fatalf("linked discussion peer: configured=%v active=%v", configured, active)
	}
}

func TestCovTelegramSourcePeerMonitoringStatusError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := postgres.NewTelegramSourceRepo(db).PeerMonitoringStatus(context.Background(), 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourceHistoryCheckpoint(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)
	s := covTelegramSource(t, db, uuid.New(), covBuyerID, -10001, "History")

	n, err := repo.HistoryCheckpoint(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 checkpoint, got %d", n)
	}

	// Advance.
	if err := repo.CompleteHistoryScan(ctx, s.ID, 50); err != nil {
		t.Fatalf("advance: %v", err)
	}
	n, err = repo.HistoryCheckpoint(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 50 {
		t.Fatalf("expected 50, got %d", n)
	}
	got, err := repo.GetByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastMessageID != 50 {
		t.Fatalf("last_message_id not advanced: %+v", got)
	}
	// Lower value must not regress.
	if err := repo.CompleteHistoryScan(ctx, s.ID, 40); err != nil {
		t.Fatalf("regress attempt: %v", err)
	}
	n, err = repo.HistoryCheckpoint(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 50 {
		t.Fatalf("checkpoint regressed: %d", n)
	}
}

func TestCovTelegramSourceHistoryCheckpointMissingRow(t *testing.T) {
	db := covDB(t, true)
	_, err := postgres.NewTelegramSourceRepo(db).HistoryCheckpoint(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected no-rows error for missing source")
	}
}

func TestCovTelegramSourceCompleteHistoryScanError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if err := postgres.NewTelegramSourceRepo(db).CompleteHistoryScan(context.Background(), uuid.New(), 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourceAllowExternalProcessing(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)
	covUser(t, db, covBuyerID, "buyer1")

	// Invalid uuid: false without error.
	allowed, err := repo.AllowExternalProcessing(ctx, "not-a-uuid", covBuyerID)
	if err != nil || allowed {
		t.Fatalf("invalid id: %v / %v", allowed, err)
	}

	// Unknown id: false, nil.
	allowed, err = repo.AllowExternalProcessing(ctx, uuid.New().String(), covBuyerID)
	if err != nil || allowed {
		t.Fatalf("unknown id: %v / %v", allowed, err)
	}

	// Active + allowed source: true.
	s := covTelegramSource(t, db, uuid.New(), covBuyerID, -11001, "LLM")
	if _, err := db.Pool.Exec(ctx, `UPDATE telegram_sources SET external_llm_allowed=true WHERE id=$1`, s.ID); err != nil {
		t.Fatal(err)
	}
	allowed, err = repo.AllowExternalProcessing(ctx, s.ID.String(), covBuyerID)
	if err != nil || !allowed {
		t.Fatalf("expected allowed, got %v / %v", allowed, err)
	}

	// Wrong owner: false, nil.
	allowed, err = repo.AllowExternalProcessing(ctx, s.ID.String(), covOwnerID)
	if err != nil || allowed {
		t.Fatalf("wrong owner must be denied: %v / %v", allowed, err)
	}

	// Paused: false.
	if _, err := db.Pool.Exec(ctx, `UPDATE telegram_sources SET ingestion_status='paused' WHERE id=$1`, s.ID); err != nil {
		t.Fatal(err)
	}
	allowed, err = repo.AllowExternalProcessing(ctx, s.ID.String(), covBuyerID)
	if err != nil || allowed {
		t.Fatalf("paused must be denied: %v / %v", allowed, err)
	}
}

func TestCovTelegramSourceAllowExternalProcessingQueryError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE telegram_sources CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewTelegramSourceRepo(db).AllowExternalProcessing(context.Background(), uuid.New().String(), covBuyerID); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovTelegramSourceReserveLLMRequest(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewTelegramSourceRepo(db)

	// Within the limit of 3: each of the first three is allowed.
	for i := 0; i < 3; i++ {
		allowed, err := repo.ReserveLLMRequest(ctx, 3)
		if err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
		if !allowed {
			t.Fatalf("reserve %d should be allowed", i)
		}
	}
	// At the limit (requests already 3, not < 3): denied.
	allowed, err := repo.ReserveLLMRequest(ctx, 3)
	if err != nil {
		t.Fatalf("reserve at limit: %v", err)
	}
	if allowed {
		t.Fatal("reserve at limit must be denied")
	}
	// A higher limit re-allows: requests 3 < 5.
	allowed, err = repo.ReserveLLMRequest(ctx, 5)
	if err != nil || !allowed {
		t.Fatalf("reserve higher limit: %v / %v", allowed, err)
	}

	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT requests FROM llm_usage_buckets WHERE minute=date_trunc('minute',NOW())").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("expected 4 recorded requests, got %d", n)
	}
}

func TestCovTelegramSourceReserveLLMRequestError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE llm_usage_buckets CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewTelegramSourceRepo(db).ReserveLLMRequest(context.Background(), 1); err == nil {
		t.Fatal("expected error")
	}
}
