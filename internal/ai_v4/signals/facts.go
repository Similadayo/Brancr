package signals

// SemanticFacts represents purely observational facts extracted from the user's message.
// It answers "what did they mention?" not "what do they mean?".
type SemanticFacts struct {
	MentionsProduct      bool
	MentionsQuantity     bool
	MentionsPrice        bool
	MentionsPayment      bool
	MentionsConfirmation bool
	MentionsHuman        bool
	MentionsComplaint    bool
	MentionsRefund       bool

	Confidence float64 // 0.0 to 1.0
}
