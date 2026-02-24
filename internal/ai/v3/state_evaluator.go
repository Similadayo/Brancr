package v3

// StateEvaluator is the "Brain" of the Think layer.
// It takes raw intents and signals and derives the human-level mental model.
type StateEvaluator struct{}

// NewStateEvaluator creates a new evaluator
func NewStateEvaluator() *StateEvaluator {
	return &StateEvaluator{}
}

// Evaluate performs the "Think" step: Raw Signals -> Mental Model
// It updates the ConversationContext with new Phase, Gradient, and Tone.
func (e *StateEvaluator) Evaluate(ctx *ConversationContext, intent Intent, lastPhase ConversationPhase) {
	// 1. Map Intent to UserSignals
	ctx.UserSignals = UserSignals{
		PrimaryIntent: intent,
		Momentum:      e.calculateMomentum(intent),
		SocialSignals: e.detectSocialSignals(intent),
	}

	// 2. Update Emotional Tone (simple heuristic for now, can be LLM-enhanced later)
	ctx.EmotionalTone = e.deriveTone(intent)

	// 3. Update Commitment Gradient
	ctx.CommitmentGradient = e.calculateGradient(ctx, intent)

	// 4. derive Conversation Phase
	ctx.Phase = e.derivePhase(ctx, lastPhase)
}

func (e *StateEvaluator) calculateMomentum(intent Intent) float64 {
	// Simple heuristic: Questions/Confirmations = Positive, Confusion/Cancel = Negative
	switch intent.Type {
	case IntentAddItem, IntentConfirmPayment, IntentCheckout:
		return 0.5 // Strong forward
	case IntentProductInquiry, IntentPriceInquiry:
		return 0.2 // Mild forward
	case IntentNegotiation:
		return 0.1 // Slow forward
	case IntentCancel, IntentRemoveItem:
		return -0.5 // Backward
	case IntentHelp, IntentUnknown:
		return 0.0 // Stalled
	}
	return 0.1
}

func (e *StateEvaluator) detectSocialSignals(intent Intent) SocialSignals {
	// This would map detailed entities or metadata in a real implementation
	// For now, checks if "friend" or emojis are in entities (if extraction supported it)
	// We'll rely on text analysis placeholders for now.
	return SocialSignals{
		IsFriend:  false, // TODO: Extract from text
		IsPlayful: false,
		HasEmoji:  false,
	}
}

func (e *StateEvaluator) deriveTone(intent Intent) string {
	switch intent.Emotion.Type {
	case EmotionHappy, EmotionEager:
		return "positive"
	case EmotionFrustrated, EmotionConfused:
		return "negative"
	}
	return "neutral"
}

// calculateGradient estimates 0.0 to 1.0 (Ready to Buy)
func (e *StateEvaluator) calculateGradient(ctx *ConversationContext, intent Intent) float64 {
	base := 0.0

	// Previous context weight
	if ctx.CurrentOrder != nil {
		base = 0.9 // Already ordered
	} else if len(ctx.PendingCart) > 0 {
		base = 0.4 // Has items
	}

	// Intent bump
	switch intent.Type {
	case IntentConfirmPayment:
		return 1.0 // Paid
	case IntentCheckout:
		base += 0.4 // Wants to pay
	case IntentAddItem:
		base += 0.1
	case IntentNegotiation:
		base += 0.1 // Interested but haggling
	case IntentPriceInquiry:
		base += 0.05
	}

	// Cap at 1.0
	if base > 1.0 {
		return 1.0
	}
	return base
}

// derivePhase determines the macro phase of conversation
func (e *StateEvaluator) derivePhase(ctx *ConversationContext, current ConversationPhase) ConversationPhase {
	// If just starting
	if current == "" {
		current = PhaseDiscovery
	}

	// Phase transitions
	gradient := ctx.CommitmentGradient

	switch current {
	case PhaseDiscovery:
		if len(ctx.PendingCart) > 0 || gradient > 0.3 {
			return PhaseComparison
		}
	case PhaseComparison:
		if ctx.UserSignals.PrimaryIntent.Type == IntentNegotiation {
			return PhaseNegotiation
		}
		if gradient > 0.6 {
			return PhaseCommitment
		}
	case PhaseNegotiation:
		if gradient > 0.7 || ctx.UserSignals.PrimaryIntent.Type == IntentCheckout {
			return PhaseCommitment
		}
	case PhaseCommitment:
		if ctx.UserSignals.PrimaryIntent.Type == IntentConfirmPayment {
			return PhasePayment
		}
	case PhasePayment:
		// Only move to fulfillment if payment is actually confirmed
		if gradient >= 1.0 {
			return PhaseFulfillment
		}
	}

	// [NEW] Allow explicit jump to Fulfillment if asking for status
	// Logic: If user asks "Where is my order?" (IntentFulfillment), jump to Fulfillment
	// (Assuming IntentFulfillment exists or using UserSignals)
	// For now, we rely on gradient=1.0 which ActionExecutor sets on payment success.

	return current
}
