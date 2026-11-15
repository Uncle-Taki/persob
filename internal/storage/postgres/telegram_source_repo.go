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

type TelegramSourceRepo struct {
	db *DB
}

func NewTelegramSourceRepo(db *DB) *TelegramSourceRepo {
	return &TelegramSourceRepo{db: db}
}

func (r *TelegramSourceRepo) Create(ctx context.Context, s *domain.TelegramSource) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now

	capsJSON, err := json.Marshal(s.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}

	query := `
		INSERT INTO telegram_sources (
			id, owner_id, telegram_peer_id, telegram_access_hash, username, title,
			source_type, access_type, expected_type, detected_type, linked_discussion_peer_id,
			capabilities, is_active, ingestion_status, last_message_id, last_synced_at,
			created_at, updated_at,external_llm_allowed
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18,$19
		)
		ON CONFLICT (owner_id, telegram_peer_id) DO UPDATE SET
			external_llm_allowed=EXCLUDED.external_llm_allowed,telegram_access_hash=EXCLUDED.telegram_access_hash,title = EXCLUDED.title,
			username = EXCLUDED.username,
			source_type = EXCLUDED.source_type,
			access_type = EXCLUDED.access_type,
			detected_type = EXCLUDED.detected_type,
			linked_discussion_peer_id = EXCLUDED.linked_discussion_peer_id,
			capabilities = EXCLUDED.capabilities,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at
	`

	var expectedTypeStr *string
	if s.ExpectedType != "" {
		str := string(s.ExpectedType)
		expectedTypeStr = &str
	}

	err = r.db.Pool.QueryRow(ctx, query,
		s.ID, s.OwnerID, s.TelegramPeerID, s.TelegramAccessHash, s.Username, s.Title,
		string(s.SourceType), string(s.AccessType), expectedTypeStr, string(s.DetectedType), s.LinkedDiscussionPeerID,
		capsJSON, s.IsActive, string(s.IngestionStatus), s.LastMessageID, s.LastSyncedAt,
		s.CreatedAt, s.UpdatedAt, s.ExternalLLMAllowed,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)

	if err != nil {
		return fmt.Errorf("create telegram source: %w", err)
	}

	return nil
}

func (r *TelegramSourceRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.TelegramSource, error) {
	query := `
		SELECT 
			id, owner_id, telegram_peer_id, telegram_access_hash, username, title,
			source_type, access_type, expected_type, detected_type, linked_discussion_peer_id,
			capabilities, is_active, ingestion_status, last_message_id, last_synced_at,
			created_at, updated_at,external_llm_allowed
		FROM telegram_sources
		WHERE id = $1
	`
	var s domain.TelegramSource
	var capsJSON []byte
	var expectedType *string

	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&s.ID, &s.OwnerID, &s.TelegramPeerID, &s.TelegramAccessHash, &s.Username, &s.Title,
		&s.SourceType, &s.AccessType, &expectedType, &s.DetectedType, &s.LinkedDiscussionPeerID,
		&capsJSON, &s.IsActive, &s.IngestionStatus, &s.LastMessageID, &s.LastSyncedAt,
		&s.CreatedAt, &s.UpdatedAt, &s.ExternalLLMAllowed,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("source not found")
		}
		return nil, fmt.Errorf("get telegram source by id: %w", err)
	}

	if expectedType != nil {
		s.ExpectedType = domain.TelegramSourceType(*expectedType)
	}
	if len(capsJSON) > 0 {
		_ = json.Unmarshal(capsJSON, &s.Capabilities)
	}

	return &s, nil
}

