package eval

import (
	"strings"

	"github.com/similadayo/brancr/internal/conversation"
)

type ConversationMetrics struct {
	AcknowledgementRate float64
	ForwardMotionRate   float64
	RepetitionRate      float64
	ClarificationRate   float64
	ForcedActionRate    float64
}

type TurnData struct {
	AIResponse   string
	Action       string
	Goal         string
	ResponseMode string
}

func CalculateMetrics(turns []TurnData) ConversationMetrics {
	if len(turns) == 0 {
		return ConversationMetrics{}
	}

	var ackCount, fwdCount, repCount, clarCount, forcedCount int
	ackVariants := conversation.AcknowledgeVariants // From Step 4

	for i, turn := range turns {
		// 1. Acknowledgement Rate
		// Naive check: does it start with an ack phrase?
		lowerResp := strings.ToLower(turn.AIResponse)
		for _, v := range ackVariants {
			if strings.HasPrefix(lowerResp, strings.ToLower(v)) {
				ackCount++
				break
			}
		}

		// 2. Forward Motion & Clarification
		// Based on Action
		switch turn.Action {
		case "SUGGEST_NEXT_STEP", "RESUME_CONVERSATION", "PROPOSE_DISCOUNT",
			"REQUEST_PAYMENT", "CONFIRM_ORDER", "LOCK_PRICE", "INITIALIZE_ORDER":
			fwdCount++
		case "CLARIFY_INTENT":
			clarCount++
		}

		// 3. Repetition Rate (vs previous AI response)
		if i > 0 {
			prev := turns[i-1].AIResponse
			if isRepeat(prev, turn.AIResponse) {
				repCount++
			}
		}

		// 4. Forced Action Rate
		// Action is COMMIT/PAYMENT but Goal is EXPLORE/DECIDE
		if (turn.Action == "REQUEST_PAYMENT" || turn.Action == "CONFIRM_ORDER") &&
			(turn.Goal == "EXPLORE" || turn.Goal == "DECIDE") {
			forcedCount++
		}
	}

	total := float64(len(turns))
	return ConversationMetrics{
		AcknowledgementRate: float64(ackCount) / total,
		ForwardMotionRate:   float64(fwdCount) / total,
		RepetitionRate:      float64(repCount) / total,
		ClarificationRate:   float64(clarCount) / total,
		ForcedActionRate:    float64(forcedCount) / total,
	}
}

func isRepeat(prev, curr string) bool {
	if prev == "" || curr == "" {
		return false
	}
	// Simple containment or high similarity
	// Borrow logic from repetition_guard or simplify
	return strings.Contains(strings.ToLower(curr), strings.ToLower(prev))
}
