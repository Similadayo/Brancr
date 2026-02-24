package conversation

// Platform defines the supported communication channels.
// Logic and Persona adapt based on these explicit types.
type Platform string

const (
	PlatformWhatsApp  Platform = "WHATSAPP"
	PlatformInstagram Platform = "INSTAGRAM"
	PlatformWeb       Platform = "WEB"
	PlatformSMS       Platform = "SMS"
)
