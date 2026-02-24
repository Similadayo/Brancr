package social

import (
	"strings"
	"time"

	"go.uber.org/zap"
)

// MessageSplitter handles intelligent splitting of AI replies into sequences
type MessageSplitter struct {
	logger *zap.Logger
}

// NewMessageSplitter creates a new message splitter
func NewMessageSplitter(logger *zap.Logger) *MessageSplitter {
	return &MessageSplitter{
		logger: logger,
	}
}

// SplitDecision determines if and how to split a message
type SplitDecision struct {
	ShouldSplit bool
	Parts       []string
	Reason      string
}

// ShouldSplitMessage determines if a message should be split based on strict rules
// Default: ONE message. Only split when it improves clarity or emotional pacing.
func (ms *MessageSplitter) ShouldSplitMessage(reply string) SplitDecision {
	reply = strings.TrimSpace(reply)
	
	// Rule 1: Never split if reply is ≤ 2 short sentences
	sentences := ms.countSentences(reply)
	if sentences <= 2 && len(reply) < 200 {
		return SplitDecision{
			ShouldSplit: false,
			Parts:       []string{reply},
			Reason:      "too_short",
		}
	}

	// Rule 2: Never split simple replies (price, availability, payment info)
	if ms.isSimpleReply(reply) {
		return SplitDecision{
			ShouldSplit: false,
			Parts:       []string{reply},
			Reason:      "simple_reply",
		}
	}

	// Rule 3: Check for multiple logical parts
	parts := ms.detectLogicalParts(reply)
	if len(parts) <= 1 {
		return SplitDecision{
			ShouldSplit: false,
			Parts:       []string{reply},
			Reason:      "single_part",
		}
	}

	// Rule 4: Hard cap at 3 messages
	if len(parts) > 3 {
		// Compress into 2-3 messages
		parts = ms.compressParts(parts, 3)
	}

	// Rule 5: Only split if it improves clarity
	if !ms.wouldImproveClarity(parts, reply) {
		return SplitDecision{
			ShouldSplit: false,
			Parts:       []string{reply},
			Reason:      "no_clarity_improvement",
		}
	}

	return SplitDecision{
		ShouldSplit: true,
		Parts:       parts,
		Reason:      "multiple_logical_parts",
	}
}

// countSentences counts sentences in a message
func (ms *MessageSplitter) countSentences(text string) int {
	count := 0
	for _, char := range text {
		if char == '.' || char == '!' || char == '?' {
			count++
		}
	}
	if count == 0 {
		return 1 // At least one sentence
	}
	return count
}

// isSimpleReply checks if reply is a simple response that should never be split
func (ms *MessageSplitter) isSimpleReply(reply string) bool {
	replyLower := strings.ToLower(reply)
	
	// Simple price responses
	simplePatterns := []string{
		"it's", "it is", "the price is", "costs", "₦", "$",
		"yes, it's available", "yes, available", "it's available",
		"account number", "send account", "payment details",
		"just tell me", "tell me the price",
	}

	// Check if reply is mostly a single fact
	wordCount := len(strings.Fields(reply))
	if wordCount <= 15 {
		for _, pattern := range simplePatterns {
			if strings.Contains(replyLower, pattern) {
				return true
			}
		}
	}

	// Check if it's a single sentence with price/availability
	if ms.countSentences(reply) == 1 && (strings.Contains(replyLower, "₦") || strings.Contains(replyLower, "$") || strings.Contains(replyLower, "available")) {
		return true
	}

	return false
}

// detectLogicalParts detects natural break points in a message
func (ms *MessageSplitter) detectLogicalParts(reply string) []string {
	// Split by double newlines (paragraph breaks)
	paragraphs := strings.Split(reply, "\n\n")
	if len(paragraphs) > 1 {
		// Clean and filter empty paragraphs
		var parts []string
		for _, p := range paragraphs {
			p = strings.TrimSpace(p)
			if p != "" {
				parts = append(parts, p)
			}
		}
		if len(parts) > 1 {
			return parts
		}
	}

	// Look for logical connectors that indicate separate thoughts
	// Pattern: acknowledgment + fact + next step
	replyLower := strings.ToLower(reply)
	
	// Check for emotional acknowledgment followed by facts
	acknowledgmentMarkers := []string{
		"i hear you", "i understand", "i get that", "i see",
		"sure", "absolutely", "of course",
	}
	
	factMarkers := []string{
		"the price is", "it costs", "for the", "the lowest",
		"i can do", "we have", "available",
	}
	
	nextStepMarkers := []string{
		"if that works", "let me know", "i can hold", "when you're ready",
		"just let me know", "send me", "i'll send",
	}

	hasAck := false
	hasFact := false
	hasNextStep := false

	for _, marker := range acknowledgmentMarkers {
		if strings.Contains(replyLower, marker) {
			hasAck = true
			break
		}
	}

	for _, marker := range factMarkers {
		if strings.Contains(replyLower, marker) {
			hasFact = true
			break
		}
	}

	for _, marker := range nextStepMarkers {
		if strings.Contains(replyLower, marker) {
			hasNextStep = true
			break
		}
	}

	// If we have multiple logical parts, try to split
	if (hasAck && hasFact) || (hasFact && hasNextStep) || (hasAck && hasFact && hasNextStep) {
		parts := ms.splitByLogicalMarkers(reply, acknowledgmentMarkers, factMarkers, nextStepMarkers)
		if len(parts) > 1 {
			return parts
		}
	}

	// Default: single part
	return []string{reply}
}

