package admin

import "embed"

// adminTemplates holds all admin HTML templates embedded into the binary.
//go:embed templates/*.html
var adminTemplates embed.FS


