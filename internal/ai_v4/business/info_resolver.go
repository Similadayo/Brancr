package business

import (
	"fmt"
	"strings"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
)

type BusinessInfo struct {
	Summary   string
	Offerings []string
	Services  []string
	HasMenu   bool
}

// ResolveBusinessInfo extracts human-readable facts from the context snapshot.
// It reads ONLY from DB data (ContextSnapshot).
func ResolveBusinessInfo(ctx *v4ctx.ContextSnapshot) BusinessInfo {
	info := BusinessInfo{
		Summary: ctx.Business.Description,
	}

	if info.Summary == "" {
		info.Summary = "providing authorized products and services"
	}

	for _, item := range ctx.Offerings {
		if !item.IsAvailable {
			continue
		}

		if item.Type == "menu_item" || item.Type == "product" {
			info.HasMenu = true
			if len(info.Offerings) < 5 { // Limit examples
				info.Offerings = append(info.Offerings, item.Name)
			}
		} else if item.Type == "service" {
			if len(info.Services) < 5 { // Limit examples
				info.Services = append(info.Services, item.Name)
			}
		}
	}

	return info
}

// BuildBusinessReply constructs a deterministic reply based on facts.
// LLM only polishes this result, it does not invent it.
func BuildBusinessReply(info BusinessInfo) string {
	// Strategy 1: Service Business (Explicit Services)
	if len(info.Services) > 0 {
		return fmt.Sprintf(
			"We distinguish ourselves by offering %s. Let me know what you'd like to learn more about \U0001F60A",
			strings.Join(info.Services, ", "),
		)
	}

	// Strategy 2: Product/Menu Business
	if info.HasMenu {
		if len(info.Offerings) > 0 {
			// Show a few examples
			examples := info.Offerings
			if len(examples) > 3 {
				examples = examples[:3]
			}
			return fmt.Sprintf(
				"We have a menu including %s. Would you like to see the full options?",
				strings.Join(examples, ", "),
			)
		}
		return "We have a menu with several options. Would you like to see it?"
	}

	// Strategy 3: Generic Summary
	return fmt.Sprintf("We specialize in %s. How can I assist you today?", info.Summary)
}
