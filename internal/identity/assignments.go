package identity

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/perso/perso-mind/internal/api/httputil"
	"github.com/perso/perso-mind/internal/domain"
)

type SourceAssignments struct {
	SourceID               uuid.UUID   `json:"source_id"`
	WorkspaceID            uuid.UUID   `json:"workspace_id"`
	BuyerIDs               []uuid.UUID `json:"buyer_ids"`
	Version                int64       `json:"version"`
	LinkedPeerPolicy       string      `json:"linked_peer_policy"`
	LinkedDiscussionPeerID *int64      `json:"linked_discussion_peer_id,omitempty"`
}

// SourceAssignments atomically replaces a source's explicit grants. It shares
// the identity administration lock, so actor/target status changes cannot race a
// grant write. Row locking also serializes removal and optimistic form versions.
func (s *Service) SourceAssignments(w http.ResponseWriter, r *http.Request) {
	actor := Current(r.Context())
	if actor == nil || !admin(actor.Role) {
		httputil.RespondError(w, 403, "Administrator access required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.RespondError(w, 400, "Invalid source ID")
		return
	}
	var req struct {
		BuyerIDs *[]uuid.UUID `json:"buyer_ids"`
		Version  *int64       `json:"version"`
	}
	if r.Method == http.MethodPut {
		if !decode(w, r, &req) {
			return
		}
		if req.BuyerIDs == nil || req.Version == nil || *req.Version < 0 || len(*req.BuyerIDs) > 200 {
			httputil.RespondError(w, 400, "buyer_ids (at most 200) and version are required")
			return
		}
		seen := map[uuid.UUID]bool{}
		for _, u := range *req.BuyerIDs {
			if u == uuid.Nil || seen[u] {
				httputil.RespondError(w, 400, "buyer_ids must be unique valid IDs")
				return
			}
			seen[u] = true
		}
	}
	tx, err := s.lock(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	a, err := actorInTx(r.Context(), tx, actor.ID)
	if err != nil {
		respondError(w, err)
		return
	}
	if !a.Active || !admin(a.Role) {
		httputil.RespondError(w, 403, "Administrator access required")
		return
	}
	result := SourceAssignments{SourceID: id, WorkspaceID: domain.WorkspaceID, BuyerIDs: []uuid.UUID{}, LinkedPeerPolicy: "explicit"}
	err = tx.QueryRow(r.Context(), `SELECT assignment_version,linked_discussion_peer_id FROM telegram_sources WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, domain.WorkspaceID).Scan(&result.Version, &result.LinkedDiscussionPeerID)
	if errors.Is(err, pgx.ErrNoRows) {
		httputil.RespondError(w, 404, "Source not found")
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if r.Method == http.MethodPut {
		if *req.Version != result.Version {
			httputil.RespondError(w, 409, "Assignments changed; reload before saving")
			return
		}
		var valid int
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE id=ANY($1::uuid[]) AND role='buyer' AND active`, *req.BuyerIDs).Scan(&valid)
		if err != nil {
			respondError(w, err)
			return
		}
		if valid != len(*req.BuyerIDs) {
			httputil.RespondError(w, 400, "Assign only active buyer accounts")
			return
		}
		if _, err = tx.Exec(r.Context(), `DELETE FROM source_assignments WHERE source_id=$1`, id); err != nil {
			respondError(w, err)
			return
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO source_assignments(source_id,buyer_id,assigned_by) SELECT $1,unnest($2::uuid[]),$3`, id, *req.BuyerIDs, a.ID); err != nil {
			respondError(w, err)
			return
		}
		if err = tx.QueryRow(r.Context(), `UPDATE telegram_sources SET assignment_version=assignment_version+1,updated_at=NOW() WHERE id=$1 RETURNING assignment_version`, id).Scan(&result.Version); err != nil {
			respondError(w, err)
			return
		}
		payload, _ := json.Marshal(map[string]any{"buyer_ids": *req.BuyerIDs, "version": result.Version, "linked_peer_policy": "explicit"})
		if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(actor_id,action,entity_type,entity_id,payload) VALUES($1,'source.assignments.replace','telegram_source',$2,$3)`, a.ID.String(), id.String(), payload); err != nil {
			respondError(w, err)
			return
		}
	}
	rows, err := tx.Query(r.Context(), `SELECT buyer_id FROM source_assignments WHERE source_id=$1 ORDER BY buyer_id`, id)
	if err != nil {
		respondError(w, err)
		return
	}
	for rows.Next() {
		var u uuid.UUID
		if err = rows.Scan(&u); err != nil {
			rows.Close()
			respondError(w, err)
			return
		}
		result.BuyerIDs = append(result.BuyerIDs, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		respondError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		respondError(w, err)
		return
	}
	httputil.RespondJSON(w, 200, result)
}
