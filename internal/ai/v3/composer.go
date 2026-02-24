package v3

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"

	"github.com/similadayo/brancr/internal/persona"
	"github.com/similadayo/brancr/internal/storage"
)

// =============================================================================
// RESPONSE COMPOSER - LLM + Templates + Empathy + Role Profiles
// =============================================================================

// LLMClient is the interface for the AI client (defined here to avoid import cycles)
type LLMClient interface {
	Generate(ctx context.Context, messages []LLMMessage) (string, error)
}

// LLMMessage represents a message to the LLM
type LLMMessage struct {
	Role    string
	Content string
}

// responseComposer implements ResponseComposer interface with LLM integration
type responseComposer struct {
	llmClient                 LLMClient
	expressionContractBuilder *ExpressionContractBuilder
}

// NewResponseComposer creates a new response composer with optional LLM client
func NewResponseComposer(llmClient LLMClient) ResponseComposer {
	return &responseComposer{
		llmClient:                 llmClient,
		expressionContractBuilder: NewExpressionContractBuilder(),
	}
}

// Generate creates a human-like response based on action result
// PRIORITY: LLM for natural responses, template fallback if LLM unavailable
func (c *responseComposer) Generate(ctx *ConversationContext, action Action, result ExecutionResult) Response {
	// 1. Handle ErrorResult immediately (CRITICAL: Never fake success)
	if errRes, ok := result.(ErrorResult); ok {
		return c.renderError(ctx, errRes)
	}

	var messages []string
	var err error

	// 2. TRY LLM FIRST for natural, conversational responses
	if c.llmClient != nil {
		// Get the customer message from recent messages if available
		customerMessage := ""
		if len(ctx.RecentMessages) > 0 {
			customerMessage = ctx.RecentMessages[len(ctx.RecentMessages)-1].Content
		}

		// Try 1: Standard generation
		var llmResponse string
		llmResponse, err = c.generateWithLLM(context.Background(), ctx, customerMessage, action, result, false, "")

		// Check for stalls (legacy check, still valid concept)
		isStall := false
		if err == nil && llmResponse != "" {
			lower := strings.ToLower(llmResponse)
			forbiddenStalls := []string{"checking", "one moment", "bear with me"}
			for _, phrase := range forbiddenStalls {
				if strings.Contains(lower, phrase) {
					isStall = true
					break
				}
			}
		}

		// PHASE 2: VALIDATE RESPONSE (Hard Separation of Facts vs Style)
		// We effectively check if the AI obeyed the "Mandatory Facts" contract.
		mandatory := BuildMandatoryInfo(ctx)
		validationErr := ValidateResponse(llmResponse, mandatory)

		// RETRY STRATEGY: If LLM fails, stalls, OR fails validation -> Retry with correction
		if err != nil || llmResponse == "" || isStall || validationErr != nil {
			correction := ""
			if validationErr != nil {
				correction = fmt.Sprintf("PREVIOUS RESPONSE INVALID. REASON: %s. FIX IT IMMEDIATELY.", validationErr.Error())
			}

			// Try 2: Force grounding + Correction
			retryResponse, retryErr := c.generateWithLLM(context.Background(), ctx, customerMessage, action, result, true, correction)
			if retryErr == nil && retryResponse != "" {
				llmResponse = retryResponse
				err = nil
			}
		}

		var hiddenCommands []HiddenCommand

		if err == nil && llmResponse != "" {
			// Extract hidden commands first (before cleanup)
			llmResponse, hiddenCommands = c.extractHiddenCommands(llmResponse)

			// Clean up response: remove em dashes and fancy punctuation
			llmResponse = c.cleanupResponse(llmResponse)

			// PHASE 2: Apply verbosity policy based on commitment gradient (REMOVED: Prompt handles this now)
			// llmResponse = c.applyVerbosityPolicy(llmResponse, ctx)

			// Split into multiple messages for natural conversation based on style
			messages = c.splitIntoMessages(llmResponse, ctx.RoleProfile.Style)

			// PHASE 3: Enforce closure - limit messages after key actions
			messages = c.enforceClosure(messages, action, ctx)

			// (Phase 3.5 moved to AFTER validation to ensure Truth > Voice)

			// PHASE 3.8: HARD SAFETY - If we have data, NEVER say "I don't understand"
			messages = c.enforceNoUnknownReplies(messages, ctx)

			// PHASE 3.9: STAFF VOICE - Kill robotic phrases
			messages = c.enforceStaffVoice(messages)

			// PHASE 4: Final social behavior validation
			messages = c.validateHumanOutput(messages, action, ctx)

			// PHASE 4.5: APPLY VOICE (Style Modifiers) - RUNS LAST
			// "Voice Is a Modifier, Not a Brain"
			if ctx.VoiceFingerprint != nil {
				// Convert Persona struct to storage.Persona for compatibility
				tempPersona := storage.Persona{
					Tone:     ctx.RoleProfile.Tone,
					Humor:    true,
					Language: "en",
				}

				voiceProfile := MergeVoice(tempPersona, *ctx.VoiceFingerprint)
				messages = ApplyVoice(messages, voiceProfile)
			}

			// If validation filtered everything out (e.g. stalled), fall through to raw fallback
			if len(messages) > 0 {
				// Apply length limits and return
				messages = c.applyLengthLimits(messages, ctx.RoleProfile.MaxMsgLen)
				return Response{
					Messages:       messages,
					HiddenCommands: hiddenCommands,
					Metadata: ResponseMetadata{
						IntentType: IntentUnknown,
						ActionType: action.Type,
						Confidence: 1.0,
					},
				}
			}
		}
	}

	// 3. FINAL FALLBACK: Data-Driven Response (AI Failed)
	// Return the raw action result rendered simply
	return c.renderFallback(action, result)
}

