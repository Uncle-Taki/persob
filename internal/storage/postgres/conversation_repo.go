package postgres

import (
	"bytes"
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
	if c.DraftKey == "" && c.OfferID != nil {
		c.DraftKey = c.OfferID.String()
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
			missing_fields, collected_data, notes,owner_id,draft_key,origin_key
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13,$14,NULLIF($15,''),NULLIF($16,'')
		)
		ON CONFLICT DO NOTHING
 RETURNING id,created_at,updated_at
	`
	err = r.db.Pool.QueryRow(ctx, query,
		c.ID, c.OfferID, c.SellerID, c.ProductID, c.State, c.ConsentGranted,
		c.ApprovedByUserID, c.ApprovedAt, c.QuestionsAskedCount, c.MaxQuestions,
		missingJSON, collectedJSON, c.Notes, c.OwnerID, c.DraftKey, c.OriginKey,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.db.Pool.QueryRow(ctx, `SELECT id,created_at,updated_at FROM conversations WHERE draft_key=$1 OR origin_key=$2 ORDER BY created_at LIMIT 1`, c.DraftKey, c.OriginKey).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	}
	return err
}

func (r *ConversationRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Conversation, error) {
	query := `
		SELECT c.id, c.offer_id, c.seller_id, c.product_id, c.state, c.consent_granted,
		       c.approved_by_user_id, c.approved_at, c.questions_asked_count, c.max_questions,
		       c.missing_fields, c.collected_data, c.notes, c.created_at, c.updated_at,c.owner_id,c.version,COALESCE(c.draft_key,''),
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
		&missingBytes, &collectedBytes, &c.Notes, &c.CreatedAt, &c.UpdatedAt, &c.OwnerID, &c.Version, &c.DraftKey,
		&pName, &pCat,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	_ = json.Unmarshal(missingBytes, &c.MissingFields)
	decoder := json.NewDecoder(bytes.NewReader(collectedBytes))
	decoder.UseNumber()
	_ = decoder.Decode(&c.CollectedData)

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
	return r.ListOwned(ctx, state, limit, offset, nil)
}
func (r *ConversationRepo) ListOwned(ctx context.Context, state string, limit, offset int, owner *uuid.UUID) ([]domain.Conversation, int64, error) {
	where := "WHERE 1=1"
	args := []any{}
	pIdx := 1
	if owner != nil {
		where += " AND c.owner_id=$1"
		args = append(args, *owner)
		pIdx++
	}
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
		       c.missing_fields, c.collected_data, c.notes, c.created_at, c.updated_at,c.owner_id,c.version,COALESCE(c.draft_key,''),
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
			&missingBytes, &collectedBytes, &c.Notes, &c.CreatedAt, &c.UpdatedAt, &c.OwnerID, &c.Version, &c.DraftKey,
			&pName, &pCat,
		); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(missingBytes, &c.MissingFields)
		decoder := json.NewDecoder(bytes.NewReader(collectedBytes))
		decoder.UseNumber()
		_ = decoder.Decode(&c.CollectedData)
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

