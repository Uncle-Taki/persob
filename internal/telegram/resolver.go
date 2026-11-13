package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/perso/perso-mind/internal/domain"
)

type AddressType string

const (
	AddressTypeUsername   AddressType = "username"
	AddressTypeInviteLink AddressType = "invite_link"
	AddressTypePeerID     AddressType = "peer_id"
)

type Resolver struct {
	client *telegram.Client
	isMock bool
}

func NewResolver(client *telegram.Client, isMock bool) *Resolver {
	return &Resolver{
		client: client,
		isMock: isMock,
	}
}

// NormalizeAddress normalizes any input Telegram address (URL, @username, invite link, numeric ID)
func (r *Resolver) NormalizeAddress(input string) (string, AddressType) {
	s := strings.TrimSpace(input)
	s = strings.TrimRight(s, "/")

	// Check if numeric peer ID
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return s, AddressTypePeerID
	}

	// Invite links: t.me/+hash, t.me/joinchat/hash, +hash
	if strings.Contains(s, "t.me/+") {
		parts := strings.Split(s, "t.me/+")
		if len(parts) > 1 {
			return parts[1], AddressTypeInviteLink
		}
	}
	if strings.Contains(s, "t.me/joinchat/") {
		parts := strings.Split(s, "t.me/joinchat/")
		if len(parts) > 1 {
			return parts[1], AddressTypeInviteLink
		}
	}
	if strings.HasPrefix(s, "+") {
		return strings.TrimPrefix(s, "+"), AddressTypeInviteLink
	}

	// Username URLs: t.me/username, telegram.me/username
	if strings.Contains(s, "t.me/") {
		parts := strings.Split(s, "t.me/")
		if len(parts) > 1 {
			u := strings.TrimPrefix(parts[1], "@")
			return u, AddressTypeUsername
		}
	}
	if strings.Contains(s, "telegram.me/") {
		parts := strings.Split(s, "telegram.me/")
		if len(parts) > 1 {
			u := strings.TrimPrefix(parts[1], "@")
			return u, AddressTypeUsername
		}
	}

	// Plain username with @ or without
	cleaned := strings.TrimPrefix(s, "@")
	return cleaned, AddressTypeUsername
}

// ResolveSource resolves a Telegram entity and determines its source type, access, and capabilities
func (r *Resolver) ResolveSource(ctx context.Context, rawAddress string, expectedType domain.TelegramSourceType) (*domain.ResolveSourceResponse, error) {
	normAddr, addrType := r.NormalizeAddress(rawAddress)

	if r.isMock || r.client == nil {
		return r.resolveMockSource(rawAddress, normAddr, addrType, expectedType)
	}

	api := r.client.API()

	switch addrType {
	case AddressTypeUsername:
		resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
			Username: normAddr,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve username '%s': %w", normAddr, err)
		}

		return r.inspectResolvedPeer(ctx, resolved, rawAddress, expectedType)

	case AddressTypeInviteLink:
		invite, err := api.MessagesCheckChatInvite(ctx, normAddr)
		if err != nil {
			return nil, fmt.Errorf("check chat invite '%s': %w", normAddr, err)
		}

		return r.inspectChatInvite(ctx, invite, rawAddress, expectedType)

	case AddressTypePeerID:
		// For numeric peer ID in MTProto, attempt fallback to mock or inspect
		return r.resolveMockSource(rawAddress, normAddr, addrType, expectedType)

	default:
		return nil, fmt.Errorf("unsupported address type: %s", addrType)
	}
}

