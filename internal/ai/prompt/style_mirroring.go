package prompt

// StyleMirroringBlock generates instructions to match the user's vibe.
func StyleMirroringBlock(style string) string {
	if style == "" || style == "NEUTRAL" {
		return ""
	}

	b := "STYLE ADAPTATION:\n"
	if style == "CASUAL" {
		b += "- The user is speaking casually (slang, lowercase, short).\n"
		b += "- MATCH THIS VIBE. Relax your grammar. You can use lowercase if it feels right.\n"
		b += "- Use simple words. Be chill. Don't be overly formal.\n"
		b += "- Example: Instead of 'I can certainly help with that', say 'Got u, happy to help.'\n"
	} else if style == "FORMAL" {
		b += "- The user is speaking formally.\n"
		b += "- Maintain a professional, polite, and grammatically perfect tone.\n"
		b += "- Do not use slang.\n"
	}

	return b
}
