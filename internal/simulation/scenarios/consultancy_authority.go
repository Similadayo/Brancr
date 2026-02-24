package scenarios

import (
	"time"

	"github.com/similadayo/brancr/internal/simulation"
)

var Consultancy_Authority = simulation.Scenario{
	Name: "Consultancy_Authority",
	Turns: []simulation.ConversationTurn{
		{UserMessage: "Hello", TimeSinceLastMsg: 0},
		{UserMessage: "I want a strategy session", TimeSinceLastMsg: 20 * time.Second},
		{UserMessage: "How does it work?", TimeSinceLastMsg: 1 * time.Minute},
		{UserMessage: "Sounds good", TimeSinceLastMsg: 30 * time.Second},
	},
}
