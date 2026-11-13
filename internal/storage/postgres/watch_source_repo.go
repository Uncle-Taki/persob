package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
)

type WatchSourceRepo struct {
	db *DB
}

func NewWatchSourceRepo(db *DB) *WatchSourceRepo {
	return &WatchSourceRepo{db: db}
}

func (r *WatchSourceRepo) Create(ctx context.Context, s *domain.WatchSource) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	// Enforce Phase 1 invariants: Sell = false, Buy = true
	s.SellEnabled = false
	s.BuyEnabled = true
	if s.SourceType == "" {
		s.SourceType = domain.SourceTypeChannel
	}
	if s.MonitoringStatus == "" {
		s.MonitoringStatus = domain.MonitoringStatusActive
	}

	query := `
		INSERT INTO watch_sources (id, user_id, telegram_chat_id, username, title, source_type, sell_enabled, buy_enabled, monitoring_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id, telegram_chat_id) DO UPDATE 
		SET title = EXCLUDED.title, username = EXCLUDED.username, monitoring_status = 'active', buy_enabled = true
		RETURNING id, created_at
	`
	return r.db.Pool.QueryRow(ctx, query,
		s.ID, s.UserID, s.TelegramChatID, s.Username, s.Title, s.SourceType, s.SellEnabled, s.BuyEnabled, s.MonitoringStatus,
	).Scan(&s.ID, &s.CreatedAt)
}

func (r *WatchSourceRepo) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	query := `DELETE FROM watch_sources WHERE id = $1 AND user_id = $2`
	res, err := r.db.Pool.Exec(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("delete watch source: %w", err)
	}
	if res.RowsAffected() == 0 {
		return errors.New("watch source not found or unauthorized")
	}
	return nil
}

func (r *WatchSourceRepo) ListByUserID(ctx context.Context, userID uuid.UUID) ([]domain.WatchSource, error) {
	query := `
		SELECT id, user_id, telegram_chat_id, username, title, source_type, sell_enabled, buy_enabled, monitoring_status, created_at
		FROM watch_sources
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list watch sources: %w", err)
	}
	defer rows.Close()

	var list []domain.WatchSource
	for rows.Next() {
		var s domain.WatchSource
		var username *string
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.TelegramChatID, &username, &s.Title, &s.SourceType,
			&s.SellEnabled, &s.BuyEnabled, &s.MonitoringStatus, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan watch source: %w", err)
		}
		if username != nil {
			s.Username = *username
		}
		list = append(list, s)
	}
	return list, nil
}

func (r *WatchSourceRepo) ListAllActiveChatIDs(ctx context.Context) ([]int64, error) {
	query := `
		SELECT DISTINCT telegram_chat_id 
		FROM watch_sources 
		WHERE monitoring_status = 'active'
	`
	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list active chat ids: %w", err)
	}
	defer rows.Close()

	var chatIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan chat id: %w", err)
		}
		chatIDs = append(chatIDs, id)
	}
	return chatIDs, nil
}

func (r *WatchSourceRepo) IsChatMonitored(ctx context.Context, chatID int64) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM watch_sources 
			WHERE telegram_chat_id = $1 AND monitoring_status = 'active'
		)
	`
	var exists bool
	err := r.db.Pool.QueryRow(ctx, query, chatID).Scan(&exists)
	return exists, err
}

func (r *WatchSourceRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.MonitoringStatus) error {
	query := `UPDATE watch_sources SET monitoring_status = $1 WHERE id = $2`
	_, err := r.db.Pool.Exec(ctx, query, status, id)
	return err
}
