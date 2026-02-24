package conversation

type ConfidenceLevel string

const (
	ConfidenceHigh   ConfidenceLevel = "HIGH"
	ConfidenceMedium ConfidenceLevel = "MEDIUM"
	ConfidenceLow    ConfidenceLevel = "LOW"
)

func ConfidenceFromRounds(rounds int) ConfidenceLevel {
	switch {
	case rounds <= 1:
		return ConfidenceHigh
	case rounds <= 3:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}
