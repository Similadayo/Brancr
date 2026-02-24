package core

import (
	"context"

	"github.com/similadayo/brancr/internal/ai"
	v3 "github.com/similadayo/brancr/internal/ai/v3"
)

// llmClientAdapter bridges ai.AIClient with v3.LLMClient interface
type llmClientAdapter struct {
	client *ai.AIClient
}

// Generate implements v3.LLMClient interface
func (a *llmClientAdapter) Generate(ctx context.Context, messages []v3.LLMMessage) (string, error) {
	// Convert v3.LLMMessage to ai.Message
	aiMessages := make([]ai.Message, len(messages))
	for i, m := range messages {
		aiMessages[i] = ai.Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	return a.client.Generate(ctx, aiMessages)
}
