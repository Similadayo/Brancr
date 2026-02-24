package executor

import "github.com/similadayo/brancr/internal/types"

func CheckOrder(ctx *types.ResolvedContext) TerminalResponse {
	// PHASE-8: Let the LLM generate context-aware responses
	// Don't return hardcoded text - the pipeline will call /generate endpoint
	// which will use the order data to create a proper response

	// Return empty response - pipeline will handle response generation
	return TerminalResponse{
		Text: "",    // Empty - let LLM generate response with order details
		Stop: false, // Don't stop - let pipeline continue to response generation
	}
}
