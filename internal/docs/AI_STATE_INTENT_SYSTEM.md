# AI State/Intent-Based System

## Overview

The AI response system has been redesigned to behave like a **competent human sales or support representative** who understands the tenant's business and speaks with intent, warmth, and restraint. The system is built around **state and intent, not templates**.

## Key Components

### 1. ConversationStateManager (`internal/ai/conversation_state.go`)

Tracks and analyzes conversation state:
- **Customer Intent**: inquiry, objection, negotiation, purchase-ready, frustration
- **Customer State**: browsing, interested, negotiating, committed, paid, escalated
- **AI Permissions**: What the AI is allowed to say at that moment
- **Contextual Signals**: price_discussed, consultation_unpaid, pricing_approval_required, etc.

### 2. ValuePreviewEnforcer (`internal/ai/value_preview_enforcer.go`)

Enforces value-preview model for paid services (consultancy, agency, coaching, professional services):
- Prevents AI from delivering paid outcomes before payment/approval
- Only allows detailed solutions, step-by-step guidance, strategies after payment confirmed
- Before payment: shows value preview, outlines what will be delivered, guides toward payment

### 3. ThinPromptBuilder (`internal/ai/thin_prompt_builder.go`)

Creates focused, context-aware prompts:
- Minimal core instruction
- Current context (last 2-3 turns)
- Structured business data (not verbose text)
- Constraints and signals from conversation state
- Brief writing style guidance

### 4. NegotiationHandler (`internal/ai/negotiation_handler.go`)

Handles negotiation with human-like behavior:
- Acknowledges emotion
- Anchors value
- Offers one meaningful alternative at a time
- Escalates cleanly when outside allowed range
- Confident handoff messages

## How It Works

1. **Message Analysis**: `ConversationStateManager.AnalyzeConversation()` determines:
   - Customer intent from message and context
   - Customer state from sales conversation
   - AI permissions based on industry, state, and payment status

2. **Prompt Building**: `ThinPromptBuilder.BuildPrompt()` creates a focused prompt with:
   - Core instruction based on intent
   - Current message and recent history
   - Structured business data
   - Constraints from conversation state

3. **Value Preview Enforcement**: `ValuePreviewEnforcer.EnforceConstraints()` adds constraints to prevent delivering paid outcomes before payment

4. **Response Generation**: AI generates response with thin, focused prompt

5. **Response Sanitization**: `ValuePreviewEnforcer.SanitizeResponse()` checks if response violates constraints

6. **Negotiation Handling**: If negotiation detected, `NegotiationHandler` builds human-like responses with clean escalation

## Integration

The system is integrated into `CustomerResponder`:
- Components initialized in `NewCustomerResponder()`
- `InitializeStateManager()` called when IndustryService is available
- Can be used in `GenerateCustomerReplyWithSales()` when state manager is available

## Benefits

1. **Human-like behavior**: AI responds like a competent salesperson, not a template
2. **Value protection**: Paid services don't give away outcomes before payment
3. **Clean escalation**: Negotiation escalates cleanly when outside range
4. **Thin prompts**: Focused on current message, not long instructions
5. **Structured context**: Business data injected as structure, not long text
6. **Intent-aware**: Responds differently based on customer intent and state

## Next Steps

1. Integrate into `GenerateCustomerReplyWithSales()` to use state/intent system
2. Add structured context injection for business data
3. Test with various industries and conversation flows
4. Monitor and refine based on real conversations

