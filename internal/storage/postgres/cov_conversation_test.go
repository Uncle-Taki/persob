package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

// covConversation creates a product+offer+conversation, then reloads and returns
// a fresh copy (correct version) for mutation tests.
func covConversation(t *testing.T, db *postgres.DB, sellerID int64, source string, amount int64, withProduct bool) *domain.Conversation {
	t.Helper()
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	// A real offer with a real product is always required by the FK constraints
	// on conversations.offer_id and offers.product_id.
	productID := uuid.New()
	covProduct(t, db, productID, "Conv Product", "Apple", "M", "C")
	offerID := uuid.New()
	prod := &productID
	am := amount
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{
		ID: offerID, ProductID: prod, SourceMessageID: source, SellerID: sellerID,
		PriceAmount: &am, PriceCurrency: domain.CurrencyToman, SourceRevisionAt: covNow,
	}); err != nil {
		t.Fatalf("create offer: %v", err)
	}
	var convProduct *uuid.UUID
	if withProduct {
		convProduct = &productID
	}
	c := &domain.Conversation{
		ID: uuid.New(), SellerID: sellerID, OfferID: &offerID, ProductID: convProduct,
		OwnerID: &covOwnerID,
	}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	got, err := repo.GetByID(ctx, c.ID)
	if err != nil || got == nil {
		t.Fatalf("reload conversation: %v / %+v", err, got)
	}
	return got
}

func TestCovConversationCreateDefaults(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")

	offerID := uuid.New()
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{
		ID: offerID, SourceMessageID: "conv-def", SellerID: 5, SourceRevisionAt: covNow,
	}); err != nil {
		t.Fatal(err)
	}

	// Defaults: generated id, state NEW, max_questions 3, draft_key from offer.
	c := &domain.Conversation{ID: uuid.Nil, SellerID: 5, OfferID: &offerID}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID == uuid.Nil {
		t.Fatal("expected generated id")
	}
	if c.State != domain.ConvStateNew || c.MaxQuestions != 3 {
		t.Fatalf("defaults not applied: state=%s max=%d", c.State, c.MaxQuestions)
	}
	if c.DraftKey != offerID.String() {
		t.Fatalf("draft key = %q, want %q", c.DraftKey, offerID.String())
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		t.Fatalf("timestamps missing: %+v", c)
	}
}

func TestCovConversationCreateConflictFallback(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")

	offerID := uuid.New()
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{
		ID: offerID, SourceMessageID: "conv-dup", SellerID: 5, SourceRevisionAt: covNow,
	}); err != nil {
		t.Fatal(err)
	}

	first := &domain.Conversation{ID: uuid.New(), SellerID: 5, OfferID: &offerID}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Second conversation for the same offer: same draft_key -> conflict ->
	// fallback returns the existing row's id.
	second := &domain.Conversation{ID: uuid.New(), SellerID: 5, OfferID: &offerID}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("second create: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected fallback to reuse id %v, got %v", first.ID, second.ID)
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM conversations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 conversation, got %d", n)
	}
}

func TestCovConversationGetByID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")

	c := covConversation(t, db, 7, "get-offer", 100, true)
	got, err := repo.GetByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != c.ID || got.SellerID != 7 || got.State != domain.ConvStateNew {
		t.Fatalf("fields mismatch: %+v", got)
	}
	if got.OwnerID == nil || *got.OwnerID != covOwnerID {
		t.Fatalf("owner mismatch: %+v", got)
	}
	if got.Product == nil || got.Product.Name != "Conv Product" || got.Product.Category != "C" {
		t.Fatalf("product join missing: %+v", got.Product)
	}
	if got.Version != 0 {
		t.Fatalf("expected version 0, got %d", got.Version)
	}

	// Conversation without a product: no join.
	offerID := uuid.New()
	if err := postgres.NewOfferRepo(db).Create(ctx, &domain.Offer{ID: offerID, SourceMessageID: "get-noop", SellerID: 9, SourceRevisionAt: covNow}); err != nil {
		t.Fatal(err)
	}
	plain := &domain.Conversation{ID: uuid.New(), SellerID: 9, OfferID: &offerID}
	if err := repo.Create(ctx, plain); err != nil {
		t.Fatal(err)
	}
	plainGot, err := repo.GetByID(ctx, plain.ID)
	if err != nil {
		t.Fatalf("get plain: %v", err)
	}
	if plainGot.Product != nil {
		t.Fatalf("expected nil product, got %+v", plainGot.Product)
	}

	missing, err := repo.GetByID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing conversation, got %+v", missing)
	}
}

