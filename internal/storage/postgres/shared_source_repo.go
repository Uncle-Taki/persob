package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/perso/perso-mind/internal/domain"
)

// ListWorkspace is for authorized administrators and the single gateway only.
// Buyer visibility is intentionally not implemented by this repository method.
func (r *TelegramSourceRepo) ListWorkspace(ctx context.Context) ([]*domain.TelegramSource, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT to_jsonb(s) FROM telegram_sources s WHERE workspace_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC,id`, domain.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []*domain.TelegramSource{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var s domain.TelegramSource
		if err = json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		sources = append(sources, &s)
	}
	return sources, rows.Err()
}

// SourceForPeer resolves exact provenance, never a parent's linked-discussion ID
// or a caller-supplied source ID. Unknown/removed peers remain restricted.
func (r *TelegramSourceRepo) SourceForPeer(ctx context.Context, peer int64) (*uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.Pool.QueryRow(ctx, `SELECT id FROM telegram_sources WHERE workspace_id=$1 AND telegram_peer_id=$2 AND deleted_at IS NULL`, domain.WorkspaceID, peer).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &id, err
}

var ErrSourceForbidden = errors.New("administrator access required")

// Soft removal preserves listing, grant-audit and historical provenance. Clearing
// grants and changing their version prevents stale admin forms restoring access.
func (r *TelegramSourceRepo) DeleteWorkspace(ctx context.Context, id uuid.UUID, actor uuid.UUID) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(344760767312)`); err != nil {
		return err
	}
	var role string
	var active bool
	err = tx.QueryRow(ctx, `SELECT role,active FROM users WHERE id=$1 FOR UPDATE`, actor).Scan(&role, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSourceForbidden
	}
	if err != nil {
		return err
	}
	if !active || (role != "admin" && role != "super_admin") {
		return ErrSourceForbidden
	}
	result, err := tx.Exec(ctx, `UPDATE telegram_sources SET deleted_at=NOW(),is_active=false,ingestion_status='paused',assignment_version=assignment_version+1,updated_at=NOW() WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL`, id, domain.WorkspaceID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("source not found")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs(actor_id,action,entity_type,entity_id,payload) SELECT $1,'source.assignments.clear_on_remove','telegram_source',$2,jsonb_build_object('buyer_ids',COALESCE(jsonb_agg(buyer_id),'[]'::jsonb)) FROM source_assignments WHERE source_id=$3`, actor.String(), id.String(), id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM source_assignments WHERE source_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// External enrichment follows the exact workspace source policy, independently
// of which administrator registered it. Unknown and linked-only peers fail closed.
func (r *TelegramSourceRepo) AllowWorkspaceExternalProcessing(ctx context.Context, peer int64) (bool, error) {
	var allowed bool
	err := r.db.Pool.QueryRow(ctx, `SELECT external_llm_allowed AND is_active AND ingestion_status='active' AND deleted_at IS NULL FROM telegram_sources WHERE workspace_id=$1 AND telegram_peer_id=$2`, domain.WorkspaceID, peer).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return allowed, err
}
