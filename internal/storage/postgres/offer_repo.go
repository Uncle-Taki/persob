package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/perso/perso-mind/internal/domain"
)

type OfferRepo struct {
	db *DB
}

func NewOfferRepo(db *DB) *OfferRepo {
	return &OfferRepo{db: db}
}

type OfferFilter struct {
	ProductID     *uuid.UUID
	SellerID      *int64
	Brand         string
	Model         string
	Condition     string
	ListingStatus string
	MinPrice      *int64
	MaxPrice      *int64
	SortBy        domain.SortBy
	Limit         int
	Offset        int
}

func (r *OfferRepo) Create(ctx context.Context, o *domain.Offer) error {
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

	query := `
		INSERT INTO offers (
			id, product_id, source_message_id, seller_id, brand, manufacturer, model,
			condition, quantity, price_amount, price_currency, price_raw, price_confidence,
			branded, expiration_date, seller_city, seller_area, seller_location_text,
			listing_status, extraction_confidence
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18,
			$19, $20
		)
		ON CONFLICT (source_message_id) DO UPDATE SET
			product_id = EXCLUDED.product_id,
			price_amount = EXCLUDED.price_amount,
			price_currency = EXCLUDED.price_currency,
			price_raw = EXCLUDED.price_raw,
			price_confidence = EXCLUDED.price_confidence,
			condition = EXCLUDED.condition,
			listing_status = EXCLUDED.listing_status,
			updated_at = NOW()
		RETURNING id, created_at, updated_at
	`
	err := r.db.Pool.QueryRow(ctx, query,
		o.ID, o.ProductID, o.SourceMessageID, o.SellerID, o.Brand, o.Manufacturer, o.Model,
		o.Condition, o.Quantity, o.PriceAmount, o.PriceCurrency, o.PriceRaw, o.PriceConfidence,
		o.Branded, o.ExpirationDate, o.SellerCity, o.SellerArea, o.SellerLocationText,
		o.ListingStatus, o.ExtractionConfidence,
	).Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert offer: %w", err)
	}

	// Also record price observation if price is known
	if o.PriceAmount != nil && *o.PriceAmount > 0 && o.ProductID != nil {
		obs := &domain.PriceObservation{
			ID:            uuid.New(),
			ProductID:     *o.ProductID,
			OfferID:       o.ID,
			PriceAmount:   *o.PriceAmount,
			PriceCurrency: o.PriceCurrency,
			ObservedAt:    time.Now(),
		}
		_ = r.RecordPriceObservation(ctx, obs)
	}

	return nil
}

func (r *OfferRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Offer, error) {
	query := `
		SELECT o.id, o.product_id, o.source_message_id, o.seller_id, o.brand, o.manufacturer, o.model,
		       o.condition, o.quantity, o.price_amount, o.price_currency, o.price_raw, o.price_confidence,
		       o.branded, o.expiration_date, o.seller_city, o.seller_area, o.seller_location_text,
		       o.listing_status, o.extraction_confidence, o.created_at, o.updated_at,
		       p.name, p.category
		FROM offers o
		LEFT JOIN products p ON o.product_id = p.id
		WHERE o.id = $1
	`
	var o domain.Offer
	var brand, mfr, model, pRaw, sCity, sArea, sLoc *string
	var pName, pCat *string

	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&o.ID, &o.ProductID, &o.SourceMessageID, &o.SellerID, &brand, &mfr, &model,
		&o.Condition, &o.Quantity, &o.PriceAmount, &o.PriceCurrency, &pRaw, &o.PriceConfidence,
		&o.Branded, &o.ExpirationDate, &sCity, &sArea, &sLoc,
		&o.ListingStatus, &o.ExtractionConfidence, &o.CreatedAt, &o.UpdatedAt,
		&pName, &pCat,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get offer by id: %w", err)
	}

	if brand != nil {
		o.Brand = *brand
	}
	if mfr != nil {
		o.Manufacturer = *mfr
	}
	if model != nil {
		o.Model = *model
	}
	if pRaw != nil {
		o.PriceRaw = *pRaw
	}
	if sCity != nil {
		o.SellerCity = *sCity
	}
	if sArea != nil {
		o.SellerArea = *sArea
	}
	if sLoc != nil {
		o.SellerLocationText = *sLoc
	}

	if o.ProductID != nil && pName != nil {
		o.Product = &domain.Product{
			ID:       *o.ProductID,
			Name:     *pName,
			Category: *pCat,
		}
	}

	return &o, nil
}