func TestCovConversationGetByIDError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE conversations CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewConversationRepo(db).GetByID(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovConversationGetByOfferID(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")

	c := covConversation(t, db, 8, "by-offer", 100, false)
	got, err := repo.GetByOfferID(ctx, *c.OfferID)
	if err != nil {
		t.Fatalf("get by offer: %v", err)
	}
	if got.ID != c.ID {
		t.Fatalf("id mismatch: %v vs %v", got.ID, c.ID)
	}

	none, err := repo.GetByOfferID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if none != nil {
		t.Fatalf("expected nil for unknown offer, got %+v", none)
	}
}

func TestCovConversationGetByOfferError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE conversations CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewConversationRepo(db).GetByOfferID(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovConversationUpdateState(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 10, "upd-state", 100, false)

	if err := repo.UpdateState(ctx, c.ID, domain.ConvStateAwaitingConsent, "note A"); err != nil {
		t.Fatalf("update state: %v", err)
	}
	got, _ := repo.GetByID(ctx, c.ID)
	if got.State != domain.ConvStateAwaitingConsent || got.Notes != "note A" {
		t.Fatalf("state mismatch: %+v", got)
	}
	// Empty notes must keep the previous value.
	if err := repo.UpdateState(ctx, c.ID, domain.ConvStateWaitingForReply, ""); err != nil {
		t.Fatalf("update state 2: %v", err)
	}
	got, _ = repo.GetByID(ctx, c.ID)
	if got.State != domain.ConvStateWaitingForReply || got.Notes != "note A" {
		t.Fatalf("notes should persist: %+v", got)
	}
}

func TestCovConversationApprove(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 11, "approve", 100, false)

	if err := repo.Approve(ctx, c.ID, covOwnerID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, _ := repo.GetByID(ctx, c.ID)
	if got.State != domain.ConvStateActive {
		t.Fatalf("expected ACTIVE, got %s", got.State)
	}
	if got.ApprovedByUserID == nil || *got.ApprovedByUserID != covOwnerID {
		t.Fatalf("approver mismatch: %+v", got)
	}
	if got.ApprovedAt == nil {
		t.Fatal("expected approved_at set")
	}
}

func TestCovConversationIncrementQuestions(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 12, "incr", 100, false)

	for i := 0; i < 3; i++ {
		if err := repo.IncrementQuestions(ctx, c.ID); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
	}
	got, _ := repo.GetByID(ctx, c.ID)
	if got.QuestionsAskedCount != 3 {
		t.Fatalf("expected 3 questions, got %d", got.QuestionsAskedCount)
	}
}

func TestCovConversationUpdateCollectedData(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 13, "collected", 100, false)

	remaining := []domain.MissingField{domain.FieldCondition, domain.FieldWarranty}
	collected := map[string]any{"price_amount": json.Number("150"), "brand": "apple"}
	if err := repo.UpdateCollectedData(ctx, c.ID, collected, remaining); err != nil {
		t.Fatalf("update collected: %v", err)
	}
	got, _ := repo.GetByID(ctx, c.ID)
	if len(got.MissingFields) != 2 || got.MissingFields[0] != domain.FieldCondition {
		t.Fatalf("missing fields mismatch: %+v", got.MissingFields)
	}
	if v, ok := got.CollectedData["price_amount"].(json.Number); !ok || v.String() != "150" {
		t.Fatalf("collected data mismatch: %+v", got.CollectedData)
	}
	if got.CollectedData["brand"] != "apple" {
		t.Fatalf("collected data mismatch: %+v", got.CollectedData)
	}
}

