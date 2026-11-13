package telegram_test

import (
	"context"
	"testing"

	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/telegram"
)

func TestNormalizeAddress(t *testing.T) {
	resolver := telegram.NewResolver(nil, true)

	tests := []struct {
		input       string
		expected    string
		expectedTyp telegram.AddressType
	}{
		{"@iran_tech_bazaar", "iran_tech_bazaar", telegram.AddressTypeUsername},
		{"iran_tech_bazaar", "iran_tech_bazaar", telegram.AddressTypeUsername},
		{"https://t.me/iran_tech_bazaar", "iran_tech_bazaar", telegram.AddressTypeUsername},
		{"http://t.me/iran_tech_bazaar/", "iran_tech_bazaar", telegram.AddressTypeUsername},
		{"https://t.me/+AbCdEfGhIjK", "AbCdEfGhIjK", telegram.AddressTypeInviteLink},
		{"https://t.me/joinchat/AbCdEfGhIjK", "AbCdEfGhIjK", telegram.AddressTypeInviteLink},
		{"+AbCdEfGhIjK", "AbCdEfGhIjK", telegram.AddressTypeInviteLink},
		{"-1001234567890", "-1001234567890", telegram.AddressTypePeerID},
	}

	for _, tt := range tests {
		normalized, addrType := resolver.NormalizeAddress(tt.input)
		if normalized != tt.expected {
			t.Errorf("NormalizeAddress(%s) normalized = %s, want %s", tt.input, normalized, tt.expected)
		}
		if addrType != tt.expectedTyp {
			t.Errorf("NormalizeAddress(%s) type = %s, want %s", tt.input, addrType, tt.expectedTyp)
		}
	}
}

func TestResolveSourceClassification(t *testing.T) {
	resolver := telegram.NewResolver(nil, true)
	ctx := context.Background()

	// 1. Broadcast Channel with Discussion
	resChannel, err := resolver.ResolveSource(ctx, "https://t.me/iran_tech_bazaar", domain.TelegramSourceTypeBroadcastChannel)
	if err != nil {
		t.Fatalf("ResolveSource channel error: %v", err)
	}
	if resChannel.DetectedType != domain.TelegramSourceTypeBroadcastChannel {
		t.Errorf("Channel DetectedType = %s, want broadcast_channel", resChannel.DetectedType)
	}
	if !resChannel.Capabilities.HasDiscussion {
		t.Errorf("Expected Channel to report HasDiscussion = true")
	}
	if resChannel.LinkedDiscussionPeerID == nil {
		t.Errorf("Expected LinkedDiscussionPeerID to be populated")
	}
	if resChannel.Capabilities.SenderMetadataAvailable {
		t.Errorf("Broadcast channel must NOT have sender metadata available")
	}
	if resChannel.TypeMismatch {
		t.Errorf("Expected TypeMismatch = false when expected type matches")
	}

	// 2. Forum (Topic-enabled)
	resForum, err := resolver.ResolveSource(ctx, "https://t.me/iran_tech_forum", domain.TelegramSourceTypeForum)
	if err != nil {
		t.Fatalf("ResolveSource forum error: %v", err)
	}
	if resForum.DetectedType != domain.TelegramSourceTypeForum {
		t.Errorf("Forum DetectedType = %s, want forum", resForum.DetectedType)
	}
	if !resForum.Capabilities.HasTopics {
		t.Errorf("Forum must report HasTopics = true")
	}
	if !resForum.Capabilities.SenderMetadataAvailable {
		t.Errorf("Forum must report SenderMetadataAvailable = true")
	}

	// 3. Supergroup
	resSuper, err := resolver.ResolveSource(ctx, "https://t.me/iran_tech_supergroup", domain.TelegramSourceTypeSupergroup)
	if err != nil {
		t.Fatalf("ResolveSource supergroup error: %v", err)
	}
	if resSuper.DetectedType != domain.TelegramSourceTypeSupergroup {
		t.Errorf("Supergroup DetectedType = %s, want supergroup", resSuper.DetectedType)
	}
	if resSuper.Capabilities.HasTopics {
		t.Errorf("Regular supergroup must not have topics")
	}

	// 4. Basic Group
	resBasic, err := resolver.ResolveSource(ctx, "group_small_deals", domain.TelegramSourceTypeBasicGroup)
	if err != nil {
		t.Fatalf("ResolveSource basic group error: %v", err)
	}
	if resBasic.DetectedType != domain.TelegramSourceTypeBasicGroup {
		t.Errorf("Basic Group DetectedType = %s, want basic_group", resBasic.DetectedType)
	}
	if resBasic.AccessType != domain.AccessTypePrivate {
		t.Errorf("Basic Group must be private access type")
	}

	// 5. Type Mismatch Detection
	resMismatch, err := resolver.ResolveSource(ctx, "https://t.me/iran_tech_forum", domain.TelegramSourceTypeBroadcastChannel)
	if err != nil {
		t.Fatalf("ResolveSource mismatch error: %v", err)
	}
	if !resMismatch.TypeMismatch {
		t.Errorf("Expected TypeMismatch = true when expected channel but forum was detected")
	}
}
