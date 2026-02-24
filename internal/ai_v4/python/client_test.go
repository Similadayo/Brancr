package python

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAnalyzeTurn_DegradesGracefullyOn502(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/analyze-turn" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
			return
		}
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	out, err := c.AnalyzeTurn(context.Background(), &AnalyzeTurnRequest{
		TenantID: uuid.New(),
		Message:  "how much is iphone 13",
	})
	if err != nil {
		t.Fatalf("expected graceful fallback on 502, got error: %v", err)
	}
	if out == nil {
		t.Fatalf("expected fallback response, got nil")
	}
	if out.Decision.Stage != "fallback" {
		t.Fatalf("expected fallback stage, got %q", out.Decision.Stage)
	}
	if !strings.Contains(strings.ToLower(out.Reply.Text), "brief delay") {
		t.Fatalf("expected fallback text, got %q", out.Reply.Text)
	}
}

func TestAnalyzeTurn_UsesSimplifiedLayerWhenPrimaryFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/analyze-turn" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
			return
		}
		if r.URL.Path == "/api/v4/analyze-turn-simplified" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"decision":{"domain":"general","stage":"general_chat","confidence":0.9},
				"reply":{"text":"Simplified fallback reply","mode":"fallback_simplified"},
				"actions":[],
				"conversation_update":{"stage":"general_chat","last_domain":"general","turn_count_increment":1},
				"semantic":{
					"items":[],
					"delivery_intent":"",
					"address":"",
					"ambiguities":[],
					"cancel_intent":false,
					"detected_intent":"general",
					"customer_offer":0,
					"target_item":"",
					"buying_confidence":0,
					"message_type":"general",
					"intent_confidence":0.9,
					"is_question":false
				},
				"llm_meta":{"model":"simplified_fallback","token_usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}
			}`))
			return
		}
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	out, err := c.AnalyzeTurn(context.Background(), &AnalyzeTurnRequest{
		TenantID: uuid.New(),
		Message:  "hello",
	})
	if err != nil {
		t.Fatalf("expected simplified fallback without error, got: %v", err)
	}
	if out == nil {
		t.Fatalf("expected response, got nil")
	}
	if out.Reply.Mode != "fallback_simplified" {
		t.Fatalf("expected fallback_simplified mode, got %q", out.Reply.Mode)
	}
	if out.Reply.Text != "Simplified fallback reply" {
		t.Fatalf("unexpected reply text: %q", out.Reply.Text)
	}
}
