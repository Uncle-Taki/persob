package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/perso/perso-mind/internal/domain"
)

type OfferRepo struct{ db *DB }

func NewOfferRepo(db *DB) *OfferRepo { return &OfferRepo{db: db} }

type OfferFilter struct {
	ProductID                              *uuid.UUID
	SellerID                               *int64
	OwnerID                                *uuid.UUID
	WorkspaceID                            *uuid.UUID
	Brand, Model, Condition, ListingStatus string
	MinPrice, MaxPrice                     *int64
	SortBy                                 domain.SortBy
	Limit, Offset                          int
	// Unbounded is reserved for internal aggregation/ranking, never read from a request.
	Unbounded bool
}

// Create commits a listing and its price history together. Replayed events cannot
// duplicate history, and older message revisions cannot overwrite newer ones.
func (r *OfferRepo) Create(ctx context.Context, o *domain.Offer) error {
	o.WorkspaceID = domain.WorkspaceID
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	if o.PriceCurrency == "" {
		o.PriceCurrency = domain.CurrencyToman
	}
	if o.ListingStatus == "" {
		o.ListingStatus = domain.ListingStatusActive
	}
	if o.Condition == "" {
		o.Condition = domain.ConditionUnknown
	}
	if o.Quantity <= 0 {
		o.Quantity = 1
	}
	if o.SourceRevisionAt.IsZero() {
		o.SourceRevisionAt = time.Unix(0, 0).UTC()
	}
	if o.SourceItemKey == "" {
		if o.ProductID != nil {
			o.SourceItemKey = o.ProductID.String()
		} else {
			o.SourceItemKey = "legacy:" + strings.ToLower(o.Brand+":"+o.Model)
		}
	}
	if o.PriceAmount != nil && *o.PriceAmount < 0 {
		return fmt.Errorf("negative offer price")
	}
	if o.PriceCurrency != domain.CurrencyToman && o.PriceCurrency != domain.CurrencyRial {
		return fmt.Errorf("unsupported currency")
	}
	canonical, _ := json.Marshal([]any{o.ProductID, o.SellerID, o.Brand, o.Manufacturer, o.Model, o.Condition, o.Quantity, o.PriceAmount, o.PriceCurrency, o.Branded, o.BrandedKnown, o.SellerCity, o.SellerArea, o.ListingStatus, o.ExpirationDate, o.ListingExpiresAt})
	o.ContentHash = fmt.Sprintf("%x", sha256.Sum256(canonical))
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", o.SourceMessageID); err != nil {
		return err
	}
	var oldHash string
	var oldRev time.Time
	err = tx.QueryRow(ctx, `SELECT id,content_hash,source_revision_at FROM offers WHERE source_message_id=$1 AND source_item_key=$2 FOR UPDATE`, o.SourceMessageID, o.SourceItemKey).Scan(&o.ID, &oldHash, &oldRev)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && oldRev.After(o.SourceRevisionAt) {
		o.Applied = false
		return tx.Commit(ctx)
	}
	query := `INSERT INTO offers(id,product_id,source_message_id,source_item_key,source_revision_at,source_published_at,content_hash,owner_id,seller_id,brand,manufacturer,model,condition,quantity,price_amount,price_currency,price_raw,price_confidence,branded,branded_known,expiration_date,listing_expires_at,warranty_until,seller_city,seller_area,seller_location_text,listing_status,extraction_confidence,warranty_known,variant_match,workspace_id,source_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32)
 ON CONFLICT(source_message_id,source_item_key) DO UPDATE SET source_id=COALESCE(EXCLUDED.source_id,offers.source_id),product_id=EXCLUDED.product_id,source_revision_at=EXCLUDED.source_revision_at,source_published_at=EXCLUDED.source_published_at,content_hash=EXCLUDED.content_hash,owner_id=COALESCE(offers.owner_id,EXCLUDED.owner_id),seller_id=EXCLUDED.seller_id,brand=EXCLUDED.brand,manufacturer=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.manufacturer ELSE EXCLUDED.manufacturer END,model=EXCLUDED.model,condition=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.condition ELSE EXCLUDED.condition END,quantity=EXCLUDED.quantity,price_amount=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.price_amount ELSE EXCLUDED.price_amount END,price_currency=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.price_currency ELSE EXCLUDED.price_currency END,price_raw=EXCLUDED.price_raw,price_confidence=EXCLUDED.price_confidence,branded=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.branded ELSE EXCLUDED.branded END,branded_known=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.branded_known ELSE EXCLUDED.branded_known END,expiration_date=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.expiration_date ELSE EXCLUDED.expiration_date END,listing_expires_at=EXCLUDED.listing_expires_at,warranty_known=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.warranty_known ELSE EXCLUDED.warranty_known END,warranty_until=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.warranty_until ELSE EXCLUDED.warranty_until END,seller_city=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.seller_city ELSE EXCLUDED.seller_city END,seller_area=CASE WHEN offers.intake_updated_at IS NOT NULL AND EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.seller_area ELSE EXCLUDED.seller_area END,seller_location_text=EXCLUDED.seller_location_text,listing_status=EXCLUDED.listing_status,variant_match=EXCLUDED.variant_match,extraction_confidence=EXCLUDED.extraction_confidence,intake_updated_at=CASE WHEN EXCLUDED.source_revision_at<=offers.intake_updated_at THEN offers.intake_updated_at ELSE NULL END,updated_at=NOW()
 RETURNING id,created_at,updated_at,price_amount,price_currency,seller_city,seller_area`
	err = tx.QueryRow(ctx, query, o.ID, o.ProductID, o.SourceMessageID, o.SourceItemKey, o.SourceRevisionAt, o.SourcePublishedAt, o.ContentHash, o.OwnerID, o.SellerID, o.Brand, o.Manufacturer, o.Model, o.Condition, o.Quantity, o.PriceAmount, o.PriceCurrency, o.PriceRaw, o.PriceConfidence, o.Branded, o.BrandedKnown, o.ExpirationDate, o.ListingExpiresAt, o.WarrantyUntil, o.SellerCity, o.SellerArea, o.SellerLocationText, o.ListingStatus, o.ExtractionConfidence, o.WarrantyKnown, o.VariantMatch, o.WorkspaceID, o.SourceID).Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt, &o.PriceAmount, &o.PriceCurrency, &o.SellerCity, &o.SellerArea)
	if err != nil {
		return fmt.Errorf("upsert offer: %w", err)
	}
	if oldHash != o.ContentHash && o.PriceAmount != nil && *o.PriceAmount > 0 && o.ProductID != nil {
		// A source revision is a stable event identity; direct repository calls use the content hash.
		revision := o.ContentHash
		if !o.SourceRevisionAt.Equal(time.Unix(0, 0)) {
			revision = fmt.Sprintf("%d:%s", o.SourceRevisionAt.UnixNano(), o.ContentHash)
		}
		observed := o.SourceRevisionAt
		if observed.Equal(time.Unix(0, 0)) {
			observed = time.Now().UTC()
		}
		_, err = tx.Exec(ctx, `INSERT INTO price_observations(id,product_id,offer_id,price_amount,price_currency,observed_at,revision_hash) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(offer_id,revision_hash) WHERE revision_hash IS NOT NULL DO NOTHING`, uuid.New(), o.ProductID, o.ID, *o.PriceAmount, o.PriceCurrency, observed, revision)
		if err != nil {
			return fmt.Errorf("record observation: %w", err)
		}
	}
	o.Applied = true
	return tx.Commit(ctx)
}

