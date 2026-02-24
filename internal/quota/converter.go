package quota

// TokenUsageFromAI converts ai.TokenUsage-like structure to quota.TokenUsage
// This is a helper to avoid import cycles between ai and quota packages
func TokenUsageFromAI(promptTokens, completionTokens, totalTokens int) *TokenUsage {
	if promptTokens == 0 && completionTokens == 0 && totalTokens == 0 {
		return nil
	}
	return &TokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	}
}

