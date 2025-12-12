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
