package persona

import "strings"

func CasualFormatter(input string) string {
	// relaxed, short, friendly
	return soften(input)
}

func soften(s string) string {
	// Replace formal with casual
	s = strings.ReplaceAll(s, "Hello", "Hey")
	s = strings.ReplaceAll(s, "Greetings", "Hi")
	return s
}
