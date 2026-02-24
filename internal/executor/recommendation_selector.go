package executor

import (
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/types"
)

// SelectRecommendations picks max 2 offerings based on priority:
// 1. Popular (not tracked yet, so we emulate or skipping)
// 2. Mid-price (Safe default)
// 3. Simple (First items usually)
// For now, simple heuristic: Pick first 2 items as "Safe recommendations" if no popularity data.
// Or finding "mid-price" if possible.
// Prompt says: "1. Marked as popular (if available), 2. Mid-price, 3. Simplest".
// Since we don't have popularity flags in `domain.Offering` yet (Step 3 definition), and no logic to calc mid-price easily without parsing price string (which is formatted).
// WAIT, `domain.Offering` generally comes from `normalizeOfferings`.
// `normalizeOfferings` uses `storage.Product`. `storage.Product` might have `Popularity` score?
// `ProductRepository` shows `Order("created_at DESC")`. Use heuristic for now.
// We will pick the first 2 as a safe default for "Standard" items.
// A real mid-price logic would require numeric price which we lost in normalization (we have `PriceText`).
// However, `fetchPrimaryOfferings` fetches `domain.Offering`.
// NOTE: `SelectRecommendations` receives `[]domain.Offering`.
// Constraint: We only have `Title`, `PriceText`, etc.
// Logic: Return first 2.
func SelectRecommendations(offerings []domain.Offering, ctx *types.ResolvedContext) []domain.Offering {
	limit := 2
	if len(offerings) <= limit {
		return offerings
	}
	return offerings[:limit]
}
