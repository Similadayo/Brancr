package runner

import (
	"hash/fnv"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

func retrievalEngineEnabledForScope(tenantID uuid.UUID, conversationID uuid.UUID) bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("AI_V4_RETRIEVAL_ENGINE_ENABLED")), "false") {
		return false
	}

	percent := 100
	if raw := strings.TrimSpace(os.Getenv("AI_V4_RETRIEVAL_ENGINE_CANARY_PERCENT")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			percent = parsed
		}
	}
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}

	key := tenantID.String()
	if conversationID != uuid.Nil {
		key = key + ":" + conversationID.String()
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	bucket := int(h.Sum32() % 100)
	return bucket < percent
}

