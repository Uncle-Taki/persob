package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/perso/perso-mind/internal/domain"
)

type SellerProfileRepo struct {
	db *DB
}

func NewSellerProfileRepo(db *DB) *SellerProfileRepo {
	return &SellerProfileRepo{db: db}
}

func (r *SellerProfileRepo) Upsert(ctx context.Context, profile *domain.SellerProfile) error {
	query := `
		INSERT INTO seller_profiles (
			seller_id, username, display_name, city, tehran_area, total_offers, opted_out, cooldown_until, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW()
		)
		ON CONFLICT (seller_id) DO UPDATE SET
			username = COALESCE(NULLIF(EXCLUDED.username, ''), seller_profiles.username),
			display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), seller_profiles.display_name),
			city = COALESCE(NULLIF(EXCLUDED.city, ''), seller_profiles.city),
			tehran_area = COALESCE(NULLIF(EXCLUDED.tehran_area, ''), seller_profiles.tehran_area),
			total_offers = seller_profiles.total_offers + EXCLUDED.total_offers,
			updated_at = NOW()
		RETURNING created_at, updated_at
	`
	err := r.db.Pool.QueryRow(ctx, query,
		profile.SellerID, profile.Username, profile.DisplayName,
		profile.City, profile.TehranArea, profile.TotalOffers,
		profile.OptedOut, profile.CooldownUntil,
	).Scan(&profile.CreatedAt, &profile.UpdatedAt)

	if err != nil {
		return fmt.Errorf("upsert seller profile: %w", err)
	}

	return nil
}

func (r *SellerProfileRepo) GetByID(ctx context.Context, sellerID int64) (*domain.SellerProfile, error) {
	query := `
		SELECT seller_id, username, display_name, city, tehran_area, total_offers, opted_out, cooldown_until, created_at, updated_at
		FROM seller_profiles
		WHERE seller_id = $1
	`
	var p domain.SellerProfile
	err := r.db.Pool.QueryRow(ctx, query, sellerID).Scan(
		&p.SellerID, &p.Username, &p.DisplayName, &p.City, &p.TehranArea,
		&p.TotalOffers, &p.OptedOut, &p.CooldownUntil, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("seller not found")
		}
		return nil, fmt.Errorf("get seller profile: %w", err)
	}

	return &p, nil
}

func (r *SellerProfileRepo) IncrementOfferCount(ctx context.Context, sellerID int64) error {
	query := `
		UPDATE seller_profiles
		SET total_offers = total_offers + 1, updated_at = NOW()
		WHERE seller_id = $1
	`
	_, err := r.db.Pool.Exec(ctx, query, sellerID)
	if err != nil {
		return fmt.Errorf("increment seller offer count: %w", err)
	}
	return nil
}

func (r *SellerProfileRepo) SetOptOut(ctx context.Context, sellerID int64, optOut bool) error {
	query := `
		UPDATE seller_profiles
		SET opted_out = $2, updated_at = NOW()
		WHERE seller_id = $1
	`
	_, err := r.db.Pool.Exec(ctx, query, sellerID, optOut)
	if err != nil {
		return fmt.Errorf("set seller opt out: %w", err)
	}
	return nil
}

func (r *SellerProfileRepo) SetCooldown(ctx context.Context, sellerID int64, until time.Time) error {
	query := `
		UPDATE seller_profiles
		SET cooldown_until = $2, updated_at = NOW()
		WHERE seller_id = $1
	`
	_, err := r.db.Pool.Exec(ctx, query, sellerID, until)
	if err != nil {
		return fmt.Errorf("set seller cooldown: %w", err)
	}
	return nil
}
