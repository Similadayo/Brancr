package catalog

import (
	"regexp"
	"sort"
	"strings"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"go.uber.org/zap"
)

var logger *zap.Logger
var specialCharsRegex *regexp.Regexp

func init() {
	logger, _ = zap.NewProduction()
	// Keep letters/numbers and collapse other chars into spaces.
	specialCharsRegex = regexp.MustCompile(`[^a-zA-Z0-9]+`)
}

// Stop words that are frequently used by customers but might not be in the official item name
var stopWords = map[string]bool{
	"plan":         true,
	"package":      true,
	"service":      true,
	"bundle":       true,
	"option":       true,
	"offer":        true,
	"price":        true,
	"cost":         true,
	"rate":         true,
	"monthly":      true,
	"yearly":       true,
	"subscription": true,
}

// normalizeForMatching strips punctuation and normalizes spacing.
func normalizeForMatching(s string) string {
	normalized := strings.ToLower(s)
	normalized = specialCharsRegex.ReplaceAllString(normalized, " ")
	normalized = strings.Join(strings.Fields(normalized), " ")
	return strings.TrimSpace(normalized)
}

// tokenize returns significant words from the string (excluding stop words).
func tokenize(s string) []string {
	words := strings.Fields(normalizeForMatching(s))
	var tokens []string
	for _, w := range words {
		if isNumericToken(w) || (!stopWords[w] && len(w) > 2) {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

func isNumericToken(tok string) bool {
	for _, r := range tok {
		if r < '0' || r > '9' {
			return false
		}
	}
	return tok != ""
}

// matchTokens checks if all significant query tokens are present in the candidate.
func matchTokens(queryTokens []string, candidate string) bool {
	if len(queryTokens) == 0 {
		return false
	}
	candidateTokens := tokenize(candidate)
	for _, q := range queryTokens {
		if !tokenMatchesCandidate(q, candidateTokens) {
			return false
		}
	}
	return true
}

func levenshteinDistance(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min(del, min(ins, sub))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func tokenMatchesCandidate(queryToken string, candidateTokens []string) bool {
	for _, ct := range candidateTokens {
		if queryToken == ct {
			return true
		}
		if isNumericToken(queryToken) || isNumericToken(ct) {
			continue
		}
		if strings.Contains(ct, queryToken) || strings.Contains(queryToken, ct) {
			return true
		}
		maxDist := 1
		if len(queryToken) >= 6 {
			maxDist = 2
		}
		if levenshteinDistance(queryToken, ct) <= maxDist {
			return true
		}
	}
	return false
}

func scoreCatalogNameMatch(rawNormalized string, queryTokens []string, candidateName string) int {
	candidateNorm := normalizeForMatching(candidateName)
	if candidateNorm == "" {
		return 0
	}

	score := 0
	switch {
	case rawNormalized == candidateNorm:
		score += 1000
	case strings.Contains(rawNormalized, candidateNorm):
		score += 700 + len(candidateNorm)
	case strings.Contains(candidateNorm, rawNormalized) && rawNormalized != "":
		score += 550 + len(rawNormalized)
	}

	candidateTokens := tokenize(candidateNorm)
	matched := 0
	for _, qt := range queryTokens {
		if tokenMatchesCandidate(qt, candidateTokens) {
			matched++
			if isNumericToken(qt) {
				score += 180
			} else {
				score += 95
			}
		} else if isNumericToken(qt) {
			// Explicit model/storage mismatch should strongly reduce confidence.
			score -= 250
		}
	}

	if len(queryTokens) > 0 {
		coverage := float64(matched) / float64(len(queryTokens))
		if coverage >= 1 {
			score += 200
		} else if coverage >= 0.75 {
			score += 120
		} else if coverage >= 0.5 {
			score += 40
		}
	}

	return score
}

type scoredCatalogCandidate struct {
	score int
	item  v4ctx.MenuItemSummary
}

func rankedCatalogCandidates(raw string, knowledge *v4ctx.TenantKnowledgeSnapshot) []scoredCatalogCandidate {
	rawNormalized := normalizeForMatching(raw)
	queryTokens := tokenize(rawNormalized)
	if rawNormalized == "" {
		return nil
	}

	var ranked []scoredCatalogCandidate
	for _, item := range knowledge.MenuItems {
		s := scoreCatalogNameMatch(rawNormalized, queryTokens, item.Name)
		if s <= 0 {
			continue
		}
		ranked = append(ranked, scoredCatalogCandidate{
			score: s,
			item: v4ctx.MenuItemSummary{
				Name:        item.Name,
				Description: item.Description,
				Price:       item.Price,
				Currency:    item.Currency,
			},
		})
	}
	for _, prod := range knowledge.Products {
		s := scoreCatalogNameMatch(rawNormalized, queryTokens, prod.Name)
		if s <= 0 {
			continue
		}
		ranked = append(ranked, scoredCatalogCandidate{
			score: s,
			item: v4ctx.MenuItemSummary{
				Name:        prod.Name,
				Description: prod.Description,
				Price:       prod.Price,
				Currency:    prod.Currency,
			},
		})
	}
	for _, svc := range knowledge.Services {
		s := scoreCatalogNameMatch(rawNormalized, queryTokens, svc.Name)
		if s <= 0 {
			continue
		}
		ranked = append(ranked, scoredCatalogCandidate{
			score: s,
			item: v4ctx.MenuItemSummary{
				Name:        svc.Name,
				Description: svc.Description,
				Price:       svc.Price,
				Currency:    "NGN",
			},
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return len(ranked[i].item.Name) > len(ranked[j].item.Name)
		}
		return ranked[i].score > ranked[j].score
	})
	return ranked
}

// ResolveLongestMatch finds best catalog match for raw user input.
// This is the authoritative catalog resolver; Python only extracts raw text.
func ResolveLongestMatch(raw string, knowledge *v4ctx.TenantKnowledgeSnapshot) *v4ctx.MenuItemSummary {
	if raw == "" || knowledge == nil {
		return nil
	}

	rawNormalized := normalizeForMatching(raw)

	logger.Info("catalog_matching_attempt",
		zap.String("raw", raw),
		zap.String("normalized", rawNormalized),
		zap.Int("menu_items_count", len(knowledge.MenuItems)),
		zap.Int("products_count", len(knowledge.Products)))

	ranked := rankedCatalogCandidates(raw, knowledge)
	if len(ranked) == 0 || ranked[0].score < 220 {
		logger.Warn("no_catalog_match",
			zap.String("raw", raw),
			zap.String("normalized", rawNormalized),
			zap.Int("menu_items_checked", len(knowledge.MenuItems)))
		return nil
	}

	best := ranked[0]
	logger.Info("catalog_match_found",
		zap.String("raw", raw),
		zap.String("matched", best.item.Name),
		zap.Int("score", best.score))
	return &best.item
}