// ListVisibleToUser exposes only requests whose current source grant still
// includes the buyer. Owner IDs alone are historical attribution and cannot
// bypass a revoked assignment.
func (r *ConversationRepo) ListVisibleToUser(ctx context.Context, userID uuid.UUID, state string, limit, offset int) ([]domain.Conversation, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	stateClause := ""
	args := []any{userID}
	if state != "" {
		stateClause = " AND c.state=$2"
		args = append(args, state)
	}
	visible := `EXISTS (SELECT 1 FROM conversation_requests cr JOIN offers o ON o.id=cr.offer_id JOIN source_assignments sa ON sa.workspace_id=o.workspace_id AND sa.source_id=o.source_id WHERE cr.conversation_id=c.id AND cr.user_id=$1 AND sa.buyer_id=$1)`
	var total int64
	if err := r.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM conversations c WHERE "+visible+stateClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	idx := 2
	if state != "" {
		idx = 3
	}
	rows, err := r.db.Pool.Query(ctx, fmt.Sprintf("SELECT c.id FROM conversations c WHERE %s%s ORDER BY c.created_at DESC LIMIT $%d OFFSET $%d", visible, stateClause, idx, idx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []domain.Conversation
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		conversation, err := r.GetByID(ctx, id)
		if err != nil {
			return nil, 0, err
		}
		if conversation != nil {
			list = append(list, *conversation)
		}
	}
	return list, total, rows.Err()
}

// HasCurrentBuyerGrant rechecks buyer activity, interest and source assignment before approval.
func (r *ConversationRepo) HasCurrentBuyerGrant(ctx context.Context, conversationID uuid.UUID) (bool, error) {
	var allowed bool
	err := r.db.Pool.QueryRow(ctx, `
  SELECT EXISTS (
   SELECT 1 FROM conversations c
   JOIN conversation_requests cr ON cr.conversation_id=c.id AND cr.user_id=c.owner_id
   JOIN users u ON u.id=cr.user_id AND u.active=true AND u.role='buyer'
   JOIN offers o ON o.id=c.offer_id
   JOIN telegram_sources s ON s.id=o.source_id AND s.workspace_id=o.workspace_id AND s.is_active=true AND s.deleted_at IS NULL AND s.ingestion_status='active'
   JOIN source_assignments sa ON sa.workspace_id=o.workspace_id AND sa.source_id=o.source_id AND sa.buyer_id=cr.user_id
   JOIN interests i ON i.user_id=cr.user_id AND i.product_id=c.product_id AND i.buy_enabled=true
   WHERE c.id=$1
  )`, conversationID).Scan(&allowed)
	return allowed, err
}

func (r *ConversationRepo) GetByIDForUser(ctx context.Context, id, userID uuid.UUID) (*domain.Conversation, error) {
	var exists bool
	err := r.db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM conversation_requests cr JOIN offers o ON o.id=cr.offer_id JOIN source_assignments sa ON sa.workspace_id=o.workspace_id AND sa.source_id=o.source_id WHERE cr.conversation_id=$1 AND cr.user_id=$2 AND sa.buyer_id=$2)`, id, userID).Scan(&exists)
	if err != nil || !exists {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

// SaveProgress atomically advances state and queues its outgoing message. The
// expected version prevents concurrent replies/approvals from advancing twice.
func (r *ConversationRepo) SaveProgress(ctx context.Context, c *domain.Conversation, text string) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if inbound, ok := ctx.Value(inboundKey{}).(inboundEvent); ok {
		tag, err := tx.Exec(ctx, `INSERT INTO inbound_receipts(peer_id,message_id,conversation_id,text) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, inbound.peer, inbound.message, c.ID, inbound.text)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
	}
	data, _ := json.Marshal(c.CollectedData)
	fields, _ := json.Marshal(c.MissingFields)
	tag, err := tx.Exec(ctx, `UPDATE conversations SET state=$1::text,consent_granted=$2,approved_by_user_id=$3,approved_at=$4,questions_asked_count=$5,missing_fields=$6,collected_data=$7,notes=$8,draft_key=CASE WHEN $1::text IN('COMPLETED','DECLINED','OPTED_OUT') THEN NULL ELSE draft_key END,version=version+1,updated_at=NOW() WHERE id=$9 AND version=$10`, c.State, c.ConsentGranted, c.ApprovedByUserID, c.ApprovedAt, c.QuestionsAskedCount, fields, data, c.Notes, c.ID, c.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("conversation changed; reload and retry")
	}
	if text != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO outbound_messages(id,conversation_id,seller_id,text) VALUES($1,$2,$3,$4)`, uuid.New(), c.ID, c.SellerID, text); err != nil {
			return err
		}
	}
	if c.ConsentGranted && c.OfferID != nil {
		priceChanged := false
		// Only typed validated values enter these keys; raw answers remain in history.
		if value, ok := c.CollectedData["price_amount"]; ok {
			if number, ok := value.(json.Number); ok {
				parsed, err := number.Int64()
				if err != nil {
					return err
				}
				value = parsed
			}
			currency, _ := c.CollectedData["price_currency"].(string)
			if currency == "" {
				currency = string(domain.CurrencyToman)
			}
			var previous *int64
			var previousCurrency string
			if err = tx.QueryRow(ctx, `SELECT price_amount,price_currency FROM offers WHERE id=$1 FOR UPDATE`, *c.OfferID).Scan(&previous, &previousCurrency); err != nil {
				return err
			}
			amount, ok := value.(int64)
			if !ok {
				return fmt.Errorf("collected price must be an integer")
			}
			priceChanged = previous == nil || *previous != amount || previousCurrency != currency
			if _, err = tx.Exec(ctx, `UPDATE offers SET price_amount=$1,price_currency=$2,intake_updated_at=NOW(),updated_at=NOW() WHERE id=$3`, value, currency, *c.OfferID); err != nil {
				return err
			}
		}
		if priceChanged {
			if _, err = tx.Exec(ctx, `INSERT INTO price_observations(id,product_id,offer_id,price_amount,price_currency,observed_at,revision_hash) SELECT $1,product_id,id,price_amount,price_currency,NOW(),$3 FROM offers WHERE id=$2 AND product_id IS NOT NULL ON CONFLICT(offer_id,revision_hash) WHERE revision_hash IS NOT NULL DO NOTHING`, uuid.New(), *c.OfferID, "intake:"+c.ID.String()+":"+fmt.Sprint(c.Version)); err != nil {
				return err
			}
		}
		if value, ok := c.CollectedData["seller_city"]; ok {
			if _, err = tx.Exec(ctx, `UPDATE offers SET seller_city=$1,intake_updated_at=NOW(),updated_at=NOW() WHERE id=$2`, value, *c.OfferID); err != nil {
				return err
			}
		}
		if value, ok := c.CollectedData["seller_tehran_area"]; ok {
			if _, err = tx.Exec(ctx, `UPDATE offers SET seller_area=$1,intake_updated_at=NOW(),updated_at=NOW() WHERE id=$2`, value, *c.OfferID); err != nil {
				return err
			}
		}
	}
	if c.ConsentGranted && c.OfferID != nil {
		for key, column := range map[string]string{"condition": "condition", "manufacturer": "manufacturer", "branded": "branded"} {
			if value, ok := c.CollectedData[key]; ok {
				if _, err = tx.Exec(ctx, "UPDATE offers SET "+column+"=$1,intake_updated_at=NOW(),updated_at=NOW() WHERE id=$2", value, *c.OfferID); err != nil {
					return err
				}
			}
		}
		if _, ok := c.CollectedData["branded"]; ok {
			if _, err = tx.Exec(ctx, `UPDATE offers SET branded_known=true WHERE id=$1`, *c.OfferID); err != nil {
				return err
			}
		}
		for key, column := range map[string]string{"warranty_until": "warranty_until", "expiration_date": "expiration_date"} {
			if value, ok := c.CollectedData[key]; ok {
				var date *time.Time
				if raw, ok := value.(string); ok && raw != "" {
					parsed, parseErr := time.Parse("2006-01-02", raw)
					if parseErr != nil {
						return parseErr
					}
					date = &parsed
				}
				if _, err = tx.Exec(ctx, "UPDATE offers SET "+column+"=$1,intake_updated_at=NOW(),updated_at=NOW() WHERE id=$2", date, *c.OfferID); err != nil {
					return err
				}
				if key == "warranty_until" {
					if _, err = tx.Exec(ctx, `UPDATE offers SET warranty_known=true WHERE id=$1`, *c.OfferID); err != nil {
						return err
					}
				}
			}
		}
	}
	if c.State == domain.ConvStatePaused || c.State == domain.ConvStateCompleted || c.State == domain.ConvStateDeclined || c.State == domain.ConvStateOptedOut {
		if _, err = tx.Exec(ctx, `UPDATE outbound_messages SET status='cancelled' WHERE conversation_id=$1 AND status IN ('queued','failed')`, c.ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	c.Version++
	return nil
}
func (r *ConversationRepo) FindActiveBySeller(ctx context.Context, seller int64) (*domain.Conversation, error) {
	var id uuid.UUID
	err := r.db.Pool.QueryRow(ctx, `SELECT id FROM conversations WHERE seller_id=$1 AND state IN ('AWAITING_CONSENT','WAITING_FOR_REPLY','ACTIVE') ORDER BY updated_at DESC LIMIT 1`, seller).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}
func (r *ConversationRepo) QueueHistory(ctx context.Context, id uuid.UUID) ([]map[string]any, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT id::text,text,status,created_at,telegram_message_id,last_error,'assistant' AS sender FROM outbound_messages WHERE conversation_id=$1
 UNION ALL SELECT peer_id::text||':'||message_id::text,text,'received',processed_at,message_id,NULL,'seller' FROM inbound_receipts WHERE conversation_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var key string
		var text, status, sender string
		var created time.Time
		var mid *int64
		var last *string
		if err = rows.Scan(&key, &text, &status, &created, &mid, &last, &sender); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": key, "text": text, "status": status, "sent_at": created, "sender": sender, "delivered": status == "sent", "telegram_message_id": mid, "last_error": last})
	}
	return out, rows.Err()
}

type inboundKey struct{}
type inboundEvent struct {
	peer, message int64
	text          string
}

func WithInbound(ctx context.Context, peer, message int64, text string) context.Context {
	return context.WithValue(ctx, inboundKey{}, inboundEvent{peer, message, text})
}
