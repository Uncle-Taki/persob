package domain

import (
	"testing"
	"time"
)

func newRevisionMsg(chat, msgID int64, edited *time.Time, text string) *TelegramMessage {
	return &TelegramMessage{
		TelegramChatID:    chat,
		TelegramMessageID: msgID,
		EditedDate:        edited,
		MessageText:       text,
	}
}

func TestRevisionKey(t *testing.T) {
	edited := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	base := newRevisionMsg(100, 200, nil, "سلام")
	baseKey := RevisionKey(base)

	// Pinned digest of sha256("100:200:<nil>:سلام").
	const wantBase = "62bbe0372baa61a089071dceb8f2ba8b25c41447e70d208966dcf3e0a5252dd1"
	if baseKey != wantBase {
		t.Fatalf("RevisionKey(nil edited) = %s, want %s", baseKey, wantBase)
	}

	// Determinism: an identical message yields the identical key.
	baseCopy := newRevisionMsg(100, 200, nil, "سلام")
	if got := RevisionKey(baseCopy); got != baseKey {
		t.Errorf("RevisionKey is not deterministic: %s != %s", got, baseKey)
	}

	// A set EditedDate changes the key (and matches the pinned digest of
	// sha256("100:200:2026-01-02 03:04:05 +0000 UTC:سلام")).
	editedMsg := newRevisionMsg(100, 200, &edited, "سلام")
	editedKey := RevisionKey(editedMsg)
	const wantEdited = "350053ead22f1c8b8875b9445d15b069eb3f3bc20cb8b8e2e775640754f2a33f"
	if editedKey != wantEdited {
		t.Fatalf("RevisionKey(set edited) = %s, want %s", editedKey, wantEdited)
	}
	if editedKey == baseKey {
		t.Error("RevisionKey must differ between nil and set EditedDate")
	}

	// Changing any single field changes the key.
	if got := RevisionKey(newRevisionMsg(101, 200, nil, "سلام")); got == baseKey {
		t.Error("changing TelegramChatID did not change the key")
	}
	if got := RevisionKey(newRevisionMsg(100, 201, nil, "سلام")); got == baseKey {
		t.Error("changing TelegramMessageID did not change the key")
	}
	if got := RevisionKey(newRevisionMsg(100, 200, nil, "دولت")); got == baseKey {
		t.Error("changing MessageText did not change the key")
	}
}
