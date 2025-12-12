package social

import (
	"context"
	"testing"

	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
)

func TestShouldEscalateToHuman_AccountDetails_NoEscalation(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Please send your account number", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "account", Tone: "neutral", Confidence: 0.9}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for account details request")
	}
}

func TestShouldEscalateToHuman_PaymentConfirmation_Escalates(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "I have paid", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "payment", Tone: "neutral", Confidence: 0.9}

	should, reason := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if !should {
		t.Fatalf("expected escalation for payment confirmation")
	}
	if reason == "" {
		t.Fatalf("expected a non-empty escalation reason")
	}
}

func TestShouldEscalateToHuman_PaymentConfirmation_Escalates_EvenIfIntentOrder(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "I already made the payment 50000", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "order", Tone: "neutral", Confidence: 0.9}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if !should {
		t.Fatalf("expected escalation for payment confirmation even when intent=order")
	}
}

func TestShouldEscalateToHuman_PayOnDelivery_NoEscalation(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "pay on delivery", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.9}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if !should {
		t.Fatalf("expected escalation for pay-on-delivery approval")
	}
}

func TestShouldEscalateToHuman_PayOnDelivery_Inquiry_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Do you do pay on delivery?", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.9}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for POD inquiry")
	}
}

func TestShouldEscalateToHuman_InvoiceRequest_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Can I get an invoice?", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "billing", Tone: "neutral", Confidence: 0.9}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for invoice request")
	}
}

func TestShouldEscalateToHuman_ReceiptRequest_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Please send receipt", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "billing", Tone: "neutral", Confidence: 0.9}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for receipt request")
	}
}

func TestShouldEscalateToHuman_PaymentConfirmationWithReceipt_Escalates(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "I have paid, please send receipt", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "billing", Tone: "neutral", Confidence: 0.9}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if !should {
		t.Fatalf("expected escalation for payment confirmation even if receipt is mentioned")
	}
}

func TestShouldEscalateToHuman_PriceQuestion_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "How much is this?", MessageType: "text"}
	// Even if the detector mislabels this as payment, we should not escalate without a payment-confirmation signal.
	intent := &ai.DetectedIntent{Intent: "payment", Tone: "neutral", Confidence: 0.95}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for price question")
	}
}

func TestShouldEscalateToHuman_OrderOptionSelection_DoesNotEscalate(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "I will like to go for 1", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "order", Tone: "neutral", Confidence: 0.9}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, ai.ExtractEvents(msg.Content), nil)
	if should {
		t.Fatalf("expected no escalation for simple option selection")
	}
}

func TestShouldEscalateToHuman_StatefulFollowup_PaymentConfirmationStage_Escalates(t *testing.T) {
	h := &MessageHandler{}
	msg := IncomingMessage{Content: "Any update?", MessageType: "text"}
	intent := &ai.DetectedIntent{Intent: "inquiry", Tone: "neutral", Confidence: 0.9}
	salesConv := &storage.SalesConversation{Stage: "payment_confirmation"}

	should, _ := h.shouldEscalateToHuman(context.Background(), msg, &storage.Customer{}, intent, 0, nil, salesConv)
	if !should {
		t.Fatalf("expected escalation for follow-up when stage=payment_confirmation")
	}
}
