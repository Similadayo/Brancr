package runner

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/ai_v4/catalog"
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
)

var (
	switchConfirmationRegex = regexp.MustCompile(`(?i)\b(switch to|change to|replace with)\b`)
	modelRequestRegex       = regexp.MustCompile(`(?i)\b(iphone|samsung|galaxy|pixel|infinix|tecno|redmi|xiaomi)\s+(?:[a-z]{0,10}\d{1,2}[a-z]?|\d{1,2}[a-z]?)(?:\s*(?:pro|max|plus|ultra|mini|fe|se|edge))?(?:\s*\(?\d{2,4}\s*gb\)?)?`)
	modelNumberRegex        = regexp.MustCompile(`(\d{1,2})`)
)

type skuRequest struct {
	ItemID    string
	VariantID string
	Name      string
}

func featureEnabled(name string, defaultValue bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return defaultValue
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func skuLockEnabled() bool {
	return featureEnabled("AI_V4_SKU_LOCK_ENABLED", false)
}

func unavailableGuardEnabled() bool {
	return featureEnabled("AI_V4_UNAVAILABLE_GUARD_ENABLED", false)
}

func specPriorityEnabled() bool {
	return featureEnabled("AI_V4_SPEC_PRIORITY_ENABLED", false)
}

func closeoutSuppressionEnabled() bool {
	return featureEnabled("AI_V4_CLOSEOUT_SUPPRESSION_ENABLED", false)
}

func itemIDFromName(name string) string {
	return normalizeFamilyName(name)
}

func variantIDFromName(name string) string {
	return normalizeCompact(normalizeText(name))
}

func extractRequestedModelVariant(message string, knowledge *v4ctx.TenantKnowledgeSnapshot) (string, string, string) {
	if knowledge == nil {
		return "", "", ""
	}
	if exact := catalog.ResolveLongestMatch(message, knowledge); exact != nil && strings.TrimSpace(exact.Name) != "" {
		return itemIDFromName(exact.Name), variantIDFromName(exact.Name), exact.Name
	}
	candidate := resolveAvailabilityCandidate(message, "", knowledge)
	if candidate == nil || strings.TrimSpace(candidate.name) == "" {
		return "", "", ""
	}
	return itemIDFromName(candidate.name), variantIDFromName(candidate.name), candidate.name
}

func isExplicitSwitchConfirmation(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	return switchConfirmationRegex.MatchString(msg)
}

func isLockedSKUCompatible(activeItemID string, activeVariantID string, reqItemID string, reqVariantID string) bool {
	if strings.TrimSpace(activeItemID) == "" || strings.TrimSpace(activeVariantID) == "" {
		return true
	}
	if strings.TrimSpace(reqItemID) == "" && strings.TrimSpace(reqVariantID) == "" {
		return true
	}
	if strings.TrimSpace(reqItemID) != "" && !strings.EqualFold(strings.TrimSpace(reqItemID), strings.TrimSpace(activeItemID)) {
		return false
	}
	if strings.TrimSpace(reqVariantID) != "" && !strings.EqualFold(strings.TrimSpace(reqVariantID), strings.TrimSpace(activeVariantID)) {
		return false
	}
	return true
}

func buildSwitchRequiredReply(current string, requested string) string {
	current = strings.TrimSpace(current)
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return fmt.Sprintf("You are currently on %s. Let us know if you want to change to a different item.", current)
	}
	return fmt.Sprintf("You are currently on %s. Do you want to change to %s?", current, requested)
}

func applyActiveSKULock(orderSnapshot *v4ctx.OrderSnapshot, itemName string, agreedPrice int64) {
	if orderSnapshot == nil {
		return
	}
	name := strings.TrimSpace(itemName)
	if name == "" {
		return
	}
	orderSnapshot.ActiveItemID = itemIDFromName(name)
	orderSnapshot.ActiveVariantID = variantIDFromName(name)
	orderSnapshot.PendingSwitchItemID = ""
	orderSnapshot.PendingSwitchVariantID = ""
	orderSnapshot.SwitchConfirmed = false
	if agreedPrice > 0 {
		orderSnapshot.AgreedUnitPrice = agreedPrice
	}
}

func confirmSwitchToRequested(orderSnapshot *v4ctx.OrderSnapshot, req skuRequest) {
	if orderSnapshot == nil {
		return
	}
	orderSnapshot.ActiveItemID = req.ItemID
	orderSnapshot.ActiveVariantID = req.VariantID
	orderSnapshot.PendingSwitchItemID = ""
	orderSnapshot.PendingSwitchVariantID = ""
	orderSnapshot.SwitchConfirmed = true
	orderSnapshot.AgreedUnitPrice = 0
}

