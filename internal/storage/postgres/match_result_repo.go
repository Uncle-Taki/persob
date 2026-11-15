package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/perso/perso-mind/internal/domain"
)

type MatchResultRepo struct {
	db *DB
}

func NewMatchResultRepo(db *DB) *MatchResultRepo {
	return &MatchResultRepo{db: db}
}

func (r *MatchResultRepo) Create(ctx context.Context, res *domain.OfferMatchResult) error {
	if res.ID == uuid.Nil {
		res.ID = uuid.New()
	}
	res.CreatedAt = time.Now().UTC()

	matchedJSON, err := json.Marshal(res.MatchedAttributes)
	if err != nil {
		return fmt.Errorf("marshal matched attributes: %w", err)
	}

	unmatchedJSON, err := json.Marshal(res.UnmatchedAttributes)
	if err != nil {
		return fmt.Errorf("marshal unmatched attributes: %w", err)
	}

	query := `
		INSERT INTO offer_match_results (
			id, offer_id, interest_id, user_id, product_id, match_score, match_explanation,
			matched_attributes, unmatched_attributes, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		) ON CONFLICT(offer_id,user_id) DO UPDATE SET
        interest_id=EXCLUDED.interest_id,product_id=EXCLUDED.product_id,
        match_score=EXCLUDED.match_score,match_explanation=EXCLUDED.match_explanation,
        matched_attributes=EXCLUDED.matched_attributes,unmatched_attributes=EXCLUDED.unmatched_attributes
        RETURNING id,created_at
	`
	err = r.db.Pool.QueryRow(ctx, query,
		res.ID, res.OfferID, res.InterestID, res.UserID, res.ProductID,
		res.MatchScore, res.MatchExplanation, matchedJSON, unmatchedJSON, res.CreatedAt,
	).Scan(&res.ID, &res.CreatedAt)
	if err != nil {
		return fmt.Errorf("create offer match result: %w", err)
	}

	return nil
}

func (r *MatchResultRepo) ListByOfferID(ctx context.Context, offerID uuid.UUID) ([]*domain.OfferMatchResult, error) {
	query := `
		SELECT m.id, m.offer_id, m.interest_id, m.user_id, m.product_id, m.match_score, m.match_explanation,
		       m.matched_attributes, m.unmatched_attributes, m.created_at
		FROM offer_match_results m
		WHERE m.offer_id = $1
		ORDER BY m.match_score DESC
	`
	rows, err := r.db.Pool.Query(ctx, query, offerID)
	if err != nil {
		return nil, fmt.Errorf("list match results by offer: %w", err)
	}
	defer rows.Close()

	var results []*domain.OfferMatchResult
	for rows.Next() {
		var item domain.OfferMatchResult
		var matchedJSON, unmatchedJSON []byte
		if err := rows.Scan(
			&item.ID, &item.OfferID, &item.InterestID, &item.UserID, &item.ProductID,
			&item.MatchScore, &item.MatchExplanation, &matchedJSON, &unmatchedJSON, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan match result: %w", err)
		}
		if len(matchedJSON) > 0 {
			_ = json.Unmarshal(matchedJSON, &item.MatchedAttributes)
		}
		if len(unmatchedJSON) > 0 {
			_ = json.Unmarshal(unmatchedJSON, &item.UnmatchedAttributes)
		}
		results = append(results, &item)
	}

	return results, nil
}

func (r *MatchResultRepo) ListByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.OfferMatchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT m.id, m.offer_id, m.interest_id, m.user_id, m.product_id, m.match_score, m.match_explanation,
		       m.matched_attributes, m.unmatched_attributes, m.created_at
		FROM offer_match_results m
		JOIN offers o ON o.id = m.offer_id
		JOIN source_assignments sa ON sa.workspace_id=o.workspace_id AND sa.source_id=o.source_id AND sa.buyer_id=m.user_id
		WHERE m.user_id = $1
		ORDER BY m.created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list match results by user: %w", err)
	}
	defer rows.Close()

	var results []*domain.OfferMatchResult
	for rows.Next() {
		var item domain.OfferMatchResult
		var matchedJSON, unmatchedJSON []byte
		if err := rows.Scan(
			&item.ID, &item.OfferID, &item.InterestID, &item.UserID, &item.ProductID,
			&item.MatchScore, &item.MatchExplanation, &matchedJSON, &unmatchedJSON, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan match result: %w", err)
		}
		if len(matchedJSON) > 0 {
			_ = json.Unmarshal(matchedJSON, &item.MatchedAttributes)
		}
		if len(unmatchedJSON) > 0 {
			_ = json.Unmarshal(unmatchedJSON, &item.UnmatchedAttributes)
		}
		results = append(results, &item)
	}

	return results, nil
}
