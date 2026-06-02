package llm

// WithModel returns a shallow copy of the client with a different model
// This allows per-request model selection without race conditions
func (c *Client) WithModel(model string) *Client {
	newClient := *c // Shallow copy
	newClient.Model = model
	return &newClient
}
