package persona

import "strings"

func LuxuryFormatter(input string) string {
	// polished, calm, shorter sentences
	return stripEmojis(trimCasualPhrases(input))
}

func trimCasualPhrases(s string) string {
	// Remove "Hey", "Hi there"
	s = strings.ReplaceAll(s, "Hey there, ", "")
	s = strings.ReplaceAll(s, "Hey, ", "")
	s = strings.ReplaceAll(s, "Hi! ", "")
	return s
}
