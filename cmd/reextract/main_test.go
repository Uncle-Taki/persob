package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckpointRoundTripIsPrivateAndAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "checkpoint.json")
	want := checkpoint{ReceivedAt: time.Date(2026, 11, 18, 10, 11, 12, 0, time.UTC), TelegramChatID: -100, TelegramMessage: 7}
	if err := saveCheckpoint(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("checkpoint = %#v, want %#v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("checkpoint permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestMissingCheckpointStartsFromBeginning(t *testing.T) {
	cp, err := loadCheckpoint(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !cp.ReceivedAt.IsZero() || cp.TelegramChatID != 0 || cp.TelegramMessage != 0 {
		t.Fatalf("missing checkpoint = %#v", cp)
	}
}
