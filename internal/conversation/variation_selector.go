package conversation

func SelectVariant(variants []string, seed int) string {
	if len(variants) == 0 {
		return ""
	}
	// Ensure positive modulus
	if seed < 0 {
		seed = -seed
	}
	return variants[seed%len(variants)]
}
