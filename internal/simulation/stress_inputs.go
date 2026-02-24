package simulation

import "time"

// StressInputs defines chaotic or ambiguous inputs to test system stability.
var StressInputs = []string{
	"hmm",
	"ok",
	"nah",
	"idk",
	"maybe later",
	"just looking",
	"wait",
	"stop",
	"hello", // Repeated greeting
	"hi again",
	"actually nvm",
	"price?",      // Ambiguous single word
	"expensive",   // Objection without context
	"yes",         // Ambiguous confirmation
	"no",          // Ambiguous rejection
	"123 main st", // Address dump
	"08012345678", // Phone dump
}

// ChaosScenario combines stress inputs to test degradation.
var Chaos_Scenario = Scenario{
	Name: "Chaos_Stress_Test",
	Turns: []ConversationTurn{
		{UserMessage: "Hi", TimeSinceLastMsg: 0},
		{UserMessage: "hmm", TimeSinceLastMsg: 5 * time.Second},
		{UserMessage: "price?", TimeSinceLastMsg: 2 * time.Second}, // Burst
		{UserMessage: "nah", TimeSinceLastMsg: 10 * time.Second},
		{UserMessage: "Hello", TimeSinceLastMsg: 1 * time.Minute}, // Reset attempt
		{UserMessage: "actually nvm", TimeSinceLastMsg: 5 * time.Second},
	},
}
