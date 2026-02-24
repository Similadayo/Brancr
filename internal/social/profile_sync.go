package social

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ProfileSyncService handles syncing between Brancr and WhatsApp profiles
type ProfileSyncService struct {
	Logger *zap.Logger
}

// NewProfileSyncService creates a new profile sync service
func NewProfileSyncService(logger *zap.Logger) *ProfileSyncService {
	return &ProfileSyncService{
		Logger: logger,
	}
}

// SyncPreview represents what will be synced
type SyncPreview struct {
	Fields  []SyncField `json:"fields"`
	Summary string      `json:"summary"`
}

// SyncField represents a single field that will be synced
type SyncField struct {
	Name          string `json:"name"`
	BrancrValue   string `json:"brancr_value"`
	WhatsAppValue string `json:"whatsapp_value,omitempty"` // Current WhatsApp value (if available)
	WillChange    bool   `json:"will_change"`
}

// PrepareSyncPreview prepares a preview of what will be synced from Brancr to WhatsApp
func (s *ProfileSyncService) PrepareSyncPreview(tenant *storage.Tenant, businessProfile *storage.BusinessProfile, currentWhatsAppProfile *ProfileDetails) *SyncPreview {
	preview := &SyncPreview{
		Fields: []SyncField{},
	}

	// Map Brancr data to WhatsApp format
	mapped := s.mapBrancrToWhatsApp(tenant, businessProfile)

	// Build preview fields
	fields := []SyncField{}

	// Address Line 1
	if mapped.AddLine1 != "" {
		waValue := ""
		if currentWhatsAppProfile != nil && currentWhatsAppProfile.Address != "" {
			waValue = currentWhatsAppProfile.Address
		}
		fields = append(fields, SyncField{
			Name:          "Address Line 1",
			BrancrValue:   mapped.AddLine1,
			WhatsAppValue: waValue,
			WillChange:    waValue != mapped.AddLine1,
		})
	}

	// City
	if mapped.City != "" {
		waValue := ""
		if currentWhatsAppProfile != nil {
			waValue = currentWhatsAppProfile.Address // WhatsApp returns full address, we'd need to parse
		}
		fields = append(fields, SyncField{
			Name:          "City",
			BrancrValue:   mapped.City,
			WhatsAppValue: waValue,
			WillChange:    true, // Assume will change since we can't parse WhatsApp address easily
		})
	}

	// State
	if mapped.State != "" {
		fields = append(fields, SyncField{
			Name:        "State",
			BrancrValue: mapped.State,
			WillChange:  true,
		})
	}

	// Postal Code
	if mapped.PinCode != "" {
		fields = append(fields, SyncField{
			Name:        "Postal Code",
			BrancrValue: mapped.PinCode,
			WillChange:  true,
		})
	}

	// Country
	if mapped.Country != "" {
		fields = append(fields, SyncField{
			Name:        "Country",
			BrancrValue: mapped.Country,
			WillChange:  true,
		})
	}

	// Email
	if mapped.ProfileEmail != "" {
		waValue := ""
		if currentWhatsAppProfile != nil {
			waValue = currentWhatsAppProfile.ProfileEmail
		}
		fields = append(fields, SyncField{
			Name:          "Email",
			BrancrValue:   mapped.ProfileEmail,
			WhatsAppValue: waValue,
			WillChange:    waValue != mapped.ProfileEmail,
		})
	}

	// Website
	if mapped.Website1 != "" {
		waValue := ""
		if currentWhatsAppProfile != nil {
			waValue = currentWhatsAppProfile.Website1
		}
		fields = append(fields, SyncField{
			Name:          "Primary Website",
			BrancrValue:   mapped.Website1,
			WhatsAppValue: waValue,
			WillChange:    waValue != mapped.Website1,
		})
	}

	// Description
	if mapped.Desc != "" {
		waValue := ""
		if currentWhatsAppProfile != nil {
			waValue = currentWhatsAppProfile.Desc
		}
		fields = append(fields, SyncField{
			Name:          "Description",
			BrancrValue:   mapped.Desc,
			WhatsAppValue: waValue,
			WillChange:    waValue != mapped.Desc,
		})
	}

	// Industry/Vertical
	if mapped.Vertical != "" {
		waValue := ""
		if currentWhatsAppProfile != nil {
			waValue = currentWhatsAppProfile.Vertical
		}
		fields = append(fields, SyncField{
			Name:          "Business Category",
			BrancrValue:   mapped.Vertical,
			WhatsAppValue: waValue,
			WillChange:    waValue != mapped.Vertical,
		})
	}

	preview.Fields = fields

	// Build summary
	willChangeCount := 0
	for _, f := range fields {
		if f.WillChange {
			willChangeCount++
		}
	}

	if willChangeCount == 0 {
		preview.Summary = "No changes will be made. Your WhatsApp profile already matches your business profile."
	} else {
		preview.Summary = fmt.Sprintf("%d field(s) will be updated in your WhatsApp profile.", willChangeCount)
	}

	return preview
}

