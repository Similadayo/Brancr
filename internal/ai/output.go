package ai

// GPTOutput defines the rigid JSON schema the LLM must strictly adhere to.
type GPTOutput struct {
	MessageToUser   string  `json:"message_to_user"`
	SuggestedAction string  `json:"suggested_action"`
	Confidence      float64 `json:"confidence"`
}
