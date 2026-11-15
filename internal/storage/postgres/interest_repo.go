package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
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
		INSERT INTO interests (id, user_id, product_id, sell_enabled, buy_enabled)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, product_id) DO UPDATE SET buy_enabled = true
		RETURNING id, created_at
	`
	return r.db.Pool.QueryRow(ctx, query,
		interest.ID, interest.UserID, interest.ProductID, interest.SellEnabled, interest.BuyEnabled,
	).Scan(&interest.ID, &interest.CreatedAt)
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
		SELECT i.id, i.user_id, i.product_id, i.sell_enabled, i.buy_enabled, i.created_at,
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
			&p.Name, &p.Brand, &p.Model, &p.Category,
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
		SELECT id, user_id, product_id, sell_enabled, buy_enabled, created_at
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
			&item.ID, &item.UserID, &item.ProductID, &item.SellEnabled, &item.BuyEnabled, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan interest: %w", err)
		}
		interests = append(interests, item)
	}
	return interests, nil
}

// FindAssignedInterestsByProductID returns buyer interests whose source grant
// is live for the exact listing provenance.
func (r *InterestRepo) FindAssignedInterestsByProductID(ctx context.Context, productID, sourceID uuid.UUID) ([]domain.Interest, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT DISTINCT i.id,i.user_id,i.product_id,i.sell_enabled,i.buy_enabled,i.created_at FROM interests i JOIN source_assignments sa ON sa.buyer_id=i.user_id AND sa.source_id=$2 WHERE i.product_id=$1 AND i.buy_enabled=true`, productID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("find assigned interests: %w", err)
	}
	defer rows.Close()
	var interests []domain.Interest
	for rows.Next() {
		var item domain.Interest
		if err := rows.Scan(&item.ID, &item.UserID, &item.ProductID, &item.SellEnabled, &item.BuyEnabled, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan assigned interest: %w", err)
		}
		interests = append(interests, item)
	}
	return interests, rows.Err()
}
