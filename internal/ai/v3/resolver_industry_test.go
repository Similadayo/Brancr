package v3

import (
	"testing"
)

func TestResolver_IndustryBehavior(t *testing.T) {
	resolver := NewActionResolver()

	t.Run("Food Industry: Shows Menu on Greet", func(t *testing.T) {
		ctx := &ConversationContext{
			RoleProfile: RoleProfile{
				Behavior: RoleBehavior{
					ShowMenuOnGreet: true,
				},
			},
		}
		intent := Intent{Type: IntentGreeting}
		action := resolver.Decide(ctx, intent)

		if action.Type != ActionShowProducts {
			t.Errorf("Expected ActionShowProducts for Food Greeting, got %s", action.Type)
		}
		if action.Reason != "Industry behavior: Show menu/products on greeting" {
			t.Errorf("Unexpected reason: %s", action.Reason)
		}
	})

	t.Run("Consulting Industry: Standard Greet", func(t *testing.T) {
		ctx := &ConversationContext{
			RoleProfile: RoleProfile{
				Behavior: RoleBehavior{
					ShowMenuOnGreet: false,
				},
			},
		}
		intent := Intent{Type: IntentGreeting}
		action := resolver.Decide(ctx, intent)

		if action.Type != ActionGreet {
			t.Errorf("Expected ActionGreet for Consulting Greeting, got %s", action.Type)
		}
	})
}