func (r *OfferRepo) List(ctx context.Context, f OfferFilter) ([]domain.Offer, int64, error) {
	whereClause := "WHERE 1=1"
	args := []any{}
	paramIdx := 1

	if f.ProductID != nil {
		whereClause += fmt.Sprintf(" AND o.product_id = $%d", paramIdx)
		args = append(args, *f.ProductID)
		paramIdx++
	}
	if f.SellerID != nil {
		whereClause += fmt.Sprintf(" AND o.seller_id = $%d", paramIdx)
		args = append(args, *f.SellerID)
		paramIdx++
	}
	if f.Brand != "" {
		whereClause += fmt.Sprintf(" AND o.brand ILIKE $%d", paramIdx)
		args = append(args, "%"+f.Brand+"%")
		paramIdx++
	}
	if f.Condition != "" {
		whereClause += fmt.Sprintf(" AND o.condition = $%d", paramIdx)
		args = append(args, f.Condition)
		paramIdx++
	}
	if f.ListingStatus != "" {
		whereClause += fmt.Sprintf(" AND o.listing_status = $%d", paramIdx)
		args = append(args, f.ListingStatus)
		paramIdx++
	}
	if f.MinPrice != nil {
		whereClause += fmt.Sprintf(" AND o.price_amount >= $%d", paramIdx)
		args = append(args, *f.MinPrice)
		paramIdx++
	}
	if f.MaxPrice != nil {
		whereClause += fmt.Sprintf(" AND o.price_amount <= $%d", paramIdx)
		args = append(args, *f.MaxPrice)
		paramIdx++
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM offers o " + whereClause
	var count int64
	if err := r.db.Pool.QueryRow(ctx, countQuery, args...).Scan(&count); err != nil {
		return nil, 0, fmt.Errorf("count offers: %w", err)
	}

	// Order by
	orderBy := "ORDER BY o.created_at DESC"
	switch f.SortBy {
	case domain.SortByLowestPrice:
		orderBy = "ORDER BY o.price_amount ASC NULLS LAST, o.created_at DESC"
	case domain.SortByHighestPrice:
		orderBy = "ORDER BY o.price_amount DESC NULLS LAST, o.created_at DESC"
	case domain.SortByNewest, domain.SortByFreshness:
		orderBy = "ORDER BY o.created_at DESC"
	case domain.SortByConfidence:
		orderBy = "ORDER BY o.extraction_confidence DESC, o.created_at DESC"
	}

	if f.Limit <= 0 {
		f.Limit = 20
	}

	query := fmt.Sprintf(`
		SELECT o.id, o.product_id, o.source_message_id, o.seller_id, o.brand, o.manufacturer, o.model,
		       o.condition, o.quantity, o.price_amount, o.price_currency, o.price_raw, o.price_confidence,
		       o.branded, o.expiration_date, o.seller_city, o.seller_area, o.seller_location_text,
		       o.listing_status, o.extraction_confidence, o.created_at, o.updated_at,
		       p.name, p.category
		FROM offers o
		LEFT JOIN products p ON o.product_id = p.id
		%s
		%s
		LIMIT $%d OFFSET $%d
	`, whereClause, orderBy, paramIdx, paramIdx+1)

	args = append(args, f.Limit, f.Offset)

	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list offers: %w", err)
	}
	defer rows.Close()

	var offers []domain.Offer
	for rows.Next() {
		var o domain.Offer
		var brand, mfr, model, pRaw, sCity, sArea, sLoc *string
		var pName, pCat *string

		if err := rows.Scan(
			&o.ID, &o.ProductID, &o.SourceMessageID, &o.SellerID, &brand, &mfr, &model,
			&o.Condition, &o.Quantity, &o.PriceAmount, &o.PriceCurrency, &pRaw, &o.PriceConfidence,
			&o.Branded, &o.ExpirationDate, &sCity, &sArea, &sLoc,
			&o.ListingStatus, &o.ExtractionConfidence, &o.CreatedAt, &o.UpdatedAt,
			&pName, &pCat,
		); err != nil {
			return nil, 0, fmt.Errorf("scan offer: %w", err)
		}

		if brand != nil {
			o.Brand = *brand
		}
		if mfr != nil {
			o.Manufacturer = *mfr
		}
		if model != nil {
			o.Model = *model
		}
		if pRaw != nil {
			o.PriceRaw = *pRaw
		}
		if sCity != nil {
			o.SellerCity = *sCity
		}
		if sArea != nil {
			o.SellerArea = *sArea
		}
		if sLoc != nil {
			o.SellerLocationText = *sLoc
		}

		if o.ProductID != nil && pName != nil {
			o.Product = &domain.Product{
				ID:       *o.ProductID,
				Name:     *pName,
				Category: *pCat,
			}
		}

		offers = append(offers, o)
	}

	return offers, count, nil
}

func (r *OfferRepo) GetActiveOffersByProductID(ctx context.Context, productID uuid.UUID) ([]domain.Offer, error) {
	offers, _, err := r.List(ctx, OfferFilter{
		ProductID:     &productID,
		ListingStatus: "active",
		Limit:         500,
		Offset:        0,
		SortBy:        domain.SortByNewest,
	})
	return offers, err
}

func (r *OfferRepo) RecordPriceObservation(ctx context.Context, obs *domain.PriceObservation) error {
	if obs.ID == uuid.Nil {
		obs.ID = uuid.New()
	}
	query := `
		INSERT INTO price_observations (id, product_id, offer_id, price_amount, price_currency, observed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.Pool.Exec(ctx, query, obs.ID, obs.ProductID, obs.OfferID, obs.PriceAmount, obs.PriceCurrency, obs.ObservedAt)
	return err
}

func (r *OfferRepo) CountTotalActive(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM offers WHERE listing_status = 'active'").Scan(&count)
	return count, err
}
