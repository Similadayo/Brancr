package ai

import (
	"fmt"
	"strings"
)

// BuildIndustrySalesPrompt builds industry-specific sales guidance for AI prompts
func BuildIndustrySalesPrompt(industryName string, salesStage string, commitmentLevel string, hasNegotiation bool) string {
	var guidance strings.Builder

	guidance.WriteString(fmt.Sprintf("SALES CONTEXT:\n"))
	guidance.WriteString(fmt.Sprintf("Industry: %s\n", industryName))
	guidance.WriteString(fmt.Sprintf("Sales Stage: %s\n", salesStage))
	guidance.WriteString(fmt.Sprintf("Customer Commitment: %s\n", commitmentLevel))

	switch industryName {
	case "E-commerce":
		guidance.WriteString("\nE-COMMERCE SALES APPROACH:\n")
		guidance.WriteString("- Focus on product features, quality, and value\n")
		guidance.WriteString("- Highlight benefits and use cases\n")
		guidance.WriteString("- Offer quantity discounts for bulk purchases\n")
		guidance.WriteString("- Emphasize fast shipping and delivery options\n")
		guidance.WriteString("- Be ready to negotiate on quantity or bundle deals\n")
		guidance.WriteString("- Show product images/details when relevant\n")
		
		if salesStage == "negotiation" {
			guidance.WriteString("\nNEGOTIATION TIPS:\n")
			guidance.WriteString("- Be flexible on quantity discounts\n")
			guidance.WriteString("- Offer bundle deals (buy 2 get 1 free, etc.)\n")
			guidance.WriteString("- Highlight value over price\n")
			guidance.WriteString("- Don't discount too aggressively - maintain value perception\n")
		}

		if commitmentLevel == "committed" {
			guidance.WriteString("\nCOMMITMENT CONFIRMATION:\n")
			guidance.WriteString("- Confirm order details clearly\n")
			guidance.WriteString("- Provide payment instructions\n")
			guidance.WriteString("- Set delivery expectations\n")
			guidance.WriteString("- Thank them warmly for their purchase\n")
		}

	case "Restaurant":
		guidance.WriteString("\nRESTAURANT SALES APPROACH:\n")
		guidance.WriteString("- Recommend dishes based on preferences\n")
		guidance.WriteString("- Highlight popular items and specials\n")
		guidance.WriteString("- Suggest add-ons or combos naturally\n")
		guidance.WriteString("- Be warm and welcoming, like a server would be\n")
		guidance.WriteString("- Mention preparation time if relevant\n")
		guidance.WriteString("- Ask about dietary preferences or restrictions\n")
		
		if salesStage == "negotiation" {
			guidance.WriteString("\nNEGOTIATION TIPS:\n")
			guidance.WriteString("- Offer combo deals or meal packages\n")
			guidance.WriteString("- Suggest sharing plates for groups\n")
			guidance.WriteString("- Highlight value of set meals\n")
			guidance.WriteString("- Be flexible on portion sizes or add-ons\n")
		}

		if commitmentLevel == "committed" {
			guidance.WriteString("\nCOMMITMENT CONFIRMATION:\n")
			guidance.WriteString("- Confirm order items clearly\n")
			guidance.WriteString("- Confirm delivery/pickup preference\n")
			guidance.WriteString("- Provide payment instructions\n")
			guidance.WriteString("- Give estimated preparation/delivery time\n")
		}

	case "Consultant", "Agency":
		guidance.WriteString("\nCONSULTANT/AGENCY SALES APPROACH:\n")
		guidance.WriteString("- Answer their questions directly first - don't ask questions until you've answered what they asked\n")
		guidance.WriteString("- Focus on value and ROI, not just price\n")
		guidance.WriteString("- Present packages and deliverables clearly when asked\n")
		guidance.WriteString("- Build trust through helpful, direct answers\n")
		guidance.WriteString("- Address concerns professionally\n")
		guidance.WriteString("- Show how you solve their specific problem - but only when relevant\n")
		guidance.WriteString("- Only ask questions if you genuinely need more info to help them (e.g., budget questions only if they ask about pricing)\n")
		
		if salesStage == "negotiation" {
			guidance.WriteString("\nNEGOTIATION TIPS:\n")
			guidance.WriteString("- Focus on value delivered, not just price\n")
			guidance.WriteString("- Offer flexible payment terms if appropriate\n")
			guidance.WriteString("- Adjust scope to fit budget (if possible)\n")
			guidance.WriteString("- Emphasize long-term value and results\n")
		}

		if commitmentLevel == "committed" {
			guidance.WriteString("\nCOMMITMENT CONFIRMATION:\n")
			guidance.WriteString("- Confirm service package and deliverables\n")
			guidance.WriteString("- Set clear timeline and milestones\n")
			guidance.WriteString("- Provide payment instructions\n")
			guidance.WriteString("- Set next steps and expectations\n")
		}

	default:
		guidance.WriteString("\nGENERAL SALES APPROACH:\n")
		guidance.WriteString("- Be helpful and informative\n")
		guidance.WriteString("- Answer questions clearly\n")
		guidance.WriteString("- Build rapport naturally\n")
		guidance.WriteString("- Guide toward commitment when appropriate\n")
	}

	if hasNegotiation {
		guidance.WriteString("\nCURRENT NEGOTIATION:\n")
		guidance.WriteString("- Listen to customer's concerns\n")
		guidance.WriteString("- Find win-win solutions\n")
		guidance.WriteString("- Be flexible but maintain value\n")
		guidance.WriteString("- Don't give away too much - find middle ground\n")
	}

	return guidance.String()
}

// BuildSalesStageGuidance provides specific guidance based on sales stage
func BuildSalesStageGuidance(stage string, industryName string) string {
	switch stage {
	case "greeting":
		return "Welcome the customer warmly. Introduce yourself and the business. Ask how you can help them today. Be friendly and approachable."

	case "product_inquiry":
		if industryName == "E-commerce" {
			return "Provide detailed product information. Highlight key features and benefits. Show enthusiasm about the products. Ask what they're looking for specifically."
		} else if industryName == "Restaurant" {
			return "Recommend dishes based on their preferences. Describe items appetizingly. Mention popular choices. Ask about dietary preferences or restrictions."
		} else {
			return "Explain your services clearly. Ask about their specific needs. Show how you can help solve their problem. Be consultative and helpful."
		}

	case "negotiation":
		return "Listen to their concerns. Be flexible but maintain value. Find creative solutions. Don't discount too aggressively. Focus on win-win outcomes."

	case "commitment_confirmation":
		return "Confirm their interest clearly. Summarize what they want. Address any final concerns. Guide them toward making the purchase decision."

	case "payment_instructions":
		return "Provide clear payment instructions. Confirm the total amount. Give them all necessary details. Thank them for their purchase. Set delivery/fulfillment expectations."

	default:
		return "Continue the conversation naturally. Be helpful and guide them toward their goal."
	}
}