// renderFallback renders a simple, safe message based on the Result Type
func (c *responseComposer) renderFallback(action Action, result ExecutionResult) Response {
	var msg string

	switch res := result.(type) {
	case ShowProductsResult:
		if len(res.Products) == 0 {
			msg = "No products found."
		} else {
			msg = fmt.Sprintf("Found %d products. Check the menu!", len(res.Products))
		}

	case ShowCartResult:
		msg = fmt.Sprintf("Cart Total: %.2f", res.Total)

	case ShowPaymentResult:
		msg = fmt.Sprintf("Total: %.2f. Pay to %s.", res.Total, res.Bank.AccountNumber)

	case ShowQuoteResult:
		msg = res.Message

	case ShowStatusResult:
		msg = res.Message

	case NegotiateResult:
		msg = res.Message

	case CollectRequirementsResult:
		// Generate message for requirement collection
		msg = res.Message
		if res.Requirement.Label != "" {
			msg += fmt.Sprintf("\n\nTo complete your order, please send: %s", res.Requirement.Label)
			if res.Requirement.Description != "" {
				msg += fmt.Sprintf("\n%s", res.Requirement.Description)
			}
		}

	case AckResult:
		msg = res.Message

	default:
		msg = "Action completed."
	}

	return Response{
		Messages: []string{msg},
		Metadata: ResponseMetadata{
			IntentType: IntentUnknown,
			ActionType: action.Type,
			Confidence: 0.5,
		},
	}
}

func (c *responseComposer) renderError(ctx *ConversationContext, res ErrorResult) Response {
	// Grounded escalation - no stalling or vague messages
	return Response{
		Messages: []string{
			"I've flagged this for a human team member to assist you.",
		},
		Metadata: ResponseMetadata{
			ActionType: ActionError,
			Confidence: 0.7,
		},
	}
}

