package telegram_test

import (
	"context"
	gotd "github.com/gotd/td/telegram"
	"github.com/perso/perso-mind/internal/config"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/telegram"
	"testing"
)

func TestMissingTelegramCredentialsDoNotEnableMock(t *testing.T) {
	c, err := telegram.NewCollector(&config.Config{TelegramSessionDir: t.TempDir()}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.MockClient() != nil {
		t.Fatal("missing credentials silently enabled a mock")
	}
	if _, err = c.Resolver().ResolveSource(context.Background(), "@unverified_channel", ""); err == nil {
		t.Fatal("unconfigured account returned source metadata")
	}
	if err = c.Start(context.Background()); err == nil {
		t.Fatal("unconfigured collector started")
	}
}

func TestResolverUnavailableClientFailsClosed(t *testing.T) {
	r := telegram.NewResolver(nil, false)
	for _, address := range []string{"@unverified_channel", "-1001234567890", "https://t.me/+private_fixture"} {
		if result, err := r.ResolveSource(context.Background(), address, ""); err == nil {
			t.Errorf("%q: unavailable real client returned verified metadata: %+v", address, result)
		}
	}
}

func TestResolverNumericPeerCannotFallBackToMock(t *testing.T) {
	client := gotd.NewClient(1, "audit-fixture-not-a-credential", gotd.Options{})
	if result, err := telegram.NewResolver(client, false).ResolveSource(context.Background(), "-1001234567890", ""); err == nil {
		t.Fatalf("numeric peer silently returned synthetic metadata: %+v", result)
	}
}

func TestMockExpectedTypeDoesNotControlDetection(t *testing.T) {
	r := telegram.NewResolver(nil, true)
	result, err := r.ResolveSource(context.Background(), "@ordinary_channel", domain.TelegramSourceTypeForum)
	if err != nil {
		t.Fatal(err)
	}
	if result.DetectedType != domain.TelegramSourceTypeBroadcastChannel || !result.TypeMismatch {
		t.Fatalf("expected type controlled detected entity: %+v", result)
	}
}
