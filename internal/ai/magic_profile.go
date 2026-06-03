package ai

// Compatibility shim — business profile generation now lives in internal/onboarding.
//
// Type aliases keep existing call sites (ai.BusinessProfileResult,
// ai.BusinessProfilePersona) compiling unchanged. The AIClient method delegates
// to the new package, preserving the AIClientInterface contract.

import (
	"context"

	"github.com/similadayo/brancr/internal/onboarding"
)

type (
	BusinessProfilePersona = onboarding.BusinessProfilePersona
	BusinessProfileResult  = onboarding.BusinessProfileResult
)

func (c *AIClient) GenerateBusinessProfile(ctx context.Context, content string, userDescription string) (*BusinessProfileResult, error) {
	return onboarding.GenerateBusinessProfile(ctx, c.Client, content, userDescription)
}
