package bot

import (
	"regexp"
	"strings"
)

// EscalationType represents different escalation categories
type EscalationType string

const (
	TypePayment   EscalationType = "payment"
	TypeComplaint EscalationType = "complaint"
	TypeBooking   EscalationType = "booking"
	TypeInquiry   EscalationType = "inquiry"
	TypeGeneral   EscalationType = "general"
)

// DetectEscalationType analyzes message to determine escalation type
func DetectEscalationType(message string, hasMedia bool) EscalationType {
	lower := strings.ToLower(message)

	// Payment indicators
	paymentKeywords := []string{
		"paid", "payment", "transfer", "sent money", "deposited",
		"receipt", "proof of payment", "bank transfer", "₦", "naira",
	}
	for _, kw := range paymentKeywords {
		if strings.Contains(lower, kw) || hasMedia {
			return TypePayment
		}
	}

	// Complaint indicators
	complaintKeywords := []string{
		"complaint", "problem", "issue", "wrong", "broken", "damaged",
		"refund", "return", "disappointed", "unhappy", "terrible",
		"angry", "frustrated", "not working", "doesn't work",
	}
	for _, kw := range complaintKeywords {
		if strings.Contains(lower, kw) {
			return TypeComplaint
		}
	}

	// Booking indicators
	bookingKeywords := []string{
		"book", "appointment", "schedule", "reserve", "consultation",
		"available", "slot", "time", "date", "meeting", "session",
	}
	for _, kw := range bookingKeywords {
		if strings.Contains(lower, kw) {
			return TypeBooking
		}
	}

	// Inquiry (question)
	if strings.Contains(lower, "?") ||
		strings.HasPrefix(lower, "how") ||
		strings.HasPrefix(lower, "what") ||
		strings.HasPrefix(lower, "when") {
		return TypeInquiry
	}

	return TypeGeneral
}

// EscalationContext holds extracted context for display
type EscalationContext struct {
	Type             EscalationType
	Summary          string   // One-line summary
	KeyDetails       []string // Bullet points for right panel
	SuggestedAction  string   // Primary action button label
	SecondaryActions []string // Other available actions
}

// ExtractContext extracts display context from escalation
func ExtractContext(message string, escType EscalationType) EscalationContext {
	ctx := EscalationContext{Type: escType}

	switch escType {
	case TypePayment:
		ctx.Summary = "Customer claims payment made"
		ctx.KeyDetails = extractPaymentDetails(message)
		ctx.SuggestedAction = "Approve Payment"
		ctx.SecondaryActions = []string{"Ask for Details", "Not Found"}

	case TypeComplaint:
		ctx.Summary = "Customer has a complaint"
		ctx.KeyDetails = extractComplaintDetails(message)
		ctx.SuggestedAction = "Mark Resolved"
		ctx.SecondaryActions = []string{"Send Reply", "Escalate to Admin"}

	case TypeBooking:
		ctx.Summary = "Customer wants to book"
		ctx.KeyDetails = extractBookingDetails(message)
		ctx.SuggestedAction = "Confirm Booking"
		ctx.SecondaryActions = []string{"Propose Time", "Decline"}

	default:
		ctx.Summary = "Customer message needs attention"
		ctx.SuggestedAction = "Send Reply"
		ctx.SecondaryActions = []string{"Send AI Reply", "Ignore"}
	}

	return ctx
}

func extractPaymentDetails(message string) []string {
	details := []string{}

	// Extract amount if present
	amountPattern := regexp.MustCompile(`[₦$]\s*[\d,]+(?:\.\d{2})?|\d+(?:,\d{3})*(?:\.\d{2})?\s*(?:naira|NGN|USD)`)
	if match := amountPattern.FindString(message); match != "" {
		details = append(details, "Amount: "+match)
	}

	// Check for receipt/proof
	if strings.Contains(strings.ToLower(message), "receipt") ||
		strings.Contains(strings.ToLower(message), "proof") {
		details = append(details, "Has proof/receipt attached")
	}

	return details
}

func extractComplaintDetails(message string) []string {
	details := []string{}
	lower := strings.ToLower(message)

	if strings.Contains(lower, "refund") {
		details = append(details, "Requesting refund")
	}
	if strings.Contains(lower, "damaged") || strings.Contains(lower, "broken") {
		details = append(details, "Product damaged/broken")
	}
	if strings.Contains(lower, "wrong") {
		details = append(details, "Wrong item received")
	}
	if strings.Contains(lower, "late") || strings.Contains(lower, "delay") {
		details = append(details, "Delivery delay issue")
	}

	return details
}

func extractBookingDetails(message string) []string {
	details := []string{}
	lower := strings.ToLower(message)

	// Extract time mentions
	timePattern := regexp.MustCompile(`\d{1,2}:\d{2}\s*(?:am|pm)?|\d{1,2}\s*(?:am|pm)`)
	if match := timePattern.FindString(lower); match != "" {
		details = append(details, "Requested time: "+match)
	}

	// Extract date mentions
	datePattern := regexp.MustCompile(`(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday|tomorrow|today|\d{1,2}[/-]\d{1,2})`)
	if match := datePattern.FindString(lower); match != "" {
		details = append(details, "Requested date: "+match)
	}

	return details
}
