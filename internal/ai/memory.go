package ai

// Compatibility shim — MemoryService now lives in internal/recall.
//
// Type alias keeps existing call sites (ai.MemoryService) compiling unchanged.
// GenerateInsightFromConversation's client parameter changes from AIClientInterface
// to recall.Generator — *ai.AIClient satisfies it via the promoted Generate method.

import (
	"github.com/similadayo/brancr/internal/recall"
)

type MemoryService = recall.MemoryService