// generateWithLLM creates a smart, conversational response using LLM
func (c *responseComposer) generateWithLLM(ctx context.Context, convCtx *ConversationContext, customerMessage string, action Action, result ExecutionResult, forceGrounding bool, correctionInstruction string) (string, error) {
	if c.llmClient == nil {
		return "", fmt.Errorf("llm client not available")
	}

	// Build the unified Expression Contract (one human brain, shaped by context)
	contract := c.expressionContractBuilder.Build(convCtx, customerMessage, action, result, forceGrounding)

	// Inject Correction if present
	if correctionInstruction != "" {
		contract += "\n\nCRITICAL SYSTEM INSTRUCTION:\n" + correctionInstruction
	}

	messages := []LLMMessage{
		{Role: "system", Content: contract},
		{Role: "user", Content: customerMessage},
	}

	raw, err := c.llmClient.Generate(ctx, messages)
	if err != nil {
		return "", err
	}

	// Strip error language if action succeeded
	// We assume success because ErrorResult is handled before calling this
	raw = c.stripErrorLanguage(raw)

	return strings.TrimSpace(raw), nil
}

// summarizeResult creates a human-readable summary of what happened
func (c *responseComposer) summarizeResult(action Action, result ExecutionResult, ctx *ConversationContext) string {
	// This is used by expression_contract_builder (Wait, no it's not. The builder has its own logic)
	// The builder uses buildActionContext.
	// This method might be dead code or used by `buildActionBlock` (which I removed, as Builder handles it).
	// Builder uses `buildActionContext` internally.
	// So we don't need `summarizeResult` or `buildActionBlock` anymore if `ExpressionContractBuilder.Build` does it.
	// `ExpressionContractBuilder` calls `b.buildActionContext`.
	// So `summarizeResult` is likely dead code here.
	// I will omit it to be clean.
	return ""
}

// stripErrorLanguage removes apologetic/error phrases when action succeeded
func (c *responseComposer) stripErrorLanguage(text string) string {
	errorPhrases := []string{
		"something went wrong",
		"try again",
		"an error occurred",
		"i apologize",
		"sorry for the inconvenience",
		"there was a problem",
		"couldn't process",
		"failed to",
		"unable to",
	}

	lower := strings.ToLower(text)
	for _, phrase := range errorPhrases {
		if strings.Contains(lower, phrase) {
			// If error language detected but action succeeded, return simple confirmation
			return "Done! \u2705"
		}
	}

	return text
}

// =============================================================================
// EMOJI POLICY - Confidence-based, not role-based (Preserved for Helpers)
// =============================================================================

// EmojiPolicy controls emoji usage based on confidence and emotion
type EmojiPolicy int

const (
	EmojiPolicyNone    EmojiPolicy = iota // No emojis allowed
	EmojiPolicyLight                      // Max 1 emoji
	EmojiPolicyAllowed                    // No restriction
)

// deriveEmojiPolicy determines emoji usage from context
// Humans use emojis based on confidence + momentum, not feelings
func deriveEmojiPolicy(ctx *ConversationContext) EmojiPolicy {
	// Never emojis when frustrated or confused
	if ctx.Emotion.Type == EmotionFrustrated || ctx.Emotion.Type == EmotionConfused {
		return EmojiPolicyNone
	}

	// High confidence → minimal emoji (feels earned)
	if ctx.CommitmentGradient >= 0.7 {
		return EmojiPolicyLight
	}

	// Mid confidence → optional emoji
	if ctx.CommitmentGradient >= 0.3 {
		return EmojiPolicyAllowed
	}

	// Low confidence → avoid emojis (can feel salesy)
	return EmojiPolicyNone
}

// =============================================================================
// HELPERS
// =============================================================================

