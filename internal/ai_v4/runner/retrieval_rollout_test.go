package runner

import (
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestRetrievalEngineEnabledForScope(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	convID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	t.Run("disabled_flag_wins", func(t *testing.T) {
		t.Setenv("AI_V4_RETRIEVAL_ENGINE_ENABLED", "false")
		t.Setenv("AI_V4_RETRIEVAL_ENGINE_CANARY_PERCENT", "100")
		if retrievalEngineEnabledForScope(tenantID, convID) {
			t.Fatalf("expected retrieval engine disabled")
		}
	})

	t.Run("zero_percent_disables", func(t *testing.T) {
		t.Setenv("AI_V4_RETRIEVAL_ENGINE_ENABLED", "true")
		t.Setenv("AI_V4_RETRIEVAL_ENGINE_CANARY_PERCENT", "0")
		if retrievalEngineEnabledForScope(tenantID, convID) {
			t.Fatalf("expected retrieval engine disabled at 0 percent")
		}
	})

	t.Run("full_percent_enables", func(t *testing.T) {
		t.Setenv("AI_V4_RETRIEVAL_ENGINE_ENABLED", "true")
		t.Setenv("AI_V4_RETRIEVAL_ENGINE_CANARY_PERCENT", "100")
		if !retrievalEngineEnabledForScope(tenantID, convID) {
			t.Fatalf("expected retrieval engine enabled at 100 percent")
		}
	})
}

func TestRetrievalEngineEnabledForScope_DeterministicBucket(t *testing.T) {
	os.Setenv("AI_V4_RETRIEVAL_ENGINE_ENABLED", "true")
	os.Setenv("AI_V4_RETRIEVAL_ENGINE_CANARY_PERCENT", "10")
	defer os.Unsetenv("AI_V4_RETRIEVAL_ENGINE_ENABLED")
	defer os.Unsetenv("AI_V4_RETRIEVAL_ENGINE_CANARY_PERCENT")

	tenantID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	convID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	first := retrievalEngineEnabledForScope(tenantID, convID)
	second := retrievalEngineEnabledForScope(tenantID, convID)
	if first != second {
		t.Fatalf("expected deterministic canary decision for same scope")
	}
}

