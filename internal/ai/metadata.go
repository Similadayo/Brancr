package ai

import "encoding/json"

// AttachEventsToMetadata stores extracted events into an existing metadata map
// under a reserved namespace key.
func AttachEventsToMetadata(metadata map[string]interface{}, events []Event) {
	if metadata == nil {
		return
	}
	if len(events) == 0 {
		return
	}

	ns, _ := metadata["brancr_ai"].(map[string]interface{})
	if ns == nil {
		ns = make(map[string]interface{})
	}

	ns["events_v"] = 1
	ns["events"] = events

	metadata["brancr_ai"] = ns
}

// MergeMetadataJSON merges an update object into an existing metadata JSON string.
// If existing is empty/invalid, it treats it as an empty object.
func MergeMetadataJSON(existing string, update map[string]interface{}) (string, error) {
	base := make(map[string]interface{})
	if existing != "" && existing != "{}" {
		_ = json.Unmarshal([]byte(existing), &base)
	}
	if base == nil {
		base = make(map[string]interface{})
	}
	for k, v := range update {
		base[k] = v
	}
	out, err := json.Marshal(base)
	if err != nil {
		return existing, err
	}
	return string(out), nil
}

type metadataEnvelope struct {
	BrancrAI struct {
		Events []Event `json:"events"`
		// ExpectedAmount is the amount the system expects the customer to pay for the current purchase.
		// Stored to reliably link payment confirmations that contain no amount.
		ExpectedAmount   float64 `json:"expected_amount"`
		ExpectedCurrency string  `json:"expected_currency"`
	} `json:"brancr_ai"`
}

func EventsFromMetadataJSON(metadataJSON string) []Event {
	if metadataJSON == "" || metadataJSON == "{}" {
		return nil
	}
	var env metadataEnvelope
	if err := json.Unmarshal([]byte(metadataJSON), &env); err != nil {
		return nil
	}
	return env.BrancrAI.Events
}

func MetadataHasEvent(metadataJSON string, eventType EventType) bool {
	for _, e := range EventsFromMetadataJSON(metadataJSON) {
		if e.Type == eventType {
			return true
		}
	}
	return false
}

// AttachExpectedAmountToMetadata stores expected payment amount (and optional currency) into metadata.
func AttachExpectedAmountToMetadata(metadata map[string]interface{}, amount float64, currency string) {
	if metadata == nil {
		return
	}
	if amount <= 0 {
		return
	}

	ns, _ := metadata["brancr_ai"].(map[string]interface{})
	if ns == nil {
		ns = make(map[string]interface{})
	}

	ns["expected_amount"] = amount
	if currency != "" {
		ns["expected_currency"] = currency
	}
	metadata["brancr_ai"] = ns
}

// ExpectedAmountFromMetadataJSON reads the expected amount from metadata.
func ExpectedAmountFromMetadataJSON(metadataJSON string) (amount float64, currency string, ok bool) {
	if metadataJSON == "" || metadataJSON == "{}" {
		return 0, "", false
	}
	var env metadataEnvelope
	if err := json.Unmarshal([]byte(metadataJSON), &env); err != nil {
		return 0, "", false
	}
	if env.BrancrAI.ExpectedAmount <= 0 {
		return 0, "", false
	}
	cur := env.BrancrAI.ExpectedCurrency
	if cur == "" {
		cur = "NGN"
	}
	return env.BrancrAI.ExpectedAmount, cur, true
}