func (c *responseComposer) applyLengthLimits(messages []string, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = 500 // Default max length
	}

	result := make([]string, 0, len(messages))
	for _, msg := range messages {
		if len(msg) <= maxLen {
			result = append(result, msg)
			continue
		}

		// Message is too long - split at natural breaks
		remaining := msg
		for len(remaining) > maxLen {
			chunk := remaining[:maxLen]

			// Find last natural break point (. ! ? , or space)
			breakPoint := -1
			for i := len(chunk) - 1; i > maxLen/2; i-- {
				if chunk[i] == '.' || chunk[i] == '!' || chunk[i] == '?' {
					breakPoint = i + 1 // Include the punctuation
					break
				}
			}

			// If no sentence break, try comma or space
			if breakPoint == -1 {
				for i := len(chunk) - 1; i > maxLen/2; i-- {
					if chunk[i] == ',' {
						breakPoint = i + 1
						break
					}
				}
			}
			if breakPoint == -1 {
				for i := len(chunk) - 1; i > maxLen/2; i-- {
					if chunk[i] == ' ' {
						breakPoint = i
						break
					}
				}
			}

			if breakPoint == -1 {
				breakPoint = maxLen // Fallback: hard cut
			}

			result = append(result, strings.TrimSpace(remaining[:breakPoint]))
			remaining = strings.TrimSpace(remaining[breakPoint:])
		}

		// Add remaining text as final message
		if len(remaining) > 0 {
			result = append(result, remaining)
		}
	}
	return result
}

// cleanupResponse removes em dashes and other fancy punctuation from AI responses
func (c *responseComposer) cleanupResponse(response string) string {
	// Remove em dashes and en dashes
	response = strings.ReplaceAll(response, "\u2014", "-")   // em dash
	response = strings.ReplaceAll(response, "\u2013", "-")   // en dash
	response = strings.ReplaceAll(response, "\u2026", "...") // ellipsis

	// Remove fancy quotes
	response = strings.ReplaceAll(response, "\u201C", "\"") // left double quote
	response = strings.ReplaceAll(response, "\u201D", "\"") // right double quote
	response = strings.ReplaceAll(response, "\u2018", "'")  // left single quote
	response = strings.ReplaceAll(response, "\u2019", "'")  // right single quote

	return strings.TrimSpace(response)
}

// splitIntoMessages splits a long response into multiple natural messages
func (c *responseComposer) splitIntoMessages(response string, style ConversationalStyle) []string {
	// High verbosity = don't split unless paragraphs
	if style.Verbosity == "high" {
		return strings.Split(response, "\n\n")
	}

	// First, try splitting by double newlines (paragraphs)
	paragraphs := strings.Split(response, "\n\n")

	var messages []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// BURST MODE (Low Verbosity): Split aggressive by sentence
		if style.Verbosity == "low" {
			sentences := c.splitSentences(p)
			for _, s := range sentences {
				messages = append(messages, s)
			}
			continue
		}

		// MEDIUM Verbosity: Keep short paragraphs together, split long ones
		if len(p) <= 150 {
			messages = append(messages, p)
			continue
		}

		// Split long paragraphs by sentences
		sentences := c.splitSentences(p)
		currentMsg := ""
		for _, s := range sentences {
			if len(currentMsg)+len(s) <= 150 {
				if currentMsg != "" {
					currentMsg += " " + s
				} else {
					currentMsg = s
				}
			} else {
				if currentMsg != "" {
					messages = append(messages, currentMsg)
				}
				currentMsg = s
			}
		}
		if currentMsg != "" {
			messages = append(messages, currentMsg)
		}
	}

	// If no messages were created, return original as single message
	if len(messages) == 0 {
		return []string{response}
	}

	return messages
}

// splitSentences splits text into sentences
func (c *responseComposer) splitSentences(text string) []string {
	var sentences []string
	current := ""

	for _, r := range text {
		current += string(r)
		if r == '.' || r == '!' || r == '?' {
			sentences = append(sentences, strings.TrimSpace(current))
			current = ""
		}
	}
	if current != "" {
		sentences = append(sentences, strings.TrimSpace(current))
	}

	return sentences
}

// =============================================================================
// VERBOSITY CONTROL - "Say it once, then shut up"
// =============================================================================

// applyVerbosityPolicy (DELETED: Prompt handles this now)
// trimVerbosity (DELETED: Prompt handles this now)