// MapBrancrToWhatsApp maps Brancr tenant and business profile data to WhatsApp profile format
func (s *ProfileSyncService) MapBrancrToWhatsApp(tenant *storage.Tenant, businessProfile *storage.BusinessProfile) UpdateProfileDetailsRequest {
	return s.mapBrancrToWhatsApp(tenant, businessProfile)
}

// mapBrancrToWhatsApp is the internal mapping function
func (s *ProfileSyncService) mapBrancrToWhatsApp(tenant *storage.Tenant, businessProfile *storage.BusinessProfile) UpdateProfileDetailsRequest {
	req := UpdateProfileDetailsRequest{}

	// Parse address from Location field (if available)
	// Location might be: "123 Main St, New York, NY 10001, USA"
	// We'll try to parse it, but it's not guaranteed to be structured
	if businessProfile != nil && businessProfile.Location != "" {
		addressParts := s.parseAddress(businessProfile.Location)
		req.AddLine1 = addressParts["street"]
		req.AddLine2 = addressParts["line2"]
		req.City = addressParts["city"]
		req.State = addressParts["state"]
		req.PinCode = addressParts["postal"]
		req.Country = addressParts["country"]
	}

	// Email from Tenant
	if tenant != nil && tenant.Email != "" {
		req.ProfileEmail = tenant.Email
	}

	// Website from BusinessProfile
	if businessProfile != nil && businessProfile.Website != "" {
		req.Website1 = businessProfile.Website
		// Ensure it has http:// or https://
		if !strings.HasPrefix(req.Website1, "http://") && !strings.HasPrefix(req.Website1, "https://") {
			req.Website1 = "https://" + req.Website1
		}
	}

	// Description from BusinessProfile (truncate to 512 chars for WhatsApp)
	if businessProfile != nil && businessProfile.Description != "" {
		desc := businessProfile.Description
		if len(desc) > 512 {
			desc = desc[:512]
		}
		req.Desc = desc
	}

	// Industry/Vertical mapping
	if businessProfile != nil && businessProfile.Industry != "" {
		req.Vertical = s.mapIndustryToVertical(businessProfile.Industry)
	} else if tenant != nil && tenant.IndustryID != uuid.Nil {
		// Try to get industry name from IndustryID (would need to query DB, but for now use fallback)
		req.Vertical = "OTHER"
	}

	return req
}

// parseAddress attempts to parse a location string into structured address components
// This is a simple parser - may not work for all formats
func (s *ProfileSyncService) parseAddress(location string) map[string]string {
	result := map[string]string{
		"street":  "",
		"line2":   "",
		"city":    "",
		"state":   "",
		"postal":  "",
		"country": "",
	}

	if location == "" {
		return result
	}

	// Try to parse common formats:
	// Format 1: "Street, City, State ZIP, Country"
	// Format 2: "Street, City, State, Country"
	// Format 3: "Street, City, Country"

	parts := strings.Split(location, ",")
	if len(parts) >= 1 {
		result["street"] = strings.TrimSpace(parts[0])
	}
	if len(parts) >= 2 {
		result["city"] = strings.TrimSpace(parts[1])
	}
	if len(parts) >= 3 {
		// Could be state or country
		thirdPart := strings.TrimSpace(parts[2])
		// Check if it contains a ZIP code pattern
		if matched := s.extractPostalCode(thirdPart); matched != "" {
			result["postal"] = matched
			result["state"] = strings.TrimSpace(strings.ReplaceAll(thirdPart, matched, ""))
		} else {
			result["state"] = thirdPart
		}
	}
	if len(parts) >= 4 {
		result["country"] = strings.TrimSpace(parts[3])
	} else if len(parts) == 3 && result["state"] == "" {
		// If only 3 parts and no state extracted, third part might be country
		result["country"] = strings.TrimSpace(parts[2])
	}

	return result
}

