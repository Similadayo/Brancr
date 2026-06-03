package ai

// Compatibility shim — menu parsing logic now lives in internal/catalog/ingest.
//
// The type aliases below keep existing call sites (ai.ParsedMenuItem,
// ai.ParseMenuTextResult) compiling unchanged. The AIClient methods delegate
// to the new package so the AIClientInterface contract is preserved without
// any changes to mocks or the tenantapi handler field.

import (
	"context"

	"github.com/similadayo/brancr/internal/catalog/ingest"
)

type (
	ParsedMenuItem      = ingest.ParsedMenuItem
	ParseMenuTextResult = ingest.ParseMenuTextResult
)

func (c *AIClient) ParseMenuText(ctx context.Context, text string, defaultCurrency string) (*ParseMenuTextResult, error) {
	return ingest.ParseMenuText(ctx, c.Client, text, defaultCurrency)
}

func (c *AIClient) ParseMenuFromImage(ctx context.Context, imageData string, mimeType string, defaultCurrency string) (*ParseMenuTextResult, error) {
	return ingest.ParseMenuFromImage(ctx, c.Client, imageData, mimeType, defaultCurrency)
}

// cleanJSONResponse is kept as a package-level alias so any internal/ai files
// that referenced it (e.g. other parsers) continue to compile unchanged.
func cleanJSONResponse(response string) string {
	return ingest.CleanJSONResponse(response)
}
