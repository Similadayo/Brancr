package scenarios

import (
	"time"

	"github.com/similadayo/brancr/internal/simulation"
)

var Ecommerce_Hesitation = simulation.Scenario{
	Name: "Ecommerce_Hesitation",
	Turns: []simulation.ConversationTurn{
		{UserMessage: "Hi", TimeSinceLastMsg: 0},
		{UserMessage: "Do you have size 42?", TimeSinceLastMsg: 30 * time.Second},
		{UserMessage: "I’ll check later", TimeSinceLastMsg: 1 * time.Minute},
		{UserMessage: "Hello", TimeSinceLastMsg: 5 * time.Hour}, // 5 hours silence
	},
}
