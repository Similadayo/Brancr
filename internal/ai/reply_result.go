package ai

// ReplyResult contains the AI-generated reply and any associated media
type ReplyResult struct {
	Text      string   // The text reply
	ImageURLs []string // Image URLs to send with the reply (if any)
}