func (r *TelegramSourceRepo) GetByPeerID(ctx context.Context, ownerID uuid.UUID, peerID int64) (*domain.TelegramSource, error) {
	query := `
		SELECT 
			id, owner_id, telegram_peer_id, telegram_access_hash, username, title,
			source_type, access_type, expected_type, detected_type, linked_discussion_peer_id,
			capabilities, is_active, ingestion_status, last_message_id, last_synced_at,
			created_at, updated_at,external_llm_allowed
		FROM telegram_sources
		WHERE owner_id = $1 AND telegram_peer_id = $2
	`
	var s domain.TelegramSource
	var capsJSON []byte
	var expectedType *string

	err := r.db.Pool.QueryRow(ctx, query, ownerID, peerID).Scan(
		&s.ID, &s.OwnerID, &s.TelegramPeerID, &s.TelegramAccessHash, &s.Username, &s.Title,
		&s.SourceType, &s.AccessType, &expectedType, &s.DetectedType, &s.LinkedDiscussionPeerID,
		&capsJSON, &s.IsActive, &s.IngestionStatus, &s.LastMessageID, &s.LastSyncedAt,
		&s.CreatedAt, &s.UpdatedAt, &s.ExternalLLMAllowed,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("source not found")
		}
		return nil, fmt.Errorf("get telegram source by peer: %w", err)
	}

	if expectedType != nil {
		s.ExpectedType = domain.TelegramSourceType(*expectedType)
	}
	if len(capsJSON) > 0 {
		_ = json.Unmarshal(capsJSON, &s.Capabilities)
	}

	return &s, nil
}

func (r *TelegramSourceRepo) ListByOwner(ctx context.Context, ownerID uuid.UUID) ([]*domain.TelegramSource, error) {
	query := `
		SELECT 
			id, owner_id, telegram_peer_id, telegram_access_hash, username, title,
			source_type, access_type, expected_type, detected_type, linked_discussion_peer_id,
			capabilities, is_active, ingestion_status, last_message_id, last_synced_at,
			created_at, updated_at,external_llm_allowed
		FROM telegram_sources
		WHERE owner_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Pool.Query(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list telegram sources: %w", err)
	}
	defer rows.Close()

	var sources []*domain.TelegramSource
	for rows.Next() {
		var s domain.TelegramSource
		var capsJSON []byte
		var expectedType *string

		if err := rows.Scan(
			&s.ID, &s.OwnerID, &s.TelegramPeerID, &s.TelegramAccessHash, &s.Username, &s.Title,
			&s.SourceType, &s.AccessType, &expectedType, &s.DetectedType, &s.LinkedDiscussionPeerID,
			&capsJSON, &s.IsActive, &s.IngestionStatus, &s.LastMessageID, &s.LastSyncedAt,
			&s.CreatedAt, &s.UpdatedAt, &s.ExternalLLMAllowed,
		); err != nil {
			return nil, fmt.Errorf("scan telegram source: %w", err)
		}

		if expectedType != nil {
			s.ExpectedType = domain.TelegramSourceType(*expectedType)
		}
		if len(capsJSON) > 0 {
			_ = json.Unmarshal(capsJSON, &s.Capabilities)
		}
		sources = append(sources, &s)
	}

	return sources, nil
}

func (r *TelegramSourceRepo) ListActiveSources(ctx context.Context) ([]*domain.TelegramSource, error) {
	query := `
		SELECT 
			id, owner_id, telegram_peer_id, telegram_access_hash, username, title,
			source_type, access_type, expected_type, detected_type, linked_discussion_peer_id,
			capabilities, is_active, ingestion_status, last_message_id, last_synced_at,
			created_at, updated_at,external_llm_allowed
		FROM telegram_sources
		WHERE is_active = true AND ingestion_status = 'active'
		ORDER BY created_at ASC
	`
	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list active telegram sources: %w", err)
	}
	defer rows.Close()

	var sources []*domain.TelegramSource
	for rows.Next() {
		var s domain.TelegramSource
		var capsJSON []byte
		var expectedType *string

		if err := rows.Scan(
			&s.ID, &s.OwnerID, &s.TelegramPeerID, &s.TelegramAccessHash, &s.Username, &s.Title,
			&s.SourceType, &s.AccessType, &expectedType, &s.DetectedType, &s.LinkedDiscussionPeerID,
			&capsJSON, &s.IsActive, &s.IngestionStatus, &s.LastMessageID, &s.LastSyncedAt,
			&s.CreatedAt, &s.UpdatedAt, &s.ExternalLLMAllowed,
		); err != nil {
			return nil, fmt.Errorf("scan active telegram source: %w", err)
		}

		if expectedType != nil {
			s.ExpectedType = domain.TelegramSourceType(*expectedType)
		}
		if len(capsJSON) > 0 {
			_ = json.Unmarshal(capsJSON, &s.Capabilities)
		}
		sources = append(sources, &s)
	}

	return sources, nil
}

func (r *TelegramSourceRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.IngestionStatus, isActive *bool) error {
	query := `
		UPDATE telegram_sources
		SET ingestion_status = $2,
		    is_active = COALESCE($3, is_active),
		    updated_at = NOW()
		WHERE id = $1
	`
	cmd, err := r.db.Pool.Exec(ctx, query, id, string(status), isActive)
	if err != nil {
		return fmt.Errorf("update telegram source status: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("source not found")
	}
	return nil
}

func (r *TelegramSourceRepo) UpdateSyncCheckpoint(ctx context.Context, id uuid.UUID, lastMsgID int64, syncedAt time.Time) error {
	query := `
		UPDATE telegram_sources
		SET last_message_id = GREATEST(last_message_id, $2),
		    last_synced_at = $3,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.Pool.Exec(ctx, query, id, lastMsgID, syncedAt)
	if err != nil {
		return fmt.Errorf("update sync checkpoint: %w", err)
	}
	return nil
}

