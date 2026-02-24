package rules

type RuleResult struct {
	AllowedActions []ActionType
	Reason         string
}
