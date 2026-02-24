package conversation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// HashCart creates a deterministic hash of cart contents for change detection.
// PHASE 4.8: Used to detect if cart has actually changed (not just conversational state).
//
// Hash includes: ProductID + Quantity + VariantIDs
// Hash is stable unless cart contents change.
func HashCart(items []CartItem) string {
	if len(items) == 0 {
		return ""
	}

	// Sort items for deterministic hashing
	sortedItems := make([]CartItem, len(items))
	copy(sortedItems, items)
	sort.Slice(sortedItems, func(i, j int) bool {
		return sortedItems[i].ProductID < sortedItems[j].ProductID
	})

	// Build hash input
	var parts []string
	for _, item := range sortedItems {
		// Sort variants for deterministic ordering
		sortedVariants := make([]string, len(item.Variants))
		copy(sortedVariants, item.Variants)
		sort.Strings(sortedVariants)

		variantStr := strings.Join(sortedVariants, ",")
		part := fmt.Sprintf("%s:%d:%s", item.ProductID, item.Quantity, variantStr)
		parts = append(parts, part)
	}

	hashInput := strings.Join(parts, "|")

	// SHA256 hash
	hasher := sha256.New()
	hasher.Write([]byte(hashInput))
	return hex.EncodeToString(hasher.Sum(nil))
}

// CartItem represents an item in the pending cart
// PHASE 4.8: Extended with variant resolution tracking
type CartItem struct {
	ProductID string
	Quantity  int
	Variants  []string // Selected variant IDs

	// Phase 4.8: Variant Resolution Tracking
	VariantsResolved  bool // All required variants selected (computed from product schema)
	VariantsConfirmed bool // User explicitly approved variants
}