// isConversationClosed determines if the social move is complete
// After these actions, the AI should not add more questions or options
func (c *responseComposer) isConversationClosed(action Action, ctx *ConversationContext) bool {
	switch action.Type {
	case ActionAddToCart:
		// Do not truncate post-purchase upsells
		return false
	case ActionConfirmOrder:
		// Order confirmed = definitely closed
		return true
	case ActionShowPayment:
		// PAYMENT DETAILS MUST BE FULLY VISIBLE. Do not close/truncate.
		return false
	case ActionRemoveFromCart:
		// Item removed = closed
		return true
	}
	return false
}

// enforceClosure limits messages after a closure point
// Prevents the AI from reopening loops with extra questions
func (c *responseComposer) enforceClosure(messages []string, action Action, ctx *ConversationContext) []string {
	if !c.isConversationClosed(action, ctx) {
		return messages
	}

	// If momentum is very high, use micro-response
	if ctx.UserSignals.Momentum >= 0.7 && len(messages) > 0 {
		// Just acknowledge - don't elaborate
		microResponses := []string{"👍", "Done!", "Got it!", "✅"}
		return []string{microResponses[rand.Intn(len(microResponses))]}
	}

	// Allow at most 2 messages after closure
	if len(messages) > 2 {
		return messages[:2]
	}

	return messages
}

// =============================================================================
// EMOJI MANIPULATION - Strip or limit emojis based on policy
// =============================================================================

// Common emojis to detect and optionally remove
var emojiPatterns = []string{
	"👍", "✅", "🎉", "😊", "🙏", "💯", "❤️", "🔥", "👏", "😄",
	"👋", "✨", "💪", "🤝", "💡", "📦", "🛒", "💵", "🙌", "😁",
}

// stripEmojis removes all common emojis from messages
func stripEmojis(messages []string) []string {
	result := make([]string, len(messages))
	for i, msg := range messages {
		cleaned := msg
		for _, emoji := range emojiPatterns {
			cleaned = strings.ReplaceAll(cleaned, emoji, "")
		}
		result[i] = strings.TrimSpace(cleaned)
	}
	return result
}

// limitEmojis keeps only the first N emojis across all messages
func limitEmojis(messages []string, maxEmojis int) []string {
	emojiCount := 0
	result := make([]string, len(messages))

	for i, msg := range messages {
		cleaned := msg
		for _, emoji := range emojiPatterns {
			for strings.Contains(cleaned, emoji) {
				if emojiCount < maxEmojis {
					emojiCount++
					// Keep this emoji, but break to count next occurrence
					break
				}
				// Already at limit, remove this emoji
				cleaned = strings.Replace(cleaned, emoji, "", 1)
			}
		}
		result[i] = strings.TrimSpace(cleaned)
	}
	return result
}

// applyEmojiPolicy applies the emoji policy to messages
func applyEmojiPolicy(messages []string, policy EmojiPolicy) []string {
	switch policy {
	case EmojiPolicyNone:
		return stripEmojis(messages)
	case EmojiPolicyLight:
		return limitEmojis(messages, 1)
	default:
		return messages
	}
}

