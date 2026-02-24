package ai

import (
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// NewAIResponder wires up everything correctly
func NewAIResponder(db *gorm.DB, persona storage.Persona) *Responder {
	return NewAIResponderWithQuota(db, persona, nil)
}

// NewAIResponderWithQuota creates a responder with optional quota service for cost tracking
func NewAIResponderWithQuota(db *gorm.DB, persona storage.Persona, qs *quota.QuotaService) *Responder {
	// Create submodules
	embedder := NewEmbeddingClient()
	memory := &MemoryService{
		DB:      db,
		Encoder: embedder,
	}

	client := NewAIClient()       // Your LLM (OpenAI) chat generator
	classifier := NewClassifier() // Intent detection
	emotion := NewEmotionAnalyzer()
	policy := NewPolicyEngine()

	return &Responder{
		Client:       client,
		Memory:       memory,
		Persona:      persona,
		Classifier:   classifier,
		Emotion:      emotion,
		Policy:       policy,
		QuotaService: qs, // Optional: for cost tracking
	}
}
