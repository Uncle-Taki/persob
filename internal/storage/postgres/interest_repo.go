package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/perso/perso-mind/internal/domain"
)

type InterestRepo struct {
	db *DB
}

func NewInterestRepo(db *DB) *InterestRepo {
	return &InterestRepo{db: db}
}

func (r *InterestRepo) Create(ctx context.Context, interest *domain.Interest) error {
	if interest.ID == uuid.Nil {
		interest.ID = uuid.New()
	}
	// Enforce Phase 1 invariants: Sell = false, Buy = true
	interest.SellEnabled = false
	interest.BuyEnabled = true

	query := `
		INSERT INTO interests (id, user_id, product_id, sell_enabled, buy_enabled, max_price_amount, min_quantity, preferred_city, required_attributes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id, product_id) DO UPDATE SET buy_enabled = true, max_price_amount=EXCLUDED.max_price_amount, min_quantity=EXCLUDED.min_quantity, preferred_city=EXCLUDED.preferred_city, required_attributes=EXCLUDED.required_attributes
		RETURNING id, created_at
	`
	return r.db.Pool.QueryRow(ctx, query,
		interest.ID, interest.UserID, interest.ProductID, interest.SellEnabled, interest.BuyEnabled, interest.MaxPriceAmount, interest.MinQuantity, interest.PreferredCity, func() []byte {
			if len(interest.RequiredAttributes) == 0 {
				return []byte("{}")
			}
			return interest.RequiredAttributes
		}(),
	).Scan(&interest.ID, &interest.CreatedAt)
}

func (r *InterestRepo) Update(ctx context.Context, id, userID uuid.UUID, req domain.UpdateInterestRequest) (*domain.Interest, error) {
	attrs := req.RequiredAttributes
	if len(attrs) == 0 {
		attrs = nil
	}
	var item domain.Interest
	err := r.db.Pool.QueryRow(ctx, `UPDATE interests SET max_price_amount=$3,min_quantity=$4,preferred_city=COALESCE($5,preferred_city),required_attributes=COALESCE($6,required_attributes) WHERE id=$1 AND user_id=$2 RETURNING id,user_id,product_id,sell_enabled,buy_enabled,created_at,max_price_amount,min_quantity,preferred_city,required_attributes`, id, userID, req.MaxPriceAmount, req.MinQuantity, req.PreferredCity, attrs).Scan(&item.ID, &item.UserID, &item.ProductID, &item.SellEnabled, &item.BuyEnabled, &item.CreatedAt, &item.MaxPriceAmount, &item.MinQuantity, &item.PreferredCity, &item.RequiredAttributes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("interest not found or unauthorized")
	}
	if err != nil {
		return nil, fmt.Errorf("update interest: %w", err)
	}
	return &item, nil
}

func (r *InterestRepo) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	query := `DELETE FROM interests WHERE id = $1 AND user_id = $2`
	res, err := r.db.Pool.Exec(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("delete interest: %w", err)
	}
	if res.RowsAffected() == 0 {
		return errors.New("interest not found or unauthorized")
	}
	return nil
}

func (r *InterestRepo) ListByUserID(ctx context.Context, userID uuid.UUID) ([]domain.Interest, error) {
	query := `
		SELECT i.id, i.user_id, i.product_id, i.sell_enabled, i.buy_enabled, i.created_at, i.max_price_amount, i.min_quantity, i.preferred_city, i.required_attributes,
		       p.name, p.brand, p.model, p.category
		FROM interests i
		JOIN products p ON i.product_id = p.id
		WHERE i.user_id = $1
		ORDER BY i.created_at DESC
	`
	rows, err := r.db.Pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list interests: %w", err)
	}
	defer rows.Close()

	var list []domain.Interest
	for rows.Next() {
		var item domain.Interest
		var p domain.Product
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.ProductID, &item.SellEnabled, &item.BuyEnabled, &item.CreatedAt,
			&item.MaxPriceAmount, &item.MinQuantity, &item.PreferredCity, &item.RequiredAttributes, &p.Name, &p.Brand, &p.Model, &p.Category,
		); err != nil {
			return nil, fmt.Errorf("scan interest: %w", err)
		}
		p.ID = item.ProductID
		item.Product = &p
		list = append(list, item)
	}
	return list, nil
}

func (r *InterestRepo) FindInterestedUsers(ctx context.Context, productID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		SELECT user_id FROM interests 
		WHERE product_id = $1 AND buy_enabled = true
	`
	rows, err := r.db.Pool.Query(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("find interested users: %w", err)
	}
	defer rows.Close()

	var userIDs []uuid.UUID
	for rows.Next() {
		var u uuid.UUID
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("scan user id: %w", err)
		}
		userIDs = append(userIDs, u)
	}
	return userIDs, nil
}

func (r *InterestRepo) FindInterestsByProductID(ctx context.Context, productID uuid.UUID) ([]domain.Interest, error) {
	query := `
		SELECT id, user_id, product_id, sell_enabled, buy_enabled, created_at, max_price_amount, min_quantity, preferred_city, required_attributes
		FROM interests
		WHERE product_id = $1 AND buy_enabled = true
	`
	rows, err := r.db.Pool.Query(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("find interests by product: %w", err)
	}
	defer rows.Close()

	var interests []domain.Interest
	for rows.Next() {
		var item domain.Interest
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.ProductID, &item.SellEnabled, &item.BuyEnabled, &item.CreatedAt, &item.MaxPriceAmount, &item.MinQuantity, &item.PreferredCity, &item.RequiredAttributes,
		); err != nil {
			return nil, fmt.Errorf("scan interest: %w", err)
		}
		interests = append(interests, item)
	}
	return interests, nil
}
