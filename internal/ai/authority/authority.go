package authority

type ResponseAuthority struct {
	// What the AI is allowed to claim it can do
	CanSendMenu    bool
	CanSendPrices  bool
	CanCancelOrder bool
	CanModifyOrder bool
	CanCheckOrder  bool

	// What facts the AI is allowed to mention
	AllowedFacts map[string]bool

	// Output constraints
	AllowFollowUpQuestions bool
	AllowExplanations      bool
}
