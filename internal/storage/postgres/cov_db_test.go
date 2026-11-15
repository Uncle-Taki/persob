package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/perso/perso-mind/internal/storage/postgres"
)

func TestCovConnectSuccess(t *testing.T) {
	db := covDB(t, false)
	dsn := dbPoolDSN(t, db)
	got, err := postgres.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer got.Close()
	if got.Pool == nil {
		t.Fatal("expected non-nil pool")
	}
	if err := got.Ping(context.Background()); err != nil {
		t.Fatalf("ping after connect: %v", err)
	}
	// Close on a populated DB must not panic.
	got.Close()
	got.Close()
}

func TestCovConnectParseError(t *testing.T) {
	_, err := postgres.Connect(context.Background(), "://totally-invalid")
	if err == nil {
		t.Fatal("expected error for invalid DSN")
	}
	if !strings.Contains(err.Error(), "parse postgres config") {
		t.Fatalf("expected parse error wrap, got: %v", err)
	}
}

func TestCovConnectUnreachableHost(t *testing.T) {
	_, err := postgres.Connect(context.Background(), "postgres://postgres@127.0.0.1:55999/perso_audit?sslmode=disable")
	if err == nil {
		t.Fatal("expected ping failure for unreachable host")
	}
	if !strings.Contains(err.Error(), "ping postgres") {
		t.Fatalf("expected ping error wrap, got: %v", err)
	}
}

func TestCovCloseNilPool(t *testing.T) {
	// Must not panic.
	(&postgres.DB{}).Close()
}

func TestCovPingErrorOnClosedPool(t *testing.T) {
	base := covDB(t, false)
	dsn := dbPoolDSN(t, base)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	pool.Close()
	db := &postgres.DB{Pool: pool}
	if err := db.Ping(context.Background()); err == nil {
		t.Fatal("expected ping error on closed pool")
	}
}

func TestCovMigrateFreshAndIdempotent(t *testing.T) {
	db := covDB(t, false)
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("fresh migrate: %v", err)
	}
	var n int
	if err := db.Pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 applied migrations, got %d", n)
	}
	// Re-run must be a no-op that succeeds.
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("re-run migrate: %v", err)
	}
	if err := db.Pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatalf("recount migrations: %v", err)
	}
	if n != 3 {
		t.Fatalf("re-run changed applied migration count to %d, want 3", n)
	}
	var hasOffers bool
	if err := db.Pool.QueryRow(context.Background(), "SELECT to_regclass('offers') IS NOT NULL").Scan(&hasOffers); err != nil {
		t.Fatal(err)
	}
	if !hasOffers {
		t.Fatal("offers table missing after migration")
	}
}

func TestCovMigrateChecksumChanged(t *testing.T) {
	db := covDB(t, true)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `UPDATE schema_migrations SET checksum='deadbeef' WHERE name='migrations/001_initial_schema.sql'`); err != nil {
		t.Fatal(err)
	}
	err := db.Migrate(ctx)
	if err == nil {
		t.Fatal("expected checksum change error")
	}
	if !strings.Contains(err.Error(), "migration checksum changed: migrations/001_initial_schema.sql") {
		t.Fatalf("unexpected error: %v", err)
	}
	// The failed transaction must be rolled back: the wrong checksum row stays,
	// but the schema must still be usable and tables intact.
	var ok bool
	if err := db.Pool.QueryRow(ctx, "SELECT to_regclass('offers') IS NOT NULL").Scan(&ok); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("offers table must survive a failed re-migration")
	}
}

func TestCovMigrateBeginError(t *testing.T) {
	base := covDB(t, false)
	dsn := dbPoolDSN(t, base)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	db := &postgres.DB{Pool: pool}
	if err := db.Migrate(context.Background()); err == nil {
		t.Fatal("expected begin error on closed pool")
	}
}

func TestCovMigrateCreateSchemaMigrationsError(t *testing.T) {
	db := covDB(t, false)
	// A pre-existing view blocks CREATE TABLE IF NOT EXISTS.
	if _, err := db.Pool.Exec(context.Background(), "CREATE VIEW schema_migrations AS SELECT 1::text AS name, 'x' AS checksum, NOW() AS applied_at"); err != nil {
		t.Fatal(err)
	}
	err := db.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected error creating schema_migrations over existing view")
	}
}

func TestCovMigrateChecksumQueryError(t *testing.T) {
	db := covDB(t, false)
	// Pre-create schema_migrations with an incompatible checksum column type:
	// SELECT succeeds textually but Scan into string fails.
	if _, err := db.Pool.Exec(context.Background(), "CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, checksum INTEGER NOT NULL, applied_at TIMESTAMPTZ NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(context.Background(), "INSERT INTO schema_migrations(name, checksum, applied_at) VALUES('migrations/001_initial_schema.sql', 1, NOW())"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err == nil {
		t.Fatal("expected checksum scan error")
	}
}

func TestCovMigrateBodyError(t *testing.T) {
	db := covDB(t, false)
	// A view named `users` makes the migration body's CREATE TABLE fail.
	if _, err := db.Pool.Exec(context.Background(), "CREATE VIEW users AS SELECT 1 AS x"); err != nil {
		t.Fatal(err)
	}
	err := db.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected migration body error")
	}
	if !strings.Contains(err.Error(), "migration migrations/001_initial_schema.sql") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Rollback must leave the schema_migrations table absent (fresh state).
	var ok bool
	if err := db.Pool.QueryRow(context.Background(), "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&ok); err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("schema_migrations must be rolled back after failed migration body")
	}
}

func TestCovMigrateRecordInsertError(t *testing.T) {
	db := covDB(t, false)
	// schema_migrations exists with a NOT NULL applied_at that has no default:
	// the checksum INSERT (which omits applied_at) must fail, and the whole
	// transaction (including already-run migration DDL) must roll back.
	if _, err := db.Pool.Exec(context.Background(), "CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	err := db.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected insert error into schema_migrations")
	}
	var offersOK bool
	if err := db.Pool.QueryRow(context.Background(), "SELECT to_regclass('offers') IS NOT NULL").Scan(&offersOK); err != nil {
		t.Fatal(err)
	}
	if offersOK {
		t.Fatal("migration DDL must be rolled back when the checksum insert fails")
	}
}

// dbPoolDSN derives a DSN that targets the same server/database as the given
// test pool. Tests that exercise postgres.Connect must hit the local endpoint.
func dbPoolDSN(t *testing.T, db *postgres.DB) string {
	t.Helper()
	raw := testDSN()
	// Reuse the search_path of the isolated schema so Connect succeeds against it.
	var sp string
	if err := db.Pool.QueryRow(context.Background(), "SELECT current_schema()").Scan(&sp); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	return raw + "&search_path=" + sp + ",public"
}