// splitByLogicalMarkers splits message at logical transition points
func (ms *MessageSplitter) splitByLogicalMarkers(reply string, ackMarkers, factMarkers, nextStepMarkers []string) []string {
	var parts []string
	
	// Look for transitions between logical parts
	sentences := strings.FieldsFunc(reply, func(r rune) bool {
		return r == '.' || r == '!' || r == '?'
	})
	
	currentPart := ""
	for i, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		
		sentenceLower := strings.ToLower(sentence)
		
		// Check if this sentence starts a new logical part
		isAck := false
		isFact := false
		isNextStep := false
		
		for _, marker := range ackMarkers {
			if strings.HasPrefix(sentenceLower, marker) || strings.Contains(sentenceLower, marker) {
				isAck = true
				break
			}
		}
		
		for _, marker := range factMarkers {
			if strings.Contains(sentenceLower, marker) {
				isFact = true
				break
			}
		}
		
		for _, marker := range nextStepMarkers {
			if strings.Contains(sentenceLower, marker) {
				isNextStep = true
				break
			}
		}
		
		// If we have a current part and this sentence starts a new logical type, split
		if currentPart != "" {
			currentLower := strings.ToLower(currentPart)
			hasCurrentAck := false
			hasCurrentFact := false
			hasCurrentNextStep := false
			
			for _, marker := range ackMarkers {
				if strings.Contains(currentLower, marker) {
					hasCurrentAck = true
					break
				}
			}
			for _, marker := range factMarkers {
				if strings.Contains(currentLower, marker) {
					hasCurrentFact = true
					break
				}
			}
			for _, marker := range nextStepMarkers {
				if strings.Contains(currentLower, marker) {
					hasCurrentNextStep = true
					break
				}
			}
			
			// If logical type changes, split
			if (hasCurrentAck && (isFact || isNextStep)) ||
				(hasCurrentFact && (isAck || isNextStep)) ||
				(hasCurrentNextStep && (isAck || isFact)) {
				parts = append(parts, strings.TrimSpace(currentPart))
				currentPart = sentence
				continue
			}
		}
		
		// Add sentence to current part
		if currentPart == "" {
			currentPart = sentence
		} else {
			currentPart += ". " + sentence
		}
		
		// Don't let parts get too long (max 2-3 sentences per part)
		if i > 0 && i%2 == 0 && len(parts) < 2 {
			parts = append(parts, strings.TrimSpace(currentPart))
			currentPart = ""
		}
	}
	
	if currentPart != "" {
		parts = append(parts, strings.TrimSpace(currentPart))
	}
	
	// Fallback: if splitting didn't work well, return original
	if len(parts) <= 1 {
		return []string{reply}
	}
	
	return parts
}

// compressParts compresses parts to fit within max limit
func (ms *MessageSplitter) compressParts(parts []string, max int) []string {
	if len(parts) <= max {
		return parts
	}
	
	// Combine smaller parts
	compressed := make([]string, 0, max)
	current := ""
	
	for i, part := range parts {
		if len(compressed) < max-1 {
			if current == "" {
				current = part
			} else {
				current += "\n\n" + part
			}
			
			// If we're near the limit, add current and start new
			if i == len(parts)-1 || len(compressed) == max-2 {
				compressed = append(compressed, current)
				current = ""
			}
		} else {
			// Last part gets everything remaining
			if current != "" {
				current += "\n\n" + part
			} else {
				current = part
			}
		}
	}
	
	if current != "" {
		compressed = append(compressed, current)
	}
	
	return compressed
}

// wouldImproveClarity checks if splitting would improve clarity
func (ms *MessageSplitter) wouldImproveClarity(parts []string, original string) bool {
	// If only 2 parts and original is short, don't split
	if len(parts) == 2 && len(original) < 300 {
		return false
	}
	
	// If parts are too short individually, don't split
	allShort := true
	for _, part := range parts {
		if len(part) > 50 {
			allShort = false
			break
		}
	}
	if allShort && len(parts) == 2 {
		return false
	}
	
	// If splitting creates very uneven parts, don't split
	if len(parts) == 2 {
		ratio := float64(len(parts[0])) / float64(len(parts[1]))
		if ratio < 0.2 || ratio > 5.0 {
			return false
		}
	}
	
	return true
}

// GetSendDelays returns human-like delays between messages
func (ms *MessageSplitter) GetSendDelays(count int) []time.Duration {
	if count <= 1 {
		return []time.Duration{0}
	}
	
	delays := make([]time.Duration, count)
	delays[0] = 0 // First message immediately
	
	if count >= 2 {
		delays[1] = 350 * time.Millisecond // 300-500ms for second
	}
	
	if count >= 3 {
		delays[2] = 700 * time.Millisecond // 600-800ms for third
	}
	
	return delays
}

