package conversation_test

import (
	"testing"

	"github.com/perso/perso-mind/internal/conversation"
	"github.com/perso/perso-mind/internal/domain"
)

func TestStateMachineTransitions(t *testing.T) {
	tests := []struct {
		name      string
		from      domain.ConversationState
		to        domain.ConversationState
		shouldErr bool
	}{
		{
			name:      "NEW to AWAITING_APPROVAL",
			from:      domain.ConvStateNew,
			to:        domain.ConvStateAwaitingApproval,
			shouldErr: false,
		},
		{
			name:      "AWAITING_APPROVAL to ACTIVE",
			from:      domain.ConvStateAwaitingApproval,
			to:        domain.ConvStateActive,
			shouldErr: false,
		},
		{
			name:      "ACTIVE to WAITING_FOR_REPLY",
			from:      domain.ConvStateActive,
			to:        domain.ConvStateWaitingForReply,
			shouldErr: false,
		},
		{
			name:      "WAITING_FOR_REPLY to COMPLETED",
			from:      domain.ConvStateWaitingForReply,
			to:        domain.ConvStateCompleted,
			shouldErr: false,
		},
		{
			name:      "ACTIVE to OPTED_OUT",
			from:      domain.ConvStateActive,
			to:        domain.ConvStateOptedOut,
			shouldErr: false,
		},
		{
			name:      "COMPLETED to ACTIVE (Illegal transition)",
			from:      domain.ConvStateCompleted,
			to:        domain.ConvStateActive,
			shouldErr: true,
		},
		{
			name:      "OPTED_OUT to ACTIVE (Illegal transition)",
			from:      domain.ConvStateOptedOut,
			to:        domain.ConvStateActive,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := conversation.Transition(tt.from, tt.to)
			if (err != nil) != tt.shouldErr {
				t.Errorf("Transition(%s -> %s) error = %v, shouldErr = %v", tt.from, tt.to, err, tt.shouldErr)
			}
		})
	}
}
