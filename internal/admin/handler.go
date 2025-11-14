package admin

import (
	"encoding/json"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// WhatsAppNumbersHandler lists all WhatsApp phone numbers in the pool
func WhatsAppNumbersHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var numbers []storage.WhatsAppPhoneNumber
		if err := ctx.DB.Order("created_at DESC").Find(&numbers).Error; err != nil {
			ctx.Logger.Error("failed to fetch phone numbers", zap.Error(err))
			http.Error(w, "failed to fetch phone numbers", http.StatusInternalServerError)
			return
		}

		// Get tenant info for assigned numbers
		type numberWithTenant struct {
			storage.WhatsAppPhoneNumber
			TenantName string `json:"tenant_name"`
		}
		
		response := make([]numberWithTenant, 0, len(numbers))
		for _, num := range numbers {
			item := numberWithTenant{WhatsAppPhoneNumber: num}
			if num.TenantID != nil && *num.TenantID != 0 {
				var tenant storage.Tenant
				if err := ctx.DB.First(&tenant, *num.TenantID).Error; err == nil {
					item.TenantName = tenant.Name
				}
			}
			response = append(response, item)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"numbers": response,
			"total":   len(response),
		})
	}
}

// WhatsAppSyncHandler syncs phone numbers from Meta's WABA
func WhatsAppSyncHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ctx.Config.BrancrWABAID == "" || ctx.Config.BrancrWABAToken == "" {
			http.Error(w, "WhatsApp WABA not configured", http.StatusBadRequest)
			return
		}

		if err := social.SyncPhoneNumbersFromMeta(ctx.DB, ctx.Logger, ctx.Config); err != nil {
			ctx.Logger.Error("failed to sync phone numbers", zap.Error(err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "failed to sync phone numbers",
				"message": err.Error(),
			})
			return
		}

		// Get count of available numbers after sync
		var availableCount int64
		ctx.DB.Model(&storage.WhatsAppPhoneNumber{}).Where("status = ? AND (tenant_id IS NULL OR tenant_id = 0)", "available").Count(&availableCount)

		ctx.Logger.Info("phone numbers synced from Meta", zap.String("waba_id", ctx.Config.BrancrWABAID))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":        true,
			"message":        "Phone numbers synced successfully",
			"available_count": availableCount,
		})
	}
}

// WhatsAppAddNumberHandler adds a new phone number to Meta's WABA programmatically
func WhatsAppAddNumberHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			PhoneNumber string `json:"phone_number"` // e.g., "+1234567890"
			DisplayName string `json:"display_name"` // Business name for the number
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.PhoneNumber == "" {
			http.Error(w, "phone_number is required", http.StatusBadRequest)
			return
		}

		if ctx.Config.BrancrWABAID == "" || ctx.Config.BrancrWABAToken == "" {
			http.Error(w, "WhatsApp WABA not configured", http.StatusBadRequest)
			return
		}

		// IMPORTANT: Phone numbers must be added MANUALLY in Meta Business Manager first
		// When admin adds a phone number in Meta BM:
		// 1. Meta sends verification code to the phone number
		// 2. Admin (or tenant) provides code in Meta UI
		// 3. Number is verified and added to WABA
		// 4. Admin syncs to Brancr: POST /admin/whatsapp/sync
		// 5. Then tenant can request another verification code via API for ownership
		
		// Format phone number (ensure + prefix)
		phoneNumber := strings.TrimSpace(req.PhoneNumber)
		if !strings.HasPrefix(phoneNumber, "+") {
			phoneNumber = "+" + phoneNumber
		}
		
		// Try to find the phone number in database (must be synced first)
		var phoneNum storage.WhatsAppPhoneNumber
		if err := ctx.DB.Where("phone_number = ?", phoneNumber).First(&phoneNum).Error; err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "phone number not found",
				"message": "Phone number must be added manually in Meta Business Manager first. " +
					"When adding in Meta BM, Meta will send a verification code to the phone number. " +
					"After adding and verifying in Meta, sync phone numbers using POST /admin/whatsapp/sync, " +
					"then use POST /admin/whatsapp/request-code to request a verification code for tenant access.",
			})
			return
		}

		// Request verification code for the existing phone number
		if err := social.RequestPhoneNumberVerificationCode(ctx.Logger, ctx.Config, phoneNum.PhoneNumberID, "SMS", "en_US"); err != nil {
			ctx.Logger.Error("failed to request verification code", zap.Error(err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "failed to request verification code",
				"message": err.Error(),
			})
			return
		}

		ctx.Logger.Info("verification code requested",
			zap.String("phone_number", req.PhoneNumber),
			zap.String("phone_number_id", phoneNum.PhoneNumberID))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         true,
			"phone_number_id": phoneNum.PhoneNumberID,
			"message":         "Verification code requested. Meta will send the code to the phone number. Use POST /admin/whatsapp/verify with the code to complete verification.",
		})
	}
}