func applyTone(messages []string, preset persona.TonePreset, commitment float64) []string {
	// 1. Emoji Policy
	emojiPolicy := EmojiPolicyNone
	switch preset {
	case persona.ToneFriendly, persona.ToneCasual:
		if commitment >= 0.3 {
			emojiPolicy = EmojiPolicyAllowed
		} else {
			emojiPolicy = EmojiPolicyLight // Safe default for friendly
		}
	case persona.ToneLuxury:
		if commitment >= 0.7 {
			emojiPolicy = EmojiPolicyLight
		}
	case persona.ToneProfessional:
		emojiPolicy = EmojiPolicyNone
	default:
		emojiPolicy = EmojiPolicyLight // Fail safe
	}
	messages = applyEmojiPolicy(messages, emojiPolicy)

	// 2. Formatting Policy (Simple regex-based style adjustments)
	// This is "IDE-agent safe" - no complex NLP, just reliable patterns.
	for i, msg := range messages {
		if preset == persona.ToneProfessional || preset == persona.ToneLuxury {
			// Remove exclamation marks (too loud)
			messages[i] = strings.ReplaceAll(msg, "!", ".")
			// Remove casual slang if possible (simple substitution)
			messages[i] = strings.ReplaceAll(messages[i], "Hey!", "Hello.")
			messages[i] = strings.ReplaceAll(messages[i], "got it", "noted")
		} else if preset == persona.ToneCasual {
			// Lowercase greeting start? No, keep it correct English but relaxed.
			// Maybe ensure "Hey" instead of "Hello"
			if strings.HasPrefix(msg, "Hello") {
				messages[i] = strings.Replace(msg, "Hello", "Hey", 1)
			}
		}
	}

	return messages
}

// =============================================================================
// FINAL VALIDATION CHAIN - All social behavior guards in one place
// =============================================================================

// validateHumanOutput applies all social behavior guards in the correct order.
// This is the final gate before messages reach the user.
func (c *responseComposer) validateHumanOutput(messages []string, action Action, ctx *ConversationContext) []string {
	// =========================================================================
	// PHASE 8: STRICT BYPASS FOR DATA ACTIONS (Menu, Payment, Pitch)
	// If the action is data-driven, we bypass ALL social guards.
	// We want the raw, unadulterated data to reach the user.
	// =========================================================================
	if action.Type == ActionShowMenu ||
		action.Type == ActionPitchProduct ||
		action.Type == ActionShowPayment ||
		action.Type == ActionShowCart {
		return messages // RAW PASS-THROUGH
	}

	// Guard 0: Enforce customer expectations FIRST
	// This is the most important - match what customer anticipates
	messages = enforceExpectation(messages, ctx, action)

	// Guard 1.5: STRIP FAKE ASYNC - Remove any remaining stall phrases
	// This catches stalls for unanswerable queries (e.g., "Let me check the weather")
	messages = c.stripFakeAsync(messages)

	// Guard 2: Prevent repeating the same social move (DISABLED for Strict Obligation)
	// messages = preventRepeatMove(messages, ctx, action)

	// Guard 3: Prevent reopening closed loops
	messages = preventLoopReopen(messages, ctx, action)

	// Guard 4: Apply conciseness in high-commitment contexts (not a full silence)
	if shouldGoSilent(ctx, action) && silenceAllowed(ctx) && len(messages) > 1 {
		messages = messages[:1] // Keep first LLM message only, don't replace
	}

	// Guard 5: Apply tone policy (emoji + punctuation + length)
	messages = applyTone(messages, ctx.RoleProfile.TonePreset, ctx.CommitmentGradient)

	return messages
}

// =============================================================================
// FAKE ASYNC LANGUAGE BAN - AI is not actually checking anything
// =============================================================================

// stripFakeAsync removes deceptive async language that breaks trust
func (c *responseComposer) stripFakeAsync(messages []string) []string {
	banned := []string{
		"let me check",
		"just a moment",
		"hold on",
		"checking",
		"please wait",
		"one moment",
		"give me a sec",
	}

	result := make([]string, 0, len(messages))
	for _, msg := range messages {
		lower := strings.ToLower(msg)
		hasBanned := false
		for _, b := range banned {
			if strings.Contains(lower, b) {
				hasBanned = true
				break
			}
		}
		if hasBanned {
			// Strip banned message (don't replace, just remove so we can fallback)
			continue
		} else {
			result = append(result, msg)
		}
	}
	return result
}

// =============================================================================
// HARD SAFETY - NO UNKNOWN REPLIES
// =============================================================================

