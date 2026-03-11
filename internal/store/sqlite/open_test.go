package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenConfiguresPragmasAndSchemaVersion(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	var journalMode string
	if err := store.db.QueryRowContext(context.Background(), `pragma journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected WAL mode, got %q", journalMode)
	}

	var busyTimeout int
	if err := store.db.QueryRowContext(context.Background(), `pragma busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busyTimeout < 5000 {
		t.Fatalf("expected busy_timeout >= 5000, got %d", busyTimeout)
	}

	var schemaVersion int
	if err := store.db.QueryRowContext(context.Background(), `pragma user_version`).Scan(&schemaVersion); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if schemaVersion == 0 {
		t.Fatalf("expected schema version to be set")
	}

	if store.db.Stats().MaxOpenConnections != 1 {
		t.Fatalf("expected max open connections 1, got %d", store.db.Stats().MaxOpenConnections)
	}
}
