package scenarios

import (
	"time"

	"github.com/similadayo/brancr/internal/simulation"
)

var Agency_Discovery = simulation.Scenario{
	Name: "Agency_Discovery",
	Turns: []simulation.ConversationTurn{
		{UserMessage: "Hi", TimeSinceLastMsg: 0},
		{UserMessage: "I need help with marketing", TimeSinceLastMsg: 15 * time.Second},
		{UserMessage: "What exactly do you offer?", TimeSinceLastMsg: 45 * time.Second},
		{UserMessage: "Hmm", TimeSinceLastMsg: 2 * time.Minute},
	},
}
