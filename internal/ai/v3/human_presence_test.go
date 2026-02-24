package v3

import (
	"testing"
)

// TestHumanPresence_BurstMode tests the message splitting logic
func TestHumanPresence_BurstMode(t *testing.T) {
	composer := NewResponseComposer(nil).(*responseComposer)

	// Long message to test splitting
	longMessage := "This is the first sentence. This is the second sentence. " +
		"This is the third sentence. This is the fourth sentence. " +
		"This is the fifth sentence. This is the sixth sentence. " +
		"This is the seventh sentence. This is the eighth sentence."

	t.Run("Low Verbosity (Bursts)", func(t *testing.T) {
		style := ConversationalStyle{Verbosity: "low"}
		msgs := composer.splitIntoMessages(longMessage, style)
		if len(msgs) <= 1 {
			t.Errorf("Expected multiple messages for low verbosity, got %d", len(msgs))
		}
	})

	t.Run("High Verbosity (Paragraphs)", func(t *testing.T) {
		style := ConversationalStyle{Verbosity: "high"}
		msgs := composer.splitIntoMessages(longMessage, style)
		if len(msgs) != 1 {
			t.Errorf("Expected single message for high verbosity, got %d", len(msgs))
		}
	})
}

// TestHumanPresence_StrategicSilence tests ActionDefer
func TestHumanPresence_StrategicSilence(t *testing.T) {
	resolver := NewActionResolver()
	ctx := &ConversationContext{
		RoleProfile: RoleProfile{},
	}

	t.Run("Low Confidence -> Clarify", func(t *testing.T) {
		intent := Intent{
			Type:       IntentUnknown,
			Confidence: 0.3, // Very low confidence
		}
		action := resolver.Decide(ctx, intent)

		// V3 prefers clarification over deferral (better UX)
		if action.Type != ActionAskClarification {
			t.Errorf("Expected ActionAskClarification for low confidence (V3 improvement), got %s", action.Type)
		}
	})

	t.Run("Medium Confidence -> Clarify", func(t *testing.T) {
		intent := Intent{
			Type:       IntentUnknown,
			Confidence: 0.5,
		}
		action := resolver.Decide(ctx, intent)

		if action.Type != ActionAskClarification {
			t.Errorf("Expected ActionAskClarification for medium confidence, got %s", action.Type)
		}
	})
}

// TestHumanPresence_MenuFatigue tests avoiding repetitive menus
func TestHumanPresence_MenuFatigue(t *testing.T) {
	resolver := NewActionResolver().(*actionResolver)

	ctx := &ConversationContext{
		RoleProfile: RoleProfile{
			Behavior: RoleBehavior{ShowMenuOnGreet: true},
		},
		RecentMessages: []Message{
			{Role: "assistant", Content: "Here's what we have: ..."}, // Menu just shown
			{Role: "customer", Content: "Hi"},
		},
	}

	t.Run("Greet with recent menu -> No Menu", func(t *testing.T) {
		action := resolver.handleGreeting(ctx, StateIdle)
		if action.Type == ActionShowProducts {
			t.Errorf("Expected ActionGreet due to menu fatigue, got ActionShowProducts")
		}
	})

	t.Run("Inquiry with recent menu -> Clarify", func(t *testing.T) {
		action := resolver.handleProductInquiry(ctx)
		if action.Type == ActionShowProducts {
			t.Errorf("Expected ActionAskClarification due to menu fatigue, got ActionShowProducts")
		}
	})
}

// TestHumanPresence_SilenceGate tests handling of short inputs
func TestHumanPresence_SilenceGate(t *testing.T) {
	resolver := NewActionResolver()
	ctx := &ConversationContext{
		RoleProfile: RoleProfile{},
		RecentMessages: []Message{
			{Role: "assistant", Content: "Hello"},
			{Role: "customer", Content: "Okay"},
		},
	}

	t.Run("Silence Gate - Okay -> Defer", func(t *testing.T) {
		intent := Intent{
			Type:       IntentUnknown,
			Confidence: 0.5,
		}
		action := resolver.Decide(ctx, intent)

		if action.Type != ActionDefer {
			t.Errorf("Expected ActionDefer for Silence Gate 'Okay', got %s", action.Type)
		}
	})

	t.Run("Silence Gate - None -> Defer", func(t *testing.T) {
		ctx.RecentMessages[1].Content = "None"
		intent := Intent{
			Type:       IntentUnknown,
			Confidence: 0.5,
		}
		action := resolver.Decide(ctx, intent)

		if action.Type != ActionDefer {
			t.Errorf("Expected ActionDefer for Silence Gate 'None', got %s", action.Type)
		}
	})
}
