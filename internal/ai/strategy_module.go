package ai

import (
	"go.uber.org/zap"
)

// StrategyType defines the sales strategy style
// Tenants can choose their strategy - this affects HOW they sell, not WHAT they're allowed to sell
type StrategyType string

const (
	// StrategyConservative - Play it safe, build trust slowly, fewer counter-offers
	// Best for: high-ticket items, luxury brands, relationship-based sales
	StrategyConservative StrategyType = "conservative"

	// StrategyBalanced - Middle ground, standard negotiation flow
	// Best for: most businesses, general e-commerce
	StrategyBalanced StrategyType = "balanced"

	// StrategyAggressive - Push harder, faster closes, more persistence
	// Best for: volume sales, time-limited offers, competitive markets
	StrategyAggressive StrategyType = "aggressive"

	// StrategyFastClose - Minimize back-and-forth, accept early within range
	// Best for: low-margin items, high-volume businesses
	StrategyFastClose StrategyType = "fast_close"
)

// StrategyModule provides pluggable sales tactics
// This is separate from PolicyGate because:
// - PolicyGate: "You CANNOT do this" (hard rules)
// - StrategyModule: "You SHOULD do this" (sales tactics)
//
// Tenants can choose their strategy without ever touching safety or structure
type StrategyModule struct {
	Logger *zap.Logger
}

// StrategyConfig holds the configurable strategy parameters
type StrategyConfig struct {
	Type StrategyType

	// Counter-offer behavior
	CounterStepPct         float64 // How much to drop per round (0.10 = 10%)
	MinCounterStep         float64 // Minimum step size in currency
	MaxRoundsBeforeAccept  int     // How many rounds before accepting in-range offer

	// Persistence behavior
	PersistenceAttempts int     // How many times to try for better price
	FirmnessByRound     float64 // How much firmer to get each round (0.0-1.0)

	// Response style
	ResponseBrevity float64 // How short responses should be (0.0 = verbose, 1.0 = terse)
	SoftLanguage    bool    // Use soft language ("perhaps", "might") vs firm ("will", "is")

	// Closing behavior
	EarlyClosePct float64 // Accept if within this % of list price without countering
}

// NewStrategyModule creates a new strategy module
func NewStrategyModule(logger *zap.Logger) *StrategyModule {
	return &StrategyModule{
		Logger: logger,
	}
}

// GetConfig returns the strategy configuration for a given type
// These are the "defaults" that shape sales behavior
func (s *StrategyModule) GetConfig(strategyType StrategyType) *StrategyConfig {
	switch strategyType {
	case StrategyConservative:
		return &StrategyConfig{
			Type:                   StrategyConservative,
			CounterStepPct:         0.08, // 8% steps (slower descent)
			MinCounterStep:         10000,
			MaxRoundsBeforeAccept:  4,
			PersistenceAttempts:    3, // Try 3 times for better price
			FirmnessByRound:        0.15,
			ResponseBrevity:        0.3, // More detailed responses
			SoftLanguage:           true,
			EarlyClosePct:          0.03, // Accept if within 3%
		}

	case StrategyBalanced:
		return &StrategyConfig{
			Type:                   StrategyBalanced,
			CounterStepPct:         0.12, // 12% steps (moderate)
			MinCounterStep:         5000,
			MaxRoundsBeforeAccept:  3,
			PersistenceAttempts:    2, // Try 2 times for better price
			FirmnessByRound:        0.20,
			ResponseBrevity:        0.5, // Moderate length
			SoftLanguage:           false,
			EarlyClosePct:          0.05, // Accept if within 5%
		}

	case StrategyAggressive:
		return &StrategyConfig{
			Type:                   StrategyAggressive,
			CounterStepPct:         0.15, // 15% steps (faster descent)
			MinCounterStep:         5000,
			MaxRoundsBeforeAccept:  2,
			PersistenceAttempts:    3, // Push hard 3 times
			FirmnessByRound:        0.30,
			ResponseBrevity:        0.7, // Short, punchy responses
			SoftLanguage:           false,
			EarlyClosePct:          0.07, // Accept if within 7%
		}

	case StrategyFastClose:
		return &StrategyConfig{
			Type:                   StrategyFastClose,
			CounterStepPct:         0.20, // 20% steps (quick to floor)
			MinCounterStep:         5000,
			MaxRoundsBeforeAccept:  1,
			PersistenceAttempts:    1, // Just one try
			FirmnessByRound:        0.10,
			ResponseBrevity:        0.8, // Very brief
			SoftLanguage:           false,
			EarlyClosePct:          0.10, // Accept if within 10%
		}

	default:
		// Default to balanced
		return s.GetConfig(StrategyBalanced)
	}
}