func (r *Resolver) inspectResolvedPeer(ctx context.Context, resolved *tg.ContactsResolvedPeer, rawAddress string, expectedType domain.TelegramSourceType) (*domain.ResolveSourceResponse, error) {
	if len(resolved.Chats) == 0 {
		return nil, fmt.Errorf("resolved peer contains no chat or channel entities (might be a user)")
	}

	chatClass := resolved.Chats[0]
	switch c := chatClass.(type) {
	case *tg.Channel:
		var detectedType domain.TelegramSourceType
		if c.Broadcast {
			detectedType = domain.TelegramSourceTypeBroadcastChannel
		} else if c.Megagroup && c.Forum {
			detectedType = domain.TelegramSourceTypeForum
		} else {
			detectedType = domain.TelegramSourceTypeSupergroup
		}

		accessType := domain.AccessTypePublic
		if c.Username == "" {
			accessType = domain.AccessTypePrivate
		}

		accountHasAccess := !c.Left
		if accessType == domain.AccessTypePublic {
			accountHasAccess = true
		}

		var linkedDiscussionPeerID *int64
		hasDiscussion := false

		if c.HasLink {
			// Query full channel for linked discussion ID
			full, err := r.client.API().ChannelsGetFullChannel(ctx, &tg.InputChannel{
				ChannelID:  c.ID,
				AccessHash: c.AccessHash,
			})
			if err == nil {
				if fullChannel, ok := full.FullChat.(*tg.ChannelFull); ok {
					if linkedID, ok := fullChannel.GetLinkedChatID(); ok && linkedID != 0 {
						normalizedLinkedID := -1000000000000 - linkedID
						linkedDiscussionPeerID = &normalizedLinkedID
						hasDiscussion = true
					}
				}
			}
		}

		caps := domain.SourceCapabilities{
			CanReadHistory:          accountHasAccess,
			HasTopics:               detectedType == domain.TelegramSourceTypeForum,
			HasDiscussion:           hasDiscussion,
			SenderMetadataAvailable: detectedType != domain.TelegramSourceTypeBroadcastChannel,
		}

		peerID := -1000000000000 - c.ID
		typeMismatch := expectedType != "" && expectedType != detectedType

		return &domain.ResolveSourceResponse{
			Address:                rawAddress,
			PeerID:                 peerID,
			AccessHash:             c.AccessHash,
			Username:               c.Username,
			Title:                  c.Title,
			DetectedType:           detectedType,
			AccessType:             accessType,
			ExpectedType:           expectedType,
			TypeMismatch:           typeMismatch,
			LinkedDiscussionPeerID: linkedDiscussionPeerID,
			Capabilities:           caps,
			AccountHasAccess:       accountHasAccess,
			CanIngest:              accountHasAccess,
		}, nil

	case *tg.Chat:
		detectedType := domain.TelegramSourceTypeBasicGroup
		accessType := domain.AccessTypePrivate
		accountHasAccess := !c.Left && !c.Deactivated

		caps := domain.SourceCapabilities{
			CanReadHistory:          accountHasAccess,
			HasTopics:               false,
			HasDiscussion:           false,
			SenderMetadataAvailable: true,
		}

		peerID := -c.ID
		typeMismatch := expectedType != "" && expectedType != detectedType

		return &domain.ResolveSourceResponse{
			Address:                rawAddress,
			PeerID:                 peerID,
			Title:                  c.Title,
			DetectedType:           detectedType,
			AccessType:             accessType,
			ExpectedType:           expectedType,
			TypeMismatch:           typeMismatch,
			Capabilities:           caps,
			AccountHasAccess:       accountHasAccess,
			CanIngest:              accountHasAccess,
		}, nil

	default:
		return nil, fmt.Errorf("unexpected chat type: %T", chatClass)
	}
}

func (r *Resolver) inspectChatInvite(ctx context.Context, inviteClass tg.ChatInviteClass, rawAddress string, expectedType domain.TelegramSourceType) (*domain.ResolveSourceResponse, error) {
	switch inv := inviteClass.(type) {
	case *tg.ChatInviteAlready:
		// Account is already a member
		switch c := inv.Chat.(type) {
		case *tg.Channel:
			var detectedType domain.TelegramSourceType
			if c.Broadcast {
				detectedType = domain.TelegramSourceTypeBroadcastChannel
			} else if c.Megagroup && c.Forum {
				detectedType = domain.TelegramSourceTypeForum
			} else {
				detectedType = domain.TelegramSourceTypeSupergroup
			}

			peerID := -1000000000000 - c.ID
			caps := domain.SourceCapabilities{
				CanReadHistory:          true,
				HasTopics:               detectedType == domain.TelegramSourceTypeForum,
				HasDiscussion:           c.HasLink,
				SenderMetadataAvailable: detectedType != domain.TelegramSourceTypeBroadcastChannel,
			}

			return &domain.ResolveSourceResponse{
				Address:          rawAddress,
				PeerID:           peerID,
				AccessHash:       c.AccessHash,
				Username:         c.Username,
				Title:            c.Title,
				DetectedType:     detectedType,
				AccessType:       domain.AccessTypePrivate,
				ExpectedType:     expectedType,
				TypeMismatch:     expectedType != "" && expectedType != detectedType,
				Capabilities:     caps,
				AccountHasAccess: true,
				CanIngest:        true,
			}, nil

		case *tg.Chat:
			peerID := -c.ID
			caps := domain.SourceCapabilities{
				CanReadHistory:          true,
				HasTopics:               false,
				HasDiscussion:           false,
				SenderMetadataAvailable: true,
			}
			return &domain.ResolveSourceResponse{
				Address:          rawAddress,
				PeerID:           peerID,
				Title:            c.Title,
				DetectedType:     domain.TelegramSourceTypeBasicGroup,
				AccessType:       domain.AccessTypePrivate,
				ExpectedType:     expectedType,
				TypeMismatch:     expectedType != "" && expectedType != domain.TelegramSourceTypeBasicGroup,
				Capabilities:     caps,
				AccountHasAccess: true,
				CanIngest:        true,
			}, nil
		}

	case *tg.ChatInvite:
		// Account has NOT joined yet. We detect metadata, but do NOT automatically join.
		var detectedType domain.TelegramSourceType
		if inv.Broadcast {
			detectedType = domain.TelegramSourceTypeBroadcastChannel
		} else if inv.Megagroup {
			detectedType = domain.TelegramSourceTypeSupergroup
		} else {
			detectedType = domain.TelegramSourceTypeBasicGroup
		}

		caps := domain.SourceCapabilities{
			CanReadHistory:          false, // Not joined yet
			HasTopics:               false,
			HasDiscussion:           false,
			SenderMetadataAvailable: detectedType != domain.TelegramSourceTypeBroadcastChannel,
		}

		return &domain.ResolveSourceResponse{
			Address:          rawAddress,
			Title:            inv.Title,
			DetectedType:     detectedType,
			AccessType:       domain.AccessTypePrivate,
			ExpectedType:     expectedType,
			TypeMismatch:     expectedType != "" && expectedType != detectedType,
			Capabilities:     caps,
			AccountHasAccess: false,
			CanIngest:        false, // Explicit user authorization/joining needed
		}, nil
	}

	return nil, fmt.Errorf("unhandled chat invite type: %T", inviteClass)
}