func TestCovConversationListFilters(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	covUser(t, db, covBuyerID, "buyer")

	// Two owned by covOwnerID.
	c1 := covConversation(t, db, 20, "list-1", 100, true)
	covConversation(t, db, 21, "list-2", 200, true)
	// Move c1 to ACTIVE.
	if err := repo.UpdateState(ctx, c1.ID, domain.ConvStateActive, ""); err != nil {
		t.Fatal(err)
	}

	// No filter.
	all, total, err := repo.List(ctx, "", 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Fatalf("want 2, got %d / %d", len(all), total)
	}
	// Both should carry the product join.
	for _, c := range all {
		if c.Product == nil {
			t.Fatalf("product join missing: %+v", c)
		}
	}

	// State filter.
	active, total, err := repo.List(ctx, "ACTIVE", 0, 0)
	if err != nil || total != 1 || active[0].ID != c1.ID {
		t.Fatalf("state filter: %+v / %d / %v", active, total, err)
	}

	// Owner filter via ListOwned.
	owned, total, err := repo.ListOwned(ctx, "", 0, 0, &covOwnerID)
	if err != nil || total != 2 || len(owned) != 2 {
		t.Fatalf("owner filter: %d / %d / %v", len(owned), total, err)
	}
	none, total, err := repo.ListOwned(ctx, "", 0, 0, &covBuyerID)
	if err != nil || total != 0 || len(none) != 0 {
		t.Fatalf("owner empty: %d / %d / %v", len(none), total, err)
	}

	// Pagination: limit 1 offset 1 -> 1 row.
	page, total, err := repo.List(ctx, "", 1, 1)
	if err != nil || total != 2 || len(page) != 1 {
		t.Fatalf("pagination: %+v / %d / %v", page, total, err)
	}
}

func TestCovConversationListQueryError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE conversations CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := postgres.NewConversationRepo(db).List(context.Background(), "", 0, 0); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := postgres.NewConversationRepo(db).ListOwned(context.Background(), "", 0, 0, &covOwnerID); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovConversationSaveProgressVersionConflict(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 30, "sp-conflict", 100, false)

	// Stale version: row must not be updated.
	c.Version = 99
	err := repo.SaveProgress(ctx, c, "")
	if err == nil || !strings.Contains(err.Error(), "conversation changed; reload and retry") {
		t.Fatalf("expected version conflict error, got %v", err)
	}
}

func TestCovConversationSaveProgressBasic(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 31, "sp-basic", 100, false)

	// Advance state with a message and no collected offer fields.
	c.State = domain.ConvStateWaitingForReply
	c.CollectedData = map[string]any{"note": "x"}
	if err := repo.SaveProgress(ctx, c, "Hello seller"); err != nil {
		t.Fatalf("save progress: %v", err)
	}
	if c.Version != 1 {
		t.Fatalf("version not incremented: %d", c.Version)
	}
	got, _ := repo.GetByID(ctx, c.ID)
	if got.State != domain.ConvStateWaitingForReply {
		t.Fatalf("state mismatch: %+v", got)
	}
	// The message must be queued.
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbound_messages WHERE conversation_id=$1 AND text='Hello seller' AND status='queued'", c.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 queued outbound message, got %d", n)
	}
}

func TestCovConversationSaveProgressInboundReceipt(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 32, "sp-inbound", 100, false)

	// First inbound: receipt recorded, conversation advanced.
	inCtx := postgres.WithInbound(ctx, -55, 1001, "seller says hi")
	c.State = domain.ConvStateActive
	if err := repo.SaveProgress(inCtx, c, ""); err != nil {
		t.Fatalf("save progress inbound: %v", err)
	}
	if c.Version != 1 {
		t.Fatalf("version mismatch: %d", c.Version)
	}
	var rec int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM inbound_receipts WHERE peer_id=-55 AND message_id=1001").Scan(&rec); err != nil {
		t.Fatal(err)
	}
	if rec != 1 {
		t.Fatalf("expected 1 receipt, got %d", rec)
	}

	// Reload fresh (version 1) and replay the SAME inbound event: receipt
	// conflict -> early return, version unchanged.
	fresh, _ := repo.GetByID(ctx, c.ID)
	fresh.State = domain.ConvStateActive
	if err := repo.SaveProgress(postgres.WithInbound(ctx, -55, 1001, "seller says hi"), fresh, ""); err != nil {
		t.Fatalf("replay inbound: %v", err)
	}
	if fresh.Version != 1 {
		t.Fatalf("duplicate inbound must not advance version, got %d", fresh.Version)
	}
}