const offerJSON = `SELECT to_jsonb(o)||jsonb_build_object('product',CASE WHEN p.id IS NULL THEN NULL ELSE to_jsonb(p) END) FROM offers o LEFT JOIN products p ON p.id=o.product_id `

func (r *OfferRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Offer, error) {
	var raw []byte
	err := r.db.Pool.QueryRow(ctx, offerJSON+"WHERE o.id=$1", id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var o domain.Offer
	err = json.Unmarshal(raw, &o)
	return &o, err
}

// Active listings must also have unexpired product/listing validity. Warranty is separate.
const eligibleOffer = `o.listing_status='active' AND (o.expiration_date IS NULL OR o.expiration_date>NOW()) AND (o.listing_expires_at IS NULL OR o.listing_expires_at>NOW())`
const moneySQL = `CASE WHEN o.price_currency='IRR_RIAL' THEN o.price_amount/10 ELSE o.price_amount END`

func (r *OfferRepo) List(ctx context.Context, f OfferFilter) ([]domain.Offer, int64, error) {
	where := "WHERE 1=1"
	args := []any{}
	add := func(expr string, v any) { args = append(args, v); where += fmt.Sprintf(" AND "+expr, len(args)) }
	if f.ProductID != nil {
		add("o.product_id=$%d", *f.ProductID)
	}
	if f.SellerID != nil {
		add("o.seller_id=$%d", *f.SellerID)
	}
	if f.WorkspaceID != nil {
		add("o.workspace_id=$%d", *f.WorkspaceID)
	}
	if f.OwnerID != nil {
		add("o.owner_id=$%d", *f.OwnerID)
	}
	if f.Brand != "" {
		add("o.brand ILIKE $%d", "%"+f.Brand+"%")
	}
	if f.Model != "" {
		add("o.model ILIKE $%d", "%"+f.Model+"%")
	}
	if f.Condition != "" {
		add("o.condition=$%d", f.Condition)
	}
	if f.ListingStatus == "active" {
		where += " AND " + eligibleOffer
	} else if f.ListingStatus != "" {
		add("o.listing_status=$%d", f.ListingStatus)
	}
	if f.MinPrice != nil {
		add(moneySQL+">=$%d", *f.MinPrice)
	}
	if f.MaxPrice != nil {
		add(moneySQL+"<=$%d", *f.MaxPrice)
	}
	var total int64
	if err := r.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM offers o "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "ORDER BY COALESCE(o.source_published_at,o.created_at) DESC,o.id ASC"
	switch f.SortBy {
	case domain.SortByLowestPrice:
		order = "ORDER BY " + moneySQL + " ASC NULLS LAST,o.id ASC"
	case domain.SortByHighestPrice:
		order = "ORDER BY " + moneySQL + " DESC NULLS LAST,o.id ASC"
	case domain.SortByConfidence:
		order = "ORDER BY o.extraction_confidence DESC,o.id ASC"
	}
	query := offerJSON + where + " " + order
	if !f.Unbounded {
		if f.Limit <= 0 {
			f.Limit = 20
		}
		if f.Offset < 0 {
			f.Offset = 0
		}
		args = append(args, f.Limit, f.Offset)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}
	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	offers := []domain.Offer{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		var o domain.Offer
		if err = json.Unmarshal(raw, &o); err != nil {
			return nil, 0, err
		}
		offers = append(offers, o)
	}
	return offers, total, rows.Err()
}
func (r *OfferRepo) GetActiveOffersByProductID(ctx context.Context, id uuid.UUID) ([]domain.Offer, error) {
	o, _, err := r.List(ctx, OfferFilter{ProductID: &id, ListingStatus: "active", Unbounded: true})
	return o, err
}
func (r *OfferRepo) RecordPriceObservation(ctx context.Context, o *domain.PriceObservation) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	_, err := r.db.Pool.Exec(ctx, `INSERT INTO price_observations(id,product_id,offer_id,price_amount,price_currency,observed_at) VALUES($1,$2,$3,$4,$5,$6)`, o.ID, o.ProductID, o.OfferID, o.PriceAmount, o.PriceCurrency, o.ObservedAt)
	return err
}
func (r *OfferRepo) CountTotalActive(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM offers o WHERE "+eligibleOffer).Scan(&n)
	return n, err
}
func (r *OfferRepo) RetireMissing(ctx context.Context, source string, revision time.Time, keys []string) error {
	_, err := r.db.Pool.Exec(ctx, `UPDATE offers SET listing_status='expired',source_revision_at=$2,updated_at=NOW() WHERE source_message_id=$1 AND source_revision_at<=$2 AND NOT(source_item_key=ANY($3::text[]))`, source, revision, keys)
	return err
}
