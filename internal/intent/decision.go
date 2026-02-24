package intent

import "strings"

func IsDecisionDelegation(message string) bool {
	triggers := []string{
		"pick one",
		"you choose",
		"recommend",
		"which should i",
		"what do you suggest",
		"not sure which",
		"surprise me",
		"best option",
	}

	l := strings.ToLower(message)
	for _, t := range triggers {
		if strings.Contains(l, t) {
			return true
		}
	}
	return false
}