func TestCovConversationSaveProgressPriceIntake(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 33, "sp-price", 100, true)
	// The offer create already recorded one observation; the intake adds one more.

	// Consent granted with a typed integer price (toman), new currency.
	c.ConsentGranted = true
	c.State = domain.ConvStateActive
	c.CollectedData = map[string]any{
		"price_amount":       int64(150),
		"price_currency":     string(domain.CurrencyRial),
		"seller_city":        "Shiraz",
		"seller_tehran_area": "",
		"condition":          string(domain.ConditionUsedGood),
		"manufacturer":       "Apple Inc",
		"branded":            true,
		"warranty_until":     "2027-01-01",
		"expiration_date":    "",
	}
	if err := repo.SaveProgress(ctx, c, ""); err != nil {
		t.Fatalf("save progress price: %v", err)
	}
	var price int64
	var currency, city, cond, mfr string
	var brandedKnown, warrantyKnown bool
	var warranty *time.Time
	if err := db.Pool.QueryRow(ctx, `SELECT price_amount, price_currency, seller_city, condition, manufacturer, branded_known, warranty_known, warranty_until FROM offers WHERE id=$1`, *c.OfferID).Scan(&price, &currency, &city, &cond, &mfr, &brandedKnown, &warrantyKnown, &warranty); err != nil {
		t.Fatal(err)
	}
	if price != 150 || currency != string(domain.CurrencyRial) {
		t.Fatalf("price mismatch: %d / %s", price, currency)
	}
	if city != "Shiraz" || cond != string(domain.ConditionUsedGood) || mfr != "Apple Inc" {
		t.Fatalf("intake fields mismatch: %q / %q / %q", city, cond, mfr)
	}
	if !brandedKnown || !warrantyKnown {
		t.Fatalf("known flags not set: %v / %v", brandedKnown, warrantyKnown)
	}
	// A price observation must have been recorded (price changed 100->150),
	// on top of the one created when the offer itself was inserted.
	var obs, intakeObs int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM price_observations WHERE offer_id=$1", *c.OfferID).Scan(&obs); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM price_observations WHERE offer_id=$1 AND revision_hash LIKE 'intake:%'`, *c.OfferID).Scan(&intakeObs); err != nil {
		t.Fatal(err)
	}
	if obs != 2 || intakeObs != 1 {
		t.Fatalf("expected 2 observations with 1 intake, got %d / %d", obs, intakeObs)
	}
}

func TestCovConversationSaveProgressPriceJSONNumber(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 34, "sp-jsonnum", 100, true)

	// json.Number price with empty currency (defaults to Toman).
	c.ConsentGranted = true
	c.State = domain.ConvStateActive
	c.CollectedData = map[string]any{"price_amount": json.Number("175")}
	if err := repo.SaveProgress(ctx, c, ""); err != nil {
		t.Fatalf("save progress json number: %v", err)
	}
	var price int64
	var currency string
	if err := db.Pool.QueryRow(ctx, `SELECT price_amount, price_currency FROM offers WHERE id=$1`, *c.OfferID).Scan(&price, &currency); err != nil {
		t.Fatal(err)
	}
	if price != 175 || currency != string(domain.CurrencyToman) {
		t.Fatalf("json number price mismatch: %d / %s", price, currency)
	}
}

func TestCovConversationSaveProgressPriceErrors(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")

	// Non-integer collected price -> "collected price must be an integer".
	c1 := covConversation(t, db, 35, "sp-badprice", 100, true)
	c1.ConsentGranted = true
	c1.State = domain.ConvStateActive
	c1.CollectedData = map[string]any{"price_amount": "not-a-number"}
	if err := repo.SaveProgress(ctx, c1, ""); err == nil || !strings.Contains(err.Error(), "collected price must be an integer") {
		t.Fatalf("expected integer error, got %v", err)
	}

	// Non-integral json.Number -> Int64 parse error.
	c2 := covConversation(t, db, 36, "sp-floatprice", 100, true)
	c2.ConsentGranted = true
	c2.State = domain.ConvStateActive
	c2.CollectedData = map[string]any{"price_amount": json.Number("12.5")}
	if err := repo.SaveProgress(ctx, c2, ""); err == nil {
		t.Fatal("expected parse error for fractional price")
	}

	// Bad warranty date -> parse error.
	c3 := covConversation(t, db, 37, "sp-baddate", 100, true)
	c3.ConsentGranted = true
	c3.State = domain.ConvStateActive
	c3.CollectedData = map[string]any{"warranty_until": "not-a-date"}
	if err := repo.SaveProgress(ctx, c3, ""); err == nil {
		t.Fatal("expected date parse error")
	}
}

