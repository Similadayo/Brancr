package emotion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// Detector observes customer language to determine emotional state.
type Detector struct {
	llm types.LLMClient
}

// NewDetector creates a new emotion detector.
func NewDetector(llm types.LLMClient) *Detector {
	return &Detector{
		llm: llm,
	}
}

// Detect determines the customer's emotional state.
func (d *Detector) Detect(
	ctx context.Context,
	snapshot *v4ctx.ContextSnapshot,
) (*EmotionState, error) {

	// Default neutral state
	state := &EmotionState{
		Emotion:    types.EmotionNeutral,
		Confidence: 0.1,
	}

	if snapshot == nil {
		return state, nil
	}

	// Step 1: Heuristic Pass (Weak Signals)
	d.applyHeuristics(snapshot.Message, state)

	// Step 2: LLM Emotion Assist (Optional)
	if d.llm != nil {
		if llmState, err := d.detectByLLM(ctx, snapshot); err == nil && llmState != nil {
			// If LLM succeeds, it overrides/enhances heuristics
			state.Emotion = llmState.Emotion
			state.Confidence = llmState.Confidence
		} else {
			// Log but don't fail, stick to heuristics
			// Warning log could be useful here
		}
	}

	// Step 3: Map Emotion Flags (Final Consistency)
	d.mapFlags(state)

	return state, nil
}

func (d *Detector) applyHeuristics(msg string, state *EmotionState) {
	// Max confidence 0.4 for heuristics
	lower := strings.ToLower(msg)

	// Confusion
	if strings.Contains(msg, "???") || strings.Contains(lower, "what do you mean") || strings.Contains(lower, "don't understand") {
		state.Emotion = types.EmotionConfused
		state.Confidence = 0.4
		return
	}

	// Frustration/Anger
	if strings.Contains(msg, "!!!") || strings.Contains(lower, "wtf") || strings.Contains(lower, "stupid") {
		state.Emotion = types.EmotionAngry
		state.Confidence = 0.4
		return
	}

	// Satisfaction
	if strings.Contains(lower, "perfect") || strings.Contains(lower, "great") || strings.Contains(lower, "love it") {
		state.Emotion = types.EmotionHappy
		state.Confidence = 0.4
		return
	}
}

type llmEmotionResponse struct {
	Emotion    string  `json:"emotion"`
	Confidence float64 `json:"confidence"`
	// Ignore any reasoning or action suggestions
}

func (d *Detector) detectByLLM(ctx context.Context, snapshot *v4ctx.ContextSnapshot) (*EmotionState, error) {
	// Construct Prompt
	prompt := fmt.Sprintf(`Detect the dominant emotion expressed by the customer.
Business: %s
Message: "%s"

Categories: happy, neutral, frustrated, angry, confused.
Do NOT suggest actions.
Respond in valid JSON only: {"emotion": "string", "confidence": float}`,
		snapshot.Business.Name, snapshot.Message)

	// Call LLM
	jsonStr, err := d.llm.Generate(prompt)
	if err != nil {
		return nil, err
	}

	// Clean JSON (sometimes LLMs add markdown blocks)
	jsonStr = cleanJSON(jsonStr)

	var resp llmEmotionResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, err
	}

	// Validate & Clamp
	emotion := types.Emotion(strings.ToLower(resp.Emotion))
	conf := resp.Confidence
	if conf < 0.3 {
		conf = 0.3
	}
	if conf > 0.95 {
		conf = 0.95
	}

	return &EmotionState{
		Emotion:    emotion,
		Confidence: conf,
	}, nil
}

func (d *Detector) mapFlags(state *EmotionState) {
	// Reset all
	state.IsFrustrated = false
	state.IsAngry = false
	state.IsConfused = false
	state.IsSatisfied = false

	switch state.Emotion {
	case types.EmotionAngry:
		state.IsAngry = true
		state.IsFrustrated = true // Anger usually implies frustration
	case types.EmotionFrustrated:
		state.IsFrustrated = true
	case types.EmotionConfused:
		state.IsConfused = true
	case types.EmotionHappy:
		state.IsSatisfied = true
	}
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// Zap logger usage avoided inside logic as per instruction simplicity,
// using error return or silent fallback as strictly observational.
