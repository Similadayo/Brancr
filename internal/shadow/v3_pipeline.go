package shadow

import (
	"context"
	"time"

	v3 "github.com/similadayo/brancr/internal/ai/v3"
)

func NewV3ShadowPipeline(
	pipelineArg v3.Pipeline,
) Pipeline {
	pipelineArg.DisableExecution()   // critical
	pipelineArg.DisableEvents()      // critical
	pipelineArg.DisablePersistence() // critical

	return &v3Shadow{pipeline: pipelineArg}
}

type v3Shadow struct {
	pipeline v3.Pipeline
}

func (s *v3Shadow) Process(ctx context.Context, request interface{}) ShadowResult {
	start := time.Now()

	req, ok := request.(v3.ConversationRequest)
	if !ok {
		return ShadowResult{
			Reply:     "ERROR: Invalid request type for V3 shadow",
			Timestamp: time.Now(),
		}
	}

	out, err := s.pipeline.Process(ctx, req)
	if err != nil {
		return ShadowResult{
			Reply:     "ERROR: " + err.Error(),
			Timestamp: time.Now(),
		}
	}

	// Helpers for safe meta extraction
	actionType, _ := out.Meta["action_type"].(string)
	goal, _ := out.Meta["goal"].(string)
	respMode, _ := out.Meta["response_mode"].(string)

	return ShadowResult{
		Reply: out.HumanText,

		SuggestedAction:  actionType,
		ConversationGoal: goal,
		ResponseMode:     respMode,

		DurationMs: time.Since(start).Milliseconds(),
		Timestamp:  time.Now(),
	}
}
