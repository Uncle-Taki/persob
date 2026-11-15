package postgres_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestCovAuditLogWritesRow(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	repo := postgres.NewAuditRepo(db)

	payload := map[string]any{"action": "create", "count": 2}
	if err := repo.Log(ctx, "actor-1", "login", "user", "u-123", payload, "10.0.0.1"); err != nil {
		t.Fatalf("log: %v", err)
	}

	var actor, action, entityType, entityID, ip string
	var raw []byte
	if err := db.Pool.QueryRow(ctx, `SELECT actor_id, action, entity_type, entity_id, payload, ip_address FROM audit_logs ORDER BY created_at DESC LIMIT 1`).
		Scan(&actor, &action, &entityType, &entityID, &raw, &ip); err != nil {
		t.Fatal(err)
	}
	if actor != "actor-1" || action != "login" || entityType != "user" || entityID != "u-123" || ip != "10.0.0.1" {
		t.Fatalf("audit fields mismatch: %q %q %q %q %q", actor, action, entityType, entityID, ip)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("payload not json: %v", err)
	}
	if got["action"] != "create" || got["count"] != float64(2) {
		t.Fatalf("payload mismatch: %v", got)
	}
}

func TestCovAuditLogNilPayload(t *testing.T) {
	db := covDB(t, true)
	repo := postgres.NewAuditRepo(db)
	// nil payload marshals to "null" -> stored as jsonb null.
	if err := repo.Log(context.Background(), "actor", "a", "e", "id", nil, ""); err != nil {
		t.Fatalf("log nil payload: %v", err)
	}
	var raw []byte
	if err := db.Pool.QueryRow(context.Background(), `SELECT payload FROM audit_logs`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) != "null" {
		t.Fatalf("expected json null payload, got %s", raw)
	}
}

func TestCovAuditLogUnmarshalablePayloadFallsBackToEmpty(t *testing.T) {
	db := covDB(t, true)
	repo := postgres.NewAuditRepo(db)
	// A channel cannot be marshaled: the repository must fall back to "{}".
	if err := repo.Log(context.Background(), "actor", "a", "e", "id", make(chan int), ""); err != nil {
		t.Fatalf("log unmarshalable payload: %v", err)
	}
	var raw []byte
	if err := db.Pool.QueryRow(context.Background(), `SELECT payload FROM audit_logs`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{}" {
		t.Fatalf("expected fallback empty object payload, got %s", raw)
	}
}

func TestCovAuditLogWriteError(t *testing.T) {
	db := covDB(t, true)
	if _, err := db.Pool.Exec(context.Background(), "DROP TABLE audit_logs CASCADE"); err != nil {
		t.Fatal(err)
	}
	err := postgres.NewAuditRepo(db).Log(context.Background(), "a", "b", "c", "d", nil, "")
	if err == nil {
		t.Fatal("expected write error after dropping audit_logs")
	}
}