// WhatsAppVerifyNumberHandler verifies a phone number with the code sent by Meta
func WhatsAppVerifyNumberHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			PhoneNumberID    string `json:"phone_number_id"`
			VerificationCode string `json:"verification_code"` // Code sent via SMS/voice
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.PhoneNumberID == "" || req.VerificationCode == "" {
			http.Error(w, "phone_number_id and verification_code are required", http.StatusBadRequest)
			return
		}

		if ctx.Config.BrancrWABAID == "" || ctx.Config.BrancrWABAToken == "" {
			http.Error(w, "WhatsApp WABA not configured", http.StatusBadRequest)
			return
		}

		// Verify phone number with code
		if err := social.VerifyPhoneNumberWithCode(ctx.Logger, ctx.Config, req.PhoneNumberID, req.VerificationCode); err != nil {
			ctx.Logger.Error("failed to verify phone number", zap.Error(err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "failed to verify phone number",
				"message": err.Error(),
			})
			return
		}

		// After verification, sync to update database
		if err := social.SyncPhoneNumbersFromMeta(ctx.DB, ctx.Logger, ctx.Config); err != nil {
			ctx.Logger.Warn("phone number verified but sync failed", zap.Error(err))
		}

		ctx.Logger.Info("phone number verified successfully", zap.String("phone_number_id", req.PhoneNumberID))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Phone number verified successfully. It will appear in the pool after sync.",
		})
	}
}

// renderTemplate loads both layout and page template together
func renderTemplate(w http.ResponseWriter, page string, data any, logger *zap.Logger) {
	layout := filepath.Join("internal", "admin", "templates", "layout.html")
	pagePath := filepath.Join("internal", "admin", "templates", page)

	tpl, err := template.ParseFiles(layout, pagePath)
	if err != nil {
		logger.Error("template parse failed", zap.String("page", page), zap.Error(err))
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	err = tpl.ExecuteTemplate(w, "layout.html", data)
	if err != nil {
		logger.Error("template execute failed", zap.String("page", page), zap.Error(err))
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// simple login template loader
func loadTemplate(name string) (*template.Template, error) {
	return template.ParseFiles("internal/admin/templates/" + name)
}

// ✅ GET /admin/login
func GetLoginHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tpl, err := loadTemplate("login.html")
		if err != nil {
			ctx.Logger.Error("load login template", zap.Error(err))
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
		tpl.Execute(w, map[string]interface{}{
			"AppName": "Brancr Admin",
		})
	}
}

// ✅ POST /admin/login
func PostLoginHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")
		cfgEmail := ctx.AdminEmail
		cfgPassword := ctx.AdminPassword

		if cfgEmail == "" || cfgPassword == "" {
			ctx.Logger.Warn("admin credentials not set in config")
			http.Error(w, "admin credentials not configured", http.StatusInternalServerError)
			return
		}

		if email != cfgEmail {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		useHash := len(cfgPassword) > 3 && (cfgPassword[:3] == "$2a" || cfgPassword[:3] == "$2b")
		ok := false
		if useHash {
			if err := bcrypt.CompareHashAndPassword([]byte(cfgPassword), []byte(password)); err == nil {
				ok = true
			}
		} else if password == cfgPassword {
			ok = true
		}

		if !ok {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		tokenStr, err := createToken(ctx, cfgEmail, 24*time.Hour)
		if err != nil {
			ctx.Logger.Error("create token", zap.Error(err))
			http.Error(w, "could not create token", http.StatusInternalServerError)
			return
		}
		setAdminCookie(w, tokenStr)
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
	}
}

// ✅ GET /admin/
func DashboardHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := GetAnalyticsStats(ctx)

		data := map[string]interface{}{
			"Page":              "dashboard",
			"TotalTenants":      stats.TotalTenants,
			"ActiveBots":        stats.ActiveTenants,
			"PostsToday":        stats.EventsToday,
			"PaymentsToday":     stats.PaymentsToday,
			"DeliveriesToday":   stats.DeliveriesToday,
			"AppointmentsToday": stats.AppointmentsToday,
			"OpenRouterUsage":   stats.OpenRouterUsage,
			"CloudinaryUsage":   stats.CloudinaryUsage,
		}

		renderTemplate(w, "dashboard.html", data, ctx.Logger)
	}
}

// ✅ GET /admin/tenants
func TenantsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type Tenant struct {
			ID        uint
			Name      string
			Email     string
			PlanType  string
			Status    string
			CreatedAt time.Time
		}

		var tenants []Tenant
		if ctx.DB != nil {
			if err := ctx.DB.Model(&Tenant{}).Order("created_at DESC").Find(&tenants).Error; err != nil {
				ctx.Logger.Warn("failed to fetch tenants", zap.Error(err))
			}
		}

		data := map[string]any{
			"Page":       "tenants",
			"Tenants":    tenants,
			"TotalCount": len(tenants),
		}

		renderTemplate(w, "tenants.html", data, ctx.Logger)
	}
}

// ✅ GET /admin/tenants/{id}
func TenantDetailHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// For now, just redirect back to tenants list
		// Can be extended later to show tenant details
		http.Redirect(w, r, "/admin/tenants", http.StatusSeeOther)
	}
}

func SocialAccountsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var accounts []storage.SocialAccount
		ctx.DB.Find(&accounts)

		data := map[string]any{
			"Accounts": accounts,
		}

		renderTemplate(w, "social.html", data, ctx.Logger)
	}
}

// ✅ POST /admin/logout
func LogoutHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearAdminCookie(w)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	}
}