// enforceNoUnknownReplies prevents "I don't know" / "Connection error" if we have data.
// It rewrites the message to a helpful default.
func (c *responseComposer) enforceNoUnknownReplies(messages []string, ctx *ConversationContext) []string {
	// If we have data, we MUST NOT say "I don't know" or "I'm trouble"
	hasData := len(ctx.Products) > 0 || len(ctx.Services) > 0 || ctx.PaymentInfo != nil

	if !hasData {
		return messages // If truly no data, allow fallback
	}

	fallbackPhrases := []string{
		"having trouble connecting",
		"don't understand",
		"not sure what you mean",
		"can you clarify",
		"say that again",
		"missed that",
		"didn't catch that",
	}

	for i, msg := range messages {
		lower := strings.ToLower(msg)
		for _, phrase := range fallbackPhrases {
			if strings.Contains(lower, phrase) {
				// REWRITE to default behavior based on available data
				if len(ctx.Products) > 0 {
					// Pick a random product to suggest if possible, or just the first
					// For stability, pick the first one for now
					item := ctx.Products[0].Name
					if len(ctx.Products) > 1 {
						item = fmt.Sprintf("%s and %s", ctx.Products[0].Name, ctx.Products[1].Name)
					}
					messages[i] = fmt.Sprintf("We have %s available! Would you like to see the full menu?", item)
				} else if len(ctx.Services) > 0 {
					service := ctx.Services[0].Name
					messages[i] = fmt.Sprintf("We offer %s. Would you like to know more about it?", service)
				} else {
					// Generic polite fallback
					messages[i] = "How can I help you today?"
				}
				break // Stop checking phrases for this message, we already rewrote it
			}
		}
	}
	return messages
}

// enforceStaffVoice strips out robotic "assistant" language globaly
func (c *responseComposer) enforceStaffVoice(messages []string) []string {
	botPhrases := []string{
		"what are you trying to do",
		"i can help with that",
		"please ask again",
		"could you clarify",
		"how can i help", // Too generic, we want "What are you in the mood for?"
		"i'm an ai",
		"as an language model",
	}

	for i, msg := range messages {
		lower := strings.ToLower(msg)
		for _, phrase := range botPhrases {
			if strings.Contains(lower, phrase) {
				// Replace or strip
				// For now, simple strip logic. Ideally, we replace with better phrasing.
				// But since enforceNoUnknownReplies runs before this, we might already be safe.
				// If we find these, it means the LLM slipped up.
				// Let's replace with something generic but human.
				messages[i] = strings.ReplaceAll(lower, phrase, "")
				if strings.TrimSpace(messages[i]) == "" {
					messages[i] = "Got it. Let me suggest something."
				}
			}
		}
	}
	return messages
}

func (c *responseComposer) extractHiddenCommands(response string) (string, []HiddenCommand) {
	var commands []HiddenCommand

	// 1. Extract and remove ANALYSIS blocks (discard them, we only use them for thinking)
	reAnalysis := regexp.MustCompile(`<!--\s*ANALYSIS:\s*({[\s\S]*?})\s*-->`)
	response = reAnalysis.ReplaceAllString(response, "")

	// 2. Extract and remove COMMANDS (keep them for execution)
	reCmd := regexp.MustCompile(`<!--\s*(ORDER|DELIVERY|PAY_METHOD):\s*({.*?})\s*-->`)
	cleanResponse := reCmd.ReplaceAllStringFunc(response, func(match string) string {
		submatches := reCmd.FindStringSubmatch(match)
		if len(submatches) == 3 {
			// 4. Validate command (SECURITY HARDENING)
			// Phase 8: Reject unauthorized commands
			cmdType := submatches[1]
			payload := submatches[2]
			cmd := HiddenCommand{
				Type:    cmdType,
				Payload: payload,
			}
			if ValidateHiddenCommand(cmd, nil) { // Assuming ValidateHiddenCommand is defined elsewhere
				commands = append(commands, cmd)
			}
		}
		return ""
	})
	return strings.TrimSpace(cleanResponse), commands
}
