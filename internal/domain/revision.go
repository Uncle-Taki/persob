package domain

import (
	"crypto/sha256"
	"fmt"
)

// RevisionKey identifies the immutable source content being dispatched.
func RevisionKey(msg *TelegramMessage) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%v:%s", msg.TelegramChatID, msg.TelegramMessageID, msg.EditedDate, msg.MessageText))))
}