func (r *Resolver) resolveMockSource(rawAddress, normAddr string, addrType AddressType, expectedType domain.TelegramSourceType) (*domain.ResolveSourceResponse, error) {
	lower := strings.ToLower(normAddr)

	var detectedType domain.TelegramSourceType
	var accessType domain.AccessType = domain.AccessTypePublic
	var peerID int64 = -1001234567890
	var title = "کانال بازار و نیازمندی‌ها"
	var linkedDiscussion *int64
	var hasDiscussion = false

	if addrType == AddressTypePeerID {
		id, _ := strconv.ParseInt(normAddr, 10, 64)
		peerID = id
	}

	if addrType == AddressTypeInviteLink || strings.Contains(lower, "priv") {
		accessType = domain.AccessTypePrivate
	}

	if strings.Contains(lower, "forum") || strings.Contains(lower, "topic") || expectedType == domain.TelegramSourceTypeForum {
		detectedType = domain.TelegramSourceTypeForum
		title = "انجمن گفتگوی بازار و تجهیزات"
		if peerID == -1001234567890 {
			peerID = -1001987654321
		}
	} else if strings.Contains(lower, "basic") || strings.Contains(lower, "group_small") || expectedType == domain.TelegramSourceTypeBasicGroup {
		detectedType = domain.TelegramSourceTypeBasicGroup
		accessType = domain.AccessTypePrivate
		title = "گروه مبادلات کالا"
		if peerID == -1001234567890 {
			peerID = -456789012
		}
	} else if strings.Contains(lower, "super") || strings.Contains(lower, "gap") || expectedType == domain.TelegramSourceTypeSupergroup {
		detectedType = domain.TelegramSourceTypeSupergroup
		title = "سوپرگروه خرید و فروش لوازم دیجیتال"
		if peerID == -1001234567890 {
			peerID = -1001876543210
		}
	} else {
		detectedType = domain.TelegramSourceTypeBroadcastChannel
		title = "بازار موبایل و لپتاپ پایتخت"
		linkedID := int64(-1001234567899)
		linkedDiscussion = &linkedID
		hasDiscussion = true
	}

	caps := domain.SourceCapabilities{
		CanReadHistory:          true,
		HasTopics:               detectedType == domain.TelegramSourceTypeForum,
		HasDiscussion:           hasDiscussion,
		SenderMetadataAvailable: detectedType != domain.TelegramSourceTypeBroadcastChannel,
	}

	typeMismatch := expectedType != "" && expectedType != detectedType

	return &domain.ResolveSourceResponse{
		Address:                rawAddress,
		PeerID:                 peerID,
		AccessHash:             1234567890123456789,
		Username:               normAddr,
		Title:                  title,
		DetectedType:           detectedType,
		AccessType:             accessType,
		ExpectedType:           expectedType,
		TypeMismatch:           typeMismatch,
		LinkedDiscussionPeerID: linkedDiscussion,
		Capabilities:           caps,
		AccountHasAccess:       true,
		CanIngest:              true,
	}, nil
}
