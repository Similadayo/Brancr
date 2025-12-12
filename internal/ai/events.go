package ai

import "strings"

type EventType string

const (
	EventPaymentConfirmation EventType = "payment_confirmation"
	EventPaymentInquiry      EventType = "payment_inquiry"
	EventInvoiceRequest      EventType = "invoice_request"
	EventPODCommitment       EventType = "pay_on_delivery_commitment"
	EventOrderSelection      EventType = "order_selection"
	EventHumanRequest        EventType = "human_request"
	EventRefundPolicy        EventType = "refund_policy"
	EventRefundDemand        EventType = "refund_demand"
)

type Event struct {
	Type EventType `json:"type"`
}

// ExtractEvents derives a small set of high-signal events from raw message text.
// This is intentionally deterministic and conservative.
func ExtractEvents(message string) []Event {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return nil
	}

	var events []Event

	// Explicit human request.
	if strings.Contains(lower, "speak to") ||
		strings.Contains(lower, "talk to") ||
		strings.Contains(lower, "human") ||
		strings.Contains(lower, "person") ||
		strings.Contains(lower, "manager") ||
		strings.Contains(lower, "owner") ||
		strings.Contains(lower, "real person") ||
		strings.Contains(lower, "actual person") ||
		strings.Contains(lower, "get me someone") ||
		strings.Contains(lower, "connect me") {
		events = append(events, Event{Type: EventHumanRequest})
	}

	// Payment confirmation (customer claims they've paid).
	// Keep this aligned with existing escalation behavior.
	if strings.Contains(lower, "paid") ||
		strings.Contains(lower, "sent payment") ||
		strings.Contains(lower, "transferred") ||
		strings.Contains(lower, "payment made") ||
		strings.Contains(lower, "made the payment") ||
		strings.Contains(lower, "made payment") ||
		strings.Contains(lower, "payment done") ||
		strings.Contains(lower, "i have sent") ||
		strings.Contains(lower, "i sent") ||
		strings.Contains(lower, "already paid") {
		events = append(events, Event{Type: EventPaymentConfirmation})
	}

	// Pay-on-delivery commitment (not a question).
	if strings.Contains(lower, "pay on delivery") || strings.Contains(lower, "cash on delivery") || strings.Contains(lower, "pod") {
		isInquiry := strings.Contains(lower, "do you") || strings.Contains(lower, "can i") || strings.Contains(lower, "can we") ||
			strings.Contains(lower, "is it") || strings.Contains(lower, "is there") || strings.Contains(lower, "available") ||
			strings.Contains(lower, "possible") || strings.Contains(lower, "how") || strings.Contains(lower, "?")
		if !isInquiry {
			events = append(events, Event{Type: EventPODCommitment})
		}
	}

	// Payment inquiry (asking for account/bank details).
	if strings.Contains(lower, "how to pay") ||
		strings.Contains(lower, "payment method") ||
		strings.Contains(lower, "where to send") ||
		strings.Contains(lower, "account number") ||
		strings.Contains(lower, "bank details") ||
		strings.Contains(lower, "payment details") ||
		strings.Contains(lower, "account to pay") ||
		strings.Contains(lower, "can i get the account") ||
		strings.Contains(lower, "send account") {
		events = append(events, Event{Type: EventPaymentInquiry})
	}

	// Invoice/receipt requests are informational by default.
	if strings.Contains(lower, "invoice") || strings.Contains(lower, "receipt") || strings.Contains(lower, "billing") {
		events = append(events, Event{Type: EventInvoiceRequest})
	}

	// Refund-related: distinguish informational policy questions vs demanding a refund.
	isRefundRelated := strings.Contains(lower, "refund") || strings.Contains(lower, "return") || strings.Contains(lower, "money back")
	if isRefundRelated {
		isDemand := strings.Contains(lower, "want") || strings.Contains(lower, "need") || strings.Contains(lower, "demand") ||
			strings.Contains(lower, "right now") || strings.Contains(lower, "now") || strings.Contains(lower, "immediately") ||
			strings.Contains(lower, "refund me") || strings.Contains(lower, "return my money")
		isPolicy := strings.Contains(lower, "policy") || strings.Contains(lower, "?") || strings.Contains(lower, "what") || strings.Contains(lower, "how")
		if isDemand {
			events = append(events, Event{Type: EventRefundDemand})
		} else if isPolicy {
			events = append(events, Event{Type: EventRefundPolicy})
		}
	}

	// Simple option selection / order selection.
	trimmed := strings.TrimSpace(lower)
	isBareOption := trimmed == "1" || trimmed == "2" || trimmed == "3" || trimmed == "4" || trimmed == "5"
	orderCue := strings.Contains(lower, "i'll take") || strings.Contains(lower, "i will take") || strings.Contains(lower, "i want one") ||
		strings.Contains(lower, "add one") || strings.Contains(lower, "go for") || strings.Contains(lower, "option ")
	if isBareOption || orderCue {
		events = append(events, Event{Type: EventOrderSelection})
	}

	return dedupeEvents(events)
}

func dedupeEvents(in []Event) []Event {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[EventType]struct{}, len(in))
	out := make([]Event, 0, len(in))
	for _, e := range in {
		if _, ok := seen[e.Type]; ok {
			continue
		}
		seen[e.Type] = struct{}{}
		out = append(out, e)
	}
	return out
}
