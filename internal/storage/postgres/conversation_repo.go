package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/perso/perso-mind/internal/domain"
)

type ConversationRepo struct {
	db *DB
}

func NewConversationRepo(db *DB) *ConversationRepo {
	return &ConversationRepo{db: db}
}

func (r *ConversationRepo) Create(ctx context.Context, c *domain.Conversation) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.State == "" {
		c.State = domain.ConvStateNew
	}
	if c.MaxQuestions <= 0 {
		c.MaxQuestions = 3
	}

	missingJSON, err := json.Marshal(c.MissingFields)
	if err != nil {
		missingJSON = []byte("[]")
	}
	collectedJSON, err := json.Marshal(c.CollectedData)
	if err != nil {
		collectedJSON = []byte("{}")
	}

	query := `
		INSERT INTO conversations (
			id, offer_id, seller_id, product_id, state, consent_granted,
			approved_by_user_id, approved_at, questions_asked_count, max_questions,
			missing_fields, collected_data, notes
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13
		)
		RETURNING created_at, updated_at
	`
	return r.db.Pool.QueryRow(ctx, query,
		c.ID, c.OfferID, c.SellerID, c.ProductID, c.State, c.ConsentGranted,
		c.ApprovedByUserID, c.ApprovedAt, c.QuestionsAskedCount, c.MaxQuestions,
		missingJSON, collectedJSON, c.Notes,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
}

func (r *ConversationRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Conversation, error) {
	query := `
		SELECT c.id, c.offer_id, c.seller_id, c.product_id, c.state, c.consent_granted,
		       c.approved_by_user_id, c.approved_at, c.questions_asked_count, c.max_questions,
		       c.missing_fields, c.collected_data, c.notes, c.created_at, c.updated_at,
		       p.name, p.category
		FROM conversations c
		LEFT JOIN products p ON c.product_id = p.id
		WHERE c.id = $1
	`
	var c domain.Conversation
	var missingBytes, collectedBytes []byte
	var pName, pCat *string

	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&c.ID, &c.OfferID, &c.SellerID, &c.ProductID, &c.State, &c.ConsentGranted,
		&c.ApprovedByUserID, &c.ApprovedAt, &c.QuestionsAskedCount, &c.MaxQuestions,
		&missingBytes, &collectedBytes, &c.Notes, &c.CreatedAt, &c.UpdatedAt,
		&pName, &pCat,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	_ = json.Unmarshal(missingBytes, &c.MissingFields)
	_ = json.Unmarshal(collectedBytes, &c.CollectedData)

	if c.ProductID != nil && pName != nil {
		c.Product = &domain.Product{
			ID:       *c.ProductID,
			Name:     *pName,
			Category: *pCat,
		}
	}

	return &c, nil
}

func (r *ConversationRepo) GetByOfferID(ctx context.Context, offerID uuid.UUID) (*domain.Conversation, error) {
	query := `
		SELECT id FROM conversations WHERE offer_id = $1 ORDER BY created_at DESC LIMIT 1
	`
	var id uuid.UUID
	err := r.db.Pool.QueryRow(ctx, query, offerID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.GetByID(ctx, id)
}

func (r *ConversationRepo) UpdateState(ctx context.Context, id uuid.UUID, state domain.ConversationState, notes string) error {
	query := `
		UPDATE conversations 
		SET state = $1, notes = COALESCE(NULLIF($2, ''), notes), updated_at = NOW() 
		WHERE id = $3
	`
	_, err := r.db.Pool.Exec(ctx, query, state, notes, id)
	return err
}

func (r *ConversationRepo) Approve(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	now := time.Now()
	query := `
		UPDATE conversations 
		SET state = $1, approved_by_user_id = $2, approved_at = $3, updated_at = NOW() 
		WHERE id = $4
	`
	_, err := r.db.Pool.Exec(ctx, query, domain.ConvStateActive, userID, now, id)
	return err
}

func (r *ConversationRepo) IncrementQuestions(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE conversations 
		SET questions_asked_count = questions_asked_count + 1, updated_at = NOW() 
		WHERE id = $1
	`
	_, err := r.db.Pool.Exec(ctx, query, id)
	return err
}

func (r *ConversationRepo) UpdateCollectedData(ctx context.Context, id uuid.UUID, collected map[string]any, remaining []domain.MissingField) error {
	cBytes, _ := json.Marshal(collected)
	mBytes, _ := json.Marshal(remaining)
	query := `
		UPDATE conversations 
		SET collected_data = $1, missing_fields = $2, updated_at = NOW() 
		WHERE id = $3
	`
	_, err := r.db.Pool.Exec(ctx, query, cBytes, mBytes, id)
	return err
}

func (r *ConversationRepo) List(ctx context.Context, state string, limit, offset int) ([]domain.Conversation, int64, error) {
	where := "WHERE 1=1"
	args := []any{}
	pIdx := 1
	if state != "" {
		where += fmt.Sprintf(" AND c.state = $%d", pIdx)
		args = append(args, state)
		pIdx++
	}

	var count int64
	countQ := "SELECT COUNT(*) FROM conversations c " + where
	if err := r.db.Pool.QueryRow(ctx, countQ, args...).Scan(&count); err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 20
	}

	query := fmt.Sprintf(`
		SELECT c.id, c.offer_id, c.seller_id, c.product_id, c.state, c.consent_granted,
		       c.approved_by_user_id, c.approved_at, c.questions_asked_count, c.max_questions,
		       c.missing_fields, c.collected_data, c.notes, c.created_at, c.updated_at,
		       p.name, p.category
		FROM conversations c
		LEFT JOIN products p ON c.product_id = p.id
		%s
		ORDER BY c.created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, pIdx, pIdx+1)

	args = append(args, limit, offset)

	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []domain.Conversation
	for rows.Next() {
		var c domain.Conversation
		var missingBytes, collectedBytes []byte
		var pName, pCat *string

		if err := rows.Scan(
			&c.ID, &c.OfferID, &c.SellerID, &c.ProductID, &c.State, &c.ConsentGranted,
			&c.ApprovedByUserID, &c.ApprovedAt, &c.QuestionsAskedCount, &c.MaxQuestions,
			&missingBytes, &collectedBytes, &c.Notes, &c.CreatedAt, &c.UpdatedAt,
			&pName, &pCat,
		); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(missingBytes, &c.MissingFields)
		_ = json.Unmarshal(collectedBytes, &c.CollectedData)
		if c.ProductID != nil && pName != nil {
			c.Product = &domain.Product{
				ID:       *c.ProductID,
				Name:     *pName,
				Category: *pCat,
			}
		}
		list = append(list, c)
	}
	return list, count, nil
}
