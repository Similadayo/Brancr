package ai

// Compatibility shim — business knowledge generation now lives in internal/onboarding.
//
// Type alias keeps existing call sites (ai.KnowledgeResult) compiling unchanged.
// The AIClient method delegates to the new package, preserving the AIClientInterface.

import (
	"context"

	"github.com/similadayo/brancr/internal/onboarding"
)

type KnowledgeResult = onboarding.KnowledgeResult

func (c *AIClient) GenerateBusinessKnowledge(ctx context.Context, content string, userDescription string) (*KnowledgeResult, error) {
	return onboarding.GenerateBusinessKnowledge(ctx, c.Client, content, userDescription)
}
