package persona

func FormatterForTone(t TonePreset) Formatter {
	switch t {
	case ToneFriendly:
		return FormatterFunc(FriendlyFormatter)
	case ToneLuxury:
		return FormatterFunc(LuxuryFormatter)
	case ToneCasual:
		return FormatterFunc(CasualFormatter)
	default:
		return FormatterFunc(ProfessionalFormatter)
	}
}
