package conversation

import (
	"fmt"

	"github.com/perso/perso-mind/internal/domain"
)

var validTransitions = map[domain.ConversationState][]domain.ConversationState{
	domain.ConvStateNew: {
		domain.ConvStateAwaitingApproval,
		domain.ConvStateAwaitingConsent,
		domain.ConvStateActive,
		domain.ConvStateDeclined,
		domain.ConvStateOptedOut,
		domain.ConvStatePaused,
	},
	domain.ConvStateAwaitingApproval: {
		domain.ConvStateActive,
		domain.ConvStateAwaitingConsent,
		domain.ConvStateDeclined,
		domain.ConvStatePaused,
	},
	domain.ConvStateAwaitingConsent: {
		domain.ConvStateActive,
		domain.ConvStateWaitingForReply,
		domain.ConvStateDeclined,
		domain.ConvStateOptedOut,
		domain.ConvStatePaused,
	},
	domain.ConvStateActive: {
		domain.ConvStateWaitingForReply,
		domain.ConvStateCompleted,
		domain.ConvStatePaused,
		domain.ConvStateOptedOut,
		domain.ConvStateDeclined,
	},
	domain.ConvStateWaitingForReply: {
		domain.ConvStateActive,
		domain.ConvStateCompleted,
		domain.ConvStateDeclined,
		domain.ConvStateOptedOut,
		domain.ConvStatePaused,
	},
	domain.ConvStatePaused: {
		domain.ConvStateActive,
		domain.ConvStateWaitingForReply,
		domain.ConvStateCompleted,
		domain.ConvStateDeclined,
	},
	domain.ConvStateCompleted: {},
	domain.ConvStateDeclined:  {},
	domain.ConvStateOptedOut:  {},
}

// CanTransition validates if the transition from current to target is permitted
func CanTransition(current, target domain.ConversationState) bool {
	if current == target {
		return true
	}
	allowed, ok := validTransitions[current]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == target {
			return true
		}
	}
	return false
}

// Transition validates and returns an error if transition is illegal
func Transition(current, target domain.ConversationState) error {
	if !CanTransition(current, target) {
		return fmt.Errorf("illegal conversation transition from %s to %s", current, target)
	}
	return nil
}
