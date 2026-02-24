package memory

import "math"

// CalculateNewConfidence updates determination based on repetition.
// If values match: reinforce. If differing: decay or conflict logic.
func CalculateNewConfidence(currentConf float64, newConf float64) float64 {
	// Simple reinforcement learning curve
	// Asymptotic approach to 1.0
	// New = Old + (1 - Old) * (LearningRate * NewObservationConfidence)

	learningRate := 0.5
	boost := (1.0 - currentConf) * (learningRate * newConf)

	result := currentConf + boost

	// Cap at 0.99 (Reserve 1.0 for Explicit)
	if result > 0.99 {
		return 0.99
	}
	return math.Round(result*1000) / 1000 // Round to 3 decimals
}
