package events

import "github.com/google/uuid"

type ConversationResumedPayload struct {
	SessionID uuid.UUID
}
