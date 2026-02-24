package scenarios

import (
	"time"

	"github.com/similadayo/brancr/internal/simulation"
)

var Price_Sensitive = simulation.Scenario{
	Name: "Price_Sensitive",
	Turns: []simulation.ConversationTurn{
		{UserMessage: "Hi", TimeSinceLastMsg: 0},
		{UserMessage: "How much?", TimeSinceLastMsg: 10 * time.Second},
		{UserMessage: "That’s expensive", TimeSinceLastMsg: 30 * time.Second},
		{UserMessage: "I only have X", TimeSinceLastMsg: 1 * time.Minute},
	},
}