func (r *TelegramSourceRepo) Delete(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) error {
	query := `DELETE FROM telegram_sources WHERE id = $1 AND owner_id = $2`
	cmd, err := r.db.Pool.Exec(ctx, query, id, ownerID)
	if err != nil {
		return fmt.Errorf("delete telegram source: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("source not found")
	}
	return nil
}

// PeerMonitoringStatus distinguishes a paused normalized source from no registry
// entry, so the legacy allowlist cannot reactivate a paused normalized source.
func (r *TelegramSourceRepo) PeerMonitoringStatus(ctx context.Context, peerID int64) (configured, active bool, err error) {
	err = r.db.Pool.QueryRow(ctx, `SELECT COUNT(*) > 0, COALESCE(BOOL_OR(is_active AND ingestion_status = 'active'), false)
 FROM telegram_sources WHERE telegram_peer_id=$1 OR linked_discussion_peer_id=$1`, peerID).Scan(&configured, &active)
	return
}

func (r *TelegramSourceRepo) HistoryCheckpoint(ctx context.Context, id uuid.UUID) (int64, error) {
	var n int64
	err := r.db.Pool.QueryRow(ctx, `SELECT history_message_id FROM telegram_sources WHERE id=$1`, id).Scan(&n)
	return n, err
}
func (r *TelegramSourceRepo) CompleteHistoryScan(ctx context.Context, id uuid.UUID, last int64) error {
	_, err := r.db.Pool.Exec(ctx, `UPDATE telegram_sources SET history_message_id=GREATEST(history_message_id,$2),last_message_id=GREATEST(last_message_id,$2),last_synced_at=NOW() WHERE id=$1`, id, last)
	return err
}

func (r *TelegramSourceRepo) AllowExternalProcessing(ctx context.Context, id string, owner uuid.UUID) (bool, error) {
	key, err := uuid.Parse(id)
	if err != nil {
		return false, nil
	}
	var allowed bool
	err = r.db.Pool.QueryRow(ctx, `SELECT external_llm_allowed AND is_active AND ingestion_status='active' FROM telegram_sources WHERE id=$1 AND owner_id=$2`, key, owner).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return allowed, err
}
func (r *TelegramSourceRepo) ReserveLLMRequest(ctx context.Context, limit int) (bool, error) {
	var count int
	err := r.db.Pool.QueryRow(ctx, `INSERT INTO llm_usage_buckets(minute,requests)VALUES(date_trunc('minute',NOW()),1) ON CONFLICT(minute) DO UPDATE SET requests=llm_usage_buckets.requests+1 WHERE llm_usage_buckets.requests<$1 RETURNING requests`, limit).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return count > 0, err
}
