package conversation

func PickQualificationQuestion() string {
	// Deterministic selection (first one for now)
	return QualificationQuestions[0]
}