func detectUnavailableModelRequest(message string, knowledge *v4ctx.TenantKnowledgeSnapshot) (string, []availabilityCandidate, bool) {
	if knowledge == nil {
		return "", nil, false
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return "", nil, false
	}
	if exact := catalog.ResolveLongestMatch(msg, knowledge); exact != nil && strings.TrimSpace(exact.Name) != "" {
		return "", nil, false
	}
	match := modelRequestRegex.FindString(msg)
	if strings.TrimSpace(match) == "" {
		return "", nil, false
	}
	requested := strings.TrimSpace(match)
	requestNorm := normalizeText(requested)
	if requestNorm == "" {
		return "", nil, false
	}
	if !modelNumberRegex.MatchString(requestNorm) {
		// Ignore generic brand phrases like "iphone that can handle heavy task".
		return "", nil, false
	}

	requestNumber := ""
	if n := modelNumberRegex.FindStringSubmatch(requestNorm); len(n) > 1 {
		requestNumber = strings.TrimSpace(n[1])
	}
	requestBrand := strings.Fields(requestNorm)
	brand := ""
	if len(requestBrand) > 0 {
		brand = requestBrand[0]
	}

	candidates := buildCatalogCandidates(knowledge)
	if len(candidates) == 0 {
		return requested, nil, true
	}

	hasModel := false
	for _, c := range candidates {
		nameNorm := normalizeText(c.name)
		if nameNorm == "" {
			continue
		}
		if requestNumber != "" && strings.Contains(nameNorm, requestNumber) {
			hasModel = true
			break
		}
		if strings.Contains(nameNorm, requestNorm) || strings.Contains(requestNorm, nameNorm) {
			hasModel = true
			break
		}
	}
	if hasModel {
		return "", nil, false
	}

	alts := make([]availabilityCandidate, 0, 3)
	for _, c := range candidates {
		if !isAvailableCandidate(c) {
			continue
		}
		if brand != "" {
			nameNorm := normalizeText(c.name)
			if !strings.HasPrefix(nameNorm, brand+" ") {
				continue
			}
		}
		alts = append(alts, c)
	}
	sort.SliceStable(alts, func(i, j int) bool {
		si := scoreCatalogCandidate(requested, "", alts[i])
		sj := scoreCatalogCandidate(requested, "", alts[j])
		if si == sj {
			return strings.ToLower(alts[i].name) < strings.ToLower(alts[j].name)
		}
		return si > sj
	})
	if len(alts) > 3 {
		alts = alts[:3]
	}
	return requested, alts, true
}

func buildUnavailableWithAlternativesReply(requested string, alternatives []availabilityCandidate) string {
	req := strings.TrimSpace(requested)
	if req == "" {
		req = "that model"
	}

	// 🆕 Extract the brand name from the requested item for a more natural response
	brandName := ""
	if parts := strings.Fields(req); len(parts) > 0 {
		brandName = parts[0]
	}

	if len(alternatives) == 0 {
		// No same-brand alternatives — this might be a brand we don't carry at all.
		// Don't randomly push a different brand. Acknowledge and invite them to see what's available.
		if brandName != "" {
			return fmt.Sprintf(
				"The %s isn't something we stock. Want me to show you what we have available?",
				req,
			)
		}
		return "We don't carry that one. Want me to show you what we have?"
	}

	// Same-brand alternatives exist — list them naturally
	lines := make([]string, 0, len(alternatives))
	for _, alt := range alternatives {
		price := formatCurrency(alt.price, alt.currency)
		if price == "" {
			lines = append(lines, alt.name)
			continue
		}
		lines = append(lines, fmt.Sprintf("%s (%s)", alt.name, price))
	}

	if len(lines) == 1 {
		return fmt.Sprintf("We don't have the %s, but we've got the %s. Want more details?", req, lines[0])
	}
	return fmt.Sprintf("We don't have the %s, but we've got: %s. Which one interests you?", req, strings.Join(lines, ", "))
}

func isDirectSpecQuestion(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	if !strings.Contains(msg, "?") && !strings.Contains(msg, "is ") && !strings.Contains(msg, "does ") {
		return false
	}
	keywords := []string{
		"sim", "esim", "physical sim", "dual sim",
		"storage", "ram", "camera", "battery", "chip", "screen", "display", "refresh rate",
	}
	for _, key := range keywords {
		if strings.Contains(msg, key) {
			return true
		}
	}
	return false
}

func buildSpecPriorityReply(message string, focusItem string, knowledge *v4ctx.TenantKnowledgeSnapshot, orderSnapshot *v4ctx.OrderSnapshot) string {
	if knowledge == nil {
		return ""
	}
	target := strings.TrimSpace(focusItem)
	if target == "" && orderSnapshot != nil && len(orderSnapshot.Items) > 0 {
		target = strings.TrimSpace(orderSnapshot.Items[0].Name)
	}
	if target == "" {
		return ""
	}
	candidate := resolveAvailabilityCandidate(target, target, knowledge)
	if candidate == nil {
		candidate = resolveAvailabilityCandidate(message, target, knowledge)
	}
	if candidate == nil || strings.TrimSpace(candidate.name) == "" {
		return ""
	}
	desc := strings.TrimSpace(candidate.description)
	if desc == "" {
		return fmt.Sprintf("I don't have that specific spec detail for %s in the catalog yet.", candidate.name)
	}
	return fmt.Sprintf("%s specs from our catalog: %s", candidate.name, desc)
}

func isCloseoutMessage(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	phrases := []string{"no thank you", "no thanks", "not interested", "that's all", "that is all", "no thank", "i'm good", "im good"}
	for _, p := range phrases {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func isReopenBuyingIntent(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	phrases := []string{"buy", "price", "order", "catalog", "available", "switch to", "i want", "need a", "how much"}
	for _, p := range phrases {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func markCloseout(convSnapshot *v4ctx.ConversationSnapshot) {
	if convSnapshot == nil {
		return
	}
	convSnapshot.CloseoutState = "closed"
	convSnapshot.CloseoutMessageCount++
	convSnapshot.LastCloseoutAt = time.Now().UTC().Format(time.RFC3339)
}