func TestCovConversationSaveProgressMissingOffer(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")

	// Consent + offer reference that does not exist: the FOR UPDATE price read
	// must fail. The FK forbids such a reference, so the constraint is relaxed
	// in this disposable schema to simulate a concurrently-deleted offer.
	if _, err := db.Pool.Exec(ctx, "ALTER TABLE conversations DROP CONSTRAINT conversations_offer_id_fkey"); err != nil {
		t.Fatal(err)
	}
	missingOffer := uuid.New()
	c := &domain.Conversation{ID: uuid.New(), SellerID: 40, OfferID: &missingOffer}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	c.ConsentGranted = true
	c.State = domain.ConvStateActive
	c.CollectedData = map[string]any{"price_amount": int64(100)}
	if err := repo.SaveProgress(ctx, c, ""); err == nil {
		t.Fatal("expected error for missing offer price read")
	}
}

func TestCovConversationSaveProgressCancelOutbound(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 41, "sp-cancel", 100, false)

	// Queue a message, then transition to a terminal state that cancels it.
	c.State = domain.ConvStateActive
	if err := repo.SaveProgress(ctx, c, "pending question"); err != nil {
		t.Fatalf("queue: %v", err)
	}
	fresh, _ := repo.GetByID(ctx, c.ID)
	fresh.State = domain.ConvStateCompleted
	if err := repo.SaveProgress(ctx, fresh, ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	var queued, cancelled int
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbound_messages WHERE conversation_id=$1 AND status='queued'", c.ID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbound_messages WHERE conversation_id=$1 AND status='cancelled'", c.ID).Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if queued != 0 || cancelled != 1 {
		t.Fatalf("expected queued=0 cancelled=1, got %d / %d", queued, cancelled)
	}
}

func TestCovConversationFindActiveBySeller(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")

	c := covConversation(t, db, 50, "fa-seller", 100, false)
	if err := repo.UpdateState(ctx, c.ID, domain.ConvStateActive, ""); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindActiveBySeller(ctx, 50)
	if err != nil {
		t.Fatalf("find active: %v", err)
	}
	if got.ID != c.ID {
		t.Fatalf("id mismatch: %v vs %v", got.ID, c.ID)
	}

	// NEW state is not "active" by definition.
	none, err := repo.FindActiveBySeller(ctx, 51)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if none != nil {
		t.Fatalf("expected nil, got %+v", none)
	}
}

func TestCovConversationFindActiveBySellerError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE conversations CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewConversationRepo(db).FindActiveBySeller(context.Background(), 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovConversationQueueHistory(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewConversationRepo(db)
	covUser(t, db, covOwnerID, "owner")
	c := covConversation(t, db, 60, "qh", 100, false)

	// Empty history.
	empty, err := repo.QueueHistory(ctx, c.ID)
	if err != nil {
		t.Fatalf("empty queue: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty history, got %+v", empty)
	}

	// Add an outbound message and an inbound receipt.
	c.State = domain.ConvStateActive
	if err := repo.SaveProgress(ctx, c, "assistant says hi"); err != nil {
		t.Fatalf("save progress: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO inbound_receipts(peer_id,message_id,conversation_id,text) VALUES(-77, 2002, $1, 'seller reply')`, c.ID); err != nil {
		t.Fatal(err)
	}

	got, err := repo.QueueHistory(ctx, c.ID)
	if err != nil {
		t.Fatalf("queue history: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 history entries, got %d: %+v", len(got), got)
	}
	var sawAssistant, sawSeller bool
	for _, h := range got {
		if h["sender"] == "assistant" && h["text"] == "assistant says hi" {
			sawAssistant = true
			if h["status"] != "queued" {
				t.Fatalf("assistant status = %v", h["status"])
			}
		}
		if h["sender"] == "seller" && h["text"] == "seller reply" {
			sawSeller = true
			if h["status"] != "received" {
				t.Fatalf("seller status = %v", h["status"])
			}
		}
	}
	if !sawAssistant || !sawSeller {
		t.Fatalf("history entries missing: %+v", got)
	}
}

var _ = json.Marshal

func TestCovConversationQueueHistoryError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE outbound_messages CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewConversationRepo(db).QueueHistory(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error")
	}
}