// GenerateCounterOffer calculates the optimal counter-offer based on strategy
func (s *StrategyModule) GenerateCounterOffer(
	config *StrategyConfig,
	currentOffer float64,
	customerOffer float64,
	minPrice float64,
	round int,
) float64 {
	// Calculate the price range we're working with
	priceRange := currentOffer - minPrice
	if priceRange <= 0 {
		return minPrice
	}

	// Calculate step size based on strategy
	stepSize := priceRange * config.CounterStepPct
	if stepSize < config.MinCounterStep {
		stepSize = config.MinCounterStep
	}

	// Apply round-based firmness (reduce step size in later rounds)
	// This makes the AI "firmer" as negotiation progresses
	firmnessFactor := 1.0 - (float64(round) * config.FirmnessByRound)
	if firmnessFactor < 0.5 {
		firmnessFactor = 0.5 // Don't reduce by more than half
	}
	stepSize = stepSize * firmnessFactor

	counter := currentOffer - stepSize

	// Round to nearest 10k for cleaner numbers
	counter = float64(int(counter/10000)) * 10000

	// Never go below minimum
	if counter < minPrice {
		return minPrice
	}

	return counter
}

// ShouldAcceptOffer determines if an offer should be accepted based on strategy
func (s *StrategyModule) ShouldAcceptOffer(
	config *StrategyConfig,
	customerOffer float64,
	listPrice float64,
	minPrice float64,
	persistenceAttempts int,
	isCustomerPersisting bool,
) bool {
	// Check if offer is at or above list price
	if customerOffer >= listPrice {
		return true
	}

	// Check if offer is below minimum (never accept)
	if customerOffer < minPrice {
		return false
	}

	// Offer is in acceptable range - check strategy

	// 1. Check early close threshold
	discountPct := (listPrice - customerOffer) / listPrice
	if discountPct <= config.EarlyClosePct {
		return true // Accept near-list-price offers immediately
	}

	// 2. Check persistence threshold
	if isCustomerPersisting && persistenceAttempts >= config.PersistenceAttempts {
		return true // Customer persisted enough times
	}

	// 3. Check max rounds (if we've already tried enough times)
	if persistenceAttempts >= config.MaxRoundsBeforeAccept {
		return true
	}

	return false // Keep negotiating
}

// GetResponseStyle returns response style adjustments based on strategy and round
func (s *StrategyModule) GetResponseStyle(
	config *StrategyConfig,
	round int,
	nearFloor bool,
) *ResponseStyleHints {
	hints := &ResponseStyleHints{
		MaxLength:       250,
		UseSoftLanguage: config.SoftLanguage,
		Urgency:         0.3,
	}

	// Apply brevity setting
	hints.MaxLength = int(250 * (1.0 - config.ResponseBrevity))
	if hints.MaxLength < 80 {
		hints.MaxLength = 80 // Minimum length
	}

	// Increase firmness each round
	firmness := float64(round) * config.FirmnessByRound
	if firmness > 1.0 {
		firmness = 1.0
	}

	// Near floor = maximum firmness
	if nearFloor {
		firmness = 1.0
		hints.UseSoftLanguage = false
		hints.MaxLength = int(float64(hints.MaxLength) * 0.7) // 30% shorter near floor
	}

	// Apply firmness to response style
	hints.Urgency = 0.3 + (0.7 * firmness) // 0.3 → 1.0
	if firmness > 0.5 {
		hints.UseSoftLanguage = false // No soft language in later rounds
	}

	return hints
}

// ResponseStyleHints provides hints to ResponseComposer for tone adjustment
type ResponseStyleHints struct {
	MaxLength       int     // Maximum response length in characters
	UseSoftLanguage bool    // Use soft language ("perhaps", "might")
	Urgency         float64 // Urgency level 0.0 (relaxed) to 1.0 (urgent)
}

// CalculateConfidenceLevel calculates the AI's "confidence" in current position
// This decays over rounds, making the AI progressively firmer
// Confidence 1.0 = very flexible, 0.0 = completely firm
func (s *StrategyModule) CalculateConfidenceLevel(
	config *StrategyConfig,
	round int,
	nearFloor bool,
	listPrice float64,
	currentOffer float64,
	minPrice float64,
) float64 {
	// Start at full confidence
	confidence := 1.0

	// Decay by round
	decayPerRound := 0.15 + config.FirmnessByRound
	confidence -= float64(round) * decayPerRound

	// Extra decay if near floor
	if nearFloor {
		confidence -= 0.3
	}

	// Calculate how close we are to floor (0 = at floor, 1 = at list)
	if listPrice > minPrice {
		positionInRange := (currentOffer - minPrice) / (listPrice - minPrice)
		// Lower position = lower confidence (less room to move)
		confidence *= (0.3 + (0.7 * positionInRange))
	}

	// Clamp to valid range
	if confidence < 0.1 {
		confidence = 0.1 // Never completely inflexible (would sound robotic)
	}
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