// extractPostalCode tries to extract postal code from a string
func (s *ProfileSyncService) extractPostalCode(text string) string {
	// Simple regex-like extraction for common formats: 5 digits, or 5-4 digits
	text = strings.TrimSpace(text)
	// Look for 5-digit pattern
	if len(text) >= 5 {
		for i := 0; i <= len(text)-5; i++ {
			substr := text[i : i+5]
			if s.isNumeric(substr) {
				return substr
			}
		}
	}
	return ""
}

// isNumeric checks if a string is numeric
func (s *ProfileSyncService) isNumeric(str string) bool {
	for _, char := range str {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// mapIndustryToVertical maps Brancr industry names to WhatsApp vertical enum values
// WhatsApp verticals: OTHER, AUTO, BEAUTY, APPAREL, EDU, ENTERTAIN, EVENT_PLAN, FINANCE, GROCERY, GOVT, HOTEL, HEALTH, NONPROFIT, PROF_SERVICES, RETAIL, TRAVEL, RESTAURANT
func (s *ProfileSyncService) mapIndustryToVertical(industry string) string {
	industryLower := strings.ToLower(industry)

	// Map common industry names to WhatsApp verticals
	mapping := map[string]string{
		// Food & Beverage
		"food & beverage":   "RESTAURANT",
		"food and beverage": "RESTAURANT",
		"restaurant":        "RESTAURANT",
		"cafe":              "RESTAURANT",
		"bakery":            "RESTAURANT",
		"food":              "RESTAURANT",
		"beverage":          "RESTAURANT",

		// Retail
		"retail":     "RETAIL",
		"e-commerce": "RETAIL",
		"shop":       "RETAIL",
		"store":      "RETAIL",

		// Professional Services
		"professional services": "PROF_SERVICES",
		"consulting":            "PROF_SERVICES",
		"legal":                 "PROF_SERVICES",
		"accounting":            "PROF_SERVICES",
		"real estate":           "PROF_SERVICES",
		"agency":                "PROF_SERVICES",

		// Health
		"health":     "HEALTH",
		"healthcare": "HEALTH",
		"medical":    "HEALTH",
		"wellness":   "HEALTH",
		"fitness":    "HEALTH",

		// Beauty
		"beauty":    "BEAUTY",
		"cosmetics": "BEAUTY",
		"salon":     "BEAUTY",
		"spa":       "BEAUTY",

		// Education
		"education": "EDU",
		"school":    "EDU",
		"training":  "EDU",
		"tutoring":  "EDU",

		// Finance
		"finance":   "FINANCE",
		"banking":   "FINANCE",
		"insurance": "FINANCE",

		// Travel
		"travel":      "TRAVEL",
		"tourism":     "TRAVEL",
		"hotel":       "HOTEL",
		"hospitality": "HOTEL",

		// Entertainment
		"entertainment": "ENTERTAIN",
		"media":         "ENTERTAIN",
		"music":         "ENTERTAIN",

		// Event Planning
		"event planning": "EVENT_PLAN",
		"events":         "EVENT_PLAN",
		"wedding":        "EVENT_PLAN",

		// Auto
		"automotive": "AUTO",
		"auto":       "AUTO",
		"car":        "AUTO",

		// Apparel
		"fashion":  "APPAREL",
		"apparel":  "APPAREL",
		"clothing": "APPAREL",

		// Grocery
		"grocery":     "GROCERY",
		"supermarket": "GROCERY",

		// Nonprofit
		"nonprofit": "NONPROFIT",
		"charity":   "NONPROFIT",
		"ngo":       "NONPROFIT",
	}

	// Check exact match first
	if vertical, ok := mapping[industryLower]; ok {
		return vertical
	}

	// Check partial matches
	for key, vertical := range mapping {
		if strings.Contains(industryLower, key) {
			return vertical
		}
	}

	// Default to OTHER if no match
	s.Logger.Warn("Industry not mapped to WhatsApp vertical, using OTHER",
		zap.String("industry", industry))
	return "OTHER"
}
