package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type AuditRepo struct {
	db *DB
}

func NewAuditRepo(db *DB) *AuditRepo {
	return &AuditRepo{db: db}
}

func (r *AuditRepo) Log(ctx context.Context, actorID, action, entityType, entityID string, payload any, ipAddress string) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON = []byte("{}")
	}

	query := `
		INSERT INTO audit_logs (id, actor_id, action, entity_type, entity_id, payload, ip_address)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = r.db.Pool.Exec(ctx, query, uuid.New(), actorID, action, entityType, entityID, payloadJSON, ipAddress)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}
