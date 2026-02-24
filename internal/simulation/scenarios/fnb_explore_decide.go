package scenarios

import (
	"time"

	"github.com/similadayo/brancr/internal/simulation"
)

var FNB_Explore_Decide = simulation.Scenario{
	Name: "FNB_Explore_Decide",
	Turns: []simulation.ConversationTurn{
		{UserMessage: "Hello", TimeSinceLastMsg: 0},
		{UserMessage: "What do you sell?", TimeSinceLastMsg: 10 * time.Second},
		{UserMessage: "How much is the cake?", TimeSinceLastMsg: 30 * time.Second},
		{UserMessage: "That’s a bit high", TimeSinceLastMsg: 2 * time.Minute},
		{UserMessage: "Okay, I’ll take it", TimeSinceLastMsg: 1 * time.Minute},
	},
}
