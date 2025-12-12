package admin

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// WhatsAppNumbersHandler lists all WhatsApp phone numbers (JSON API endpoint)
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

// WhatsAppNumbersPageHandler renders the WhatsApp numbers admin page (HTML)
func WhatsAppNumbersPageHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var numbers []storage.WhatsAppPhoneNumber
		if err := ctx.DB.Order("created_at DESC").Find(&numbers).Error; err != nil {
			ctx.Logger.Error("failed to fetch phone numbers", zap.Error(err))
			http.Error(w, "failed to fetch phone numbers", http.StatusInternalServerError)
			return
		}

		// Get tenant info for assigned numbers and prepare display data
		type displayNumber struct {
			storage.WhatsAppPhoneNumber
			TenantName string
			TenantID   uint // For display purposes in template
		}

		displayNumbers := make([]displayNumber, 0, len(numbers))
		var availableCount, assignedCount int

		for _, num := range numbers {
			item := displayNumber{WhatsAppPhoneNumber: num}
			if num.TenantID != nil && *num.TenantID != 0 {
				var tenant storage.Tenant
				if err := ctx.DB.First(&tenant, *num.TenantID).Error; err == nil {
					item.TenantName = tenant.Name
					item.TenantID = *num.TenantID // Extract uint value from pointer
				}
				assignedCount++
			} else {
				availableCount++
			}
			displayNumbers = append(displayNumbers, item)
		}

		// Get phone number requests
		var requests []storage.WhatsAppPhoneNumberRequest
		ctx.DB.Where("status IN (?)", []string{"pending", "added_to_meta", "verification_code_sent"}).Order("created_at DESC").Find(&requests)

		type requestWithTenant struct {
			storage.WhatsAppPhoneNumberRequest
			TenantName  string
			TenantEmail string
		}

		displayRequests := make([]requestWithTenant, 0, len(requests))
		for _, req := range requests {
			item := requestWithTenant{WhatsAppPhoneNumberRequest: req}
			var tenant storage.Tenant
			if err := ctx.DB.First(&tenant, req.TenantID).Error; err == nil {
				item.TenantName = tenant.Name
				item.TenantEmail = tenant.Email
			}
			displayRequests = append(displayRequests, item)
		}

		data := map[string]any{
			"Numbers":       displayNumbers,
			"Total":         len(numbers),
			"Available":     availableCount,
			"Assigned":      assignedCount,
			"Requests":      displayRequests,
			"RequestsCount": len(displayRequests),
			"Page":          "whatsapp",
		}

		renderTemplate(w, r, "whatsapp.html", data, ctx.Logger)
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
			"success":         true,
			"message":         "Phone numbers synced successfully",
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

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "invalid request body",
				"message": err.Error(),
			})
			return
		}

		if req.PhoneNumber == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "missing required field",
				"message": "phone_number is required",
			})
			return
		}

		if ctx.Config.BrancrWABAID == "" || ctx.Config.BrancrWABAToken == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "configuration error",
				"message": "WhatsApp WABA not configured",
			})
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
		// Try both with and without + prefix
		var phoneNum storage.WhatsAppPhoneNumber
		phoneNumberQuery := phoneNumber
		if !strings.HasPrefix(phoneNumberQuery, "+") {
			phoneNumberQuery = "+" + phoneNumberQuery
		}

		err := ctx.DB.Where("phone_number = ? OR phone_number = ?", phoneNumber, phoneNumberQuery).First(&phoneNum).Error
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "phone number not found",
				"message": "Phone number must be added manually in Meta Business Manager first. " +
					"When adding in Meta BM, Meta will send a verification code to the phone number. " +
					"After adding and verifying in Meta, sync phone numbers using POST /admin/whatsapp/sync, " +
					"then use POST /admin/whatsapp/request-code to request a verification code for tenant access.",
				"received_phone_number": req.PhoneNumber,
				"searched_for":          phoneNumberQuery,
			})
			return
		}

		// CRITICAL: Register the phone number FIRST if it hasn't been registered yet
		// Meta requires registration before requesting a verification code
		// If status is "pending" or "pending_verification", register first
		if phoneNum.Status == "pending" || phoneNum.Status == "pending_verification" {
			ctx.Logger.Info("registering phone number before requesting code",
				zap.String("phone_number_id", phoneNum.PhoneNumberID),
				zap.String("status", phoneNum.Status))

			if err := social.RegisterPhoneNumber(ctx.Logger, ctx.Config, phoneNum.PhoneNumberID); err != nil {
				ctx.Logger.Error("failed to register phone number before requesting code", zap.Error(err))
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":   "failed to register phone number",
					"message": "Phone number must be registered before requesting verification code. Error: " + err.Error(),
				})
				return
			}

			ctx.Logger.Info("phone number registered successfully, proceeding to request code",
				zap.String("phone_number_id", phoneNum.PhoneNumberID))
		}

		// Request verification code for the existing phone number
		if err := social.RequestPhoneNumberVerificationCode(ctx.Logger, ctx.Config, phoneNum.PhoneNumberID, "SMS", "en_US"); err != nil {
			ctx.Logger.Error("failed to request verification code", zap.Error(err))
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "failed to request verification code",
				"message": err.Error(),
			})
			return
		}

		ctx.Logger.Info("verification code requested",
			zap.String("phone_number", req.PhoneNumber),
			zap.String("phone_number_id", phoneNum.PhoneNumberID),
			zap.String("phone_number_formatted", phoneNumber))

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         true,
			"phone_number_id": phoneNum.PhoneNumberID,
			"phone_number":    phoneNum.PhoneNumber,
			"message":         "Verification code requested. Meta will send the code to the phone number. Use POST /admin/whatsapp/verify with the code to complete verification.",
		})
	}
}

// WhatsAppRegisterNumberHandler registers a phone number with Meta (required before verification)
func WhatsAppRegisterNumberHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			PhoneNumberID string `json:"phone_number_id"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.PhoneNumberID == "" {
			http.Error(w, "phone_number_id is required", http.StatusBadRequest)
			return
		}

		if ctx.Config.BrancrWABAID == "" || ctx.Config.BrancrWABAToken == "" {
			http.Error(w, "WhatsApp WABA not configured", http.StatusBadRequest)
			return
		}

		// Register the phone number
		if err := social.RegisterPhoneNumber(ctx.Logger, ctx.Config, req.PhoneNumberID); err != nil {
			ctx.Logger.Error("failed to register phone number", zap.Error(err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "failed to register phone number",
				"message": err.Error(),
			})
			return
		}

		ctx.Logger.Info("phone number registered successfully", zap.String("phone_number_id", req.PhoneNumberID))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         true,
			"message":         "Phone number registered successfully. You can now verify it with the verification code.",
			"phone_number_id": req.PhoneNumberID,
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

		// STEP 1: Register the phone number (REQUIRED before verification)
		// This activates the number and makes it ready for verification
		if err := social.RegisterPhoneNumber(ctx.Logger, ctx.Config, req.PhoneNumberID); err != nil {
			ctx.Logger.Error("failed to register phone number", zap.Error(err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "failed to register phone number",
				"message": err.Error(),
			})
			return
		}

		ctx.Logger.Info("phone number registered successfully", zap.String("phone_number_id", req.PhoneNumberID))

		// STEP 2: Verify phone number with code (must be called AFTER register)
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

// WhatsAppRequestsHandler lists all phone number requests from tenants
func WhatsAppRequestsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var requests []storage.WhatsAppPhoneNumberRequest
		if err := ctx.DB.Order("created_at DESC").Find(&requests).Error; err != nil {
			ctx.Logger.Error("failed to fetch phone number requests", zap.Error(err))
			http.Error(w, "failed to fetch requests", http.StatusInternalServerError)
			return
		}

		// Get tenant info for each request
		type requestWithTenant struct {
			storage.WhatsAppPhoneNumberRequest
			TenantName  string `json:"tenant_name"`
			TenantEmail string `json:"tenant_email"`
		}

		displayRequests := make([]requestWithTenant, 0, len(requests))
		for _, req := range requests {
			item := requestWithTenant{WhatsAppPhoneNumberRequest: req}
			var tenant storage.Tenant
			if err := ctx.DB.First(&tenant, req.TenantID).Error; err == nil {
				item.TenantName = tenant.Name
				item.TenantEmail = tenant.Email
			}
			displayRequests = append(displayRequests, item)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"requests": displayRequests,
			"total":    len(displayRequests),
		})
	}
}

// WhatsAppMarkAddedHandler marks a request as added to Meta (after admin adds it in Meta BM)
func WhatsAppMarkAddedHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			RequestID     uint   `json:"request_id"`
			PhoneNumberID string `json:"phone_number_id"` // Meta's phone_number_id after adding to Meta
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		var phoneRequest storage.WhatsAppPhoneNumberRequest
		if err := ctx.DB.First(&phoneRequest, req.RequestID).Error; err != nil {
			http.Error(w, "request not found", http.StatusNotFound)
			return
		}

		phoneRequest.Status = "added_to_meta"
		phoneRequest.PhoneNumberID = req.PhoneNumberID
		now := time.Now()
		phoneRequest.ProcessedAt = &now

		if err := ctx.DB.Save(&phoneRequest).Error; err != nil {
			ctx.Logger.Error("failed to update request", zap.Error(err))
			http.Error(w, "failed to update request", http.StatusInternalServerError)
			return
		}

		ctx.Logger.Info("phone number request marked as added to Meta",
			zap.Uint("request_id", req.RequestID),
			zap.String("phone_number", phoneRequest.PhoneNumber))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Request marked as added to Meta. Tenant should provide the verification code they receive.",
		})
	}
}

// WhatsAppVerifyRequestHandler verifies a phone number request with code provided by tenant
func WhatsAppVerifyRequestHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			RequestID uint `json:"request_id"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		var phoneRequest storage.WhatsAppPhoneNumberRequest
		if err := ctx.DB.First(&phoneRequest, req.RequestID).Error; err != nil {
			http.Error(w, "request not found", http.StatusNotFound)
			return
		}

		if phoneRequest.VerificationCode == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "no_verification_code",
				"message": "Tenant has not provided the verification code yet.",
			})
			return
		}

		if phoneRequest.PhoneNumberID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "phone_number_id_missing",
				"message": "Phone number ID is missing. Make sure you marked the request as added to Meta first.",
			})
			return
		}

		// STEP 1: Register the phone number (REQUIRED before verification)
		if err := social.RegisterPhoneNumber(ctx.Logger, ctx.Config, phoneRequest.PhoneNumberID); err != nil {
			ctx.Logger.Error("failed to register phone number", zap.Error(err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "failed to register phone number",
				"message": err.Error(),
			})
			return
		}

		ctx.Logger.Info("phone number registered successfully", zap.String("phone_number_id", phoneRequest.PhoneNumberID))

		// STEP 2: Verify phone number with code (must be called AFTER register)
		if err := social.VerifyPhoneNumberWithCode(ctx.Logger, ctx.Config, phoneRequest.PhoneNumberID, phoneRequest.VerificationCode); err != nil {
			ctx.Logger.Error("failed to verify phone number", zap.Error(err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "verification_failed",
				"message": err.Error(),
			})
			return
		}

		// Sync to get the phone number details
		if err := social.SyncPhoneNumbersFromMeta(ctx.DB, ctx.Logger, ctx.Config); err != nil {
			ctx.Logger.Warn("phone number verified but sync failed", zap.Error(err))
		}

		// Find the verified phone number and assign to tenant
		var phoneNum storage.WhatsAppPhoneNumber
		if err := ctx.DB.Where("phone_number_id = ?", phoneRequest.PhoneNumberID).First(&phoneNum).Error; err != nil {
			ctx.Logger.Error("verified phone number not found in database after sync", zap.Error(err))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Phone number verified. Please sync and assign it manually.",
			})
			return
		}

		// Assign to tenant
		now := time.Now()
		phoneNum.TenantID = &phoneRequest.TenantID
		phoneNum.Status = "assigned"
		phoneNum.AssignedAt = &now
		phoneNum.DisplayName = phoneRequest.DisplayName

		if err := ctx.DB.Save(&phoneNum).Error; err != nil {
			ctx.Logger.Error("failed to assign verified phone number", zap.Error(err))
		}

		// Update request status
		phoneRequest.Status = "verified"
		ctx.DB.Save(&phoneRequest)

		// Create/update social account
		var account storage.SocialAccount
		if err := ctx.DB.Where("tenant_id = ? AND platform = ?", phoneRequest.TenantID, "whatsapp").First(&account).Error; err != nil {
			account = storage.SocialAccount{
				TenantID:   phoneRequest.TenantID,
				Platform:   "whatsapp",
				PageID:     phoneNum.PhoneNumberID,
				ExternalID: phoneNum.PhoneNumberID,
				Username:   phoneNum.PhoneNumber,
				Connected:  true,
				Mode:       "provider_owned",
			}
			ctx.DB.Create(&account)
		} else {
			account.PageID = phoneNum.PhoneNumberID
			account.ExternalID = phoneNum.PhoneNumberID
			account.Username = phoneNum.PhoneNumber
			account.Connected = true
			account.Mode = "provider_owned"
			ctx.DB.Save(&account)
		}

		ctx.Logger.Info("phone number request verified and assigned",
			zap.Uint("request_id", req.RequestID),
			zap.String("phone_number", phoneRequest.PhoneNumber),
			zap.Uint("tenant_id", phoneRequest.TenantID))

		// Subscribe phone number to webhooks for receiving messages
		// Use system token for provider-owned model
		systemToken := ctx.Config.MetaSystemUserToken
		if systemToken != "" {
			go func() {
				// Subscribe phone number to receive webhook events
				// This is async to avoid blocking the response
				social.SubscribePhoneNumberToWebhooks(ctx.Logger, phoneNum.PhoneNumberID, systemToken)
			}()
		} else {
			ctx.Logger.Warn("webhook subscription skipped: system token not configured",
				zap.String("phone_number_id", phoneNum.PhoneNumberID))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      true,
			"message":      "Phone number verified and assigned successfully!",
			"phone_number": phoneNum.PhoneNumber,
		})
	}
}

// WhatsAppRejectRequestHandler rejects a phone number request
func WhatsAppRejectRequestHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			RequestID      uint   `json:"request_id"`
			RejectedReason string `json:"rejected_reason"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		var phoneRequest storage.WhatsAppPhoneNumberRequest
		if err := ctx.DB.First(&phoneRequest, req.RequestID).Error; err != nil {
			http.Error(w, "request not found", http.StatusNotFound)
			return
		}

		phoneRequest.Status = "rejected"
		phoneRequest.RejectedReason = req.RejectedReason
		now := time.Now()
		phoneRequest.ProcessedAt = &now

		if err := ctx.DB.Save(&phoneRequest).Error; err != nil {
			ctx.Logger.Error("failed to reject request", zap.Error(err))
			http.Error(w, "failed to reject request", http.StatusInternalServerError)
			return
		}

		ctx.Logger.Info("phone number request rejected",
			zap.Uint("request_id", req.RequestID),
			zap.String("phone_number", phoneRequest.PhoneNumber),
			zap.String("reason", req.RejectedReason))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Request rejected.",
		})
	}
}

// renderTemplate loads both layout and page template together (embedded templates)
func renderTemplate(w http.ResponseWriter, r *http.Request, page string, data any, logger *zap.Logger) {
	// Get request context logger if available
	ctxLogger := GetAdminLogger(r)
	if ctxLogger != nil {
		logger = ctxLogger
	}

	// Ensure correct content type for HTML responses
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	requestID := ""
	if r != nil {
		requestID = middleware.GetRequestID(r.Context())
	}

	tpl, err := template.ParseFS(adminTemplates, "templates/layout.html", "templates/"+page)
	if err != nil {
		logger.Error("template parse failed",
			zap.String("page", page),
			zap.String("request_id", requestID),
			zap.Error(err))
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	if err := tpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		logger.Error("template execute failed",
			zap.String("page", page),
			zap.String("request_id", requestID),
			zap.Error(err))
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// simple login template loader (embedded)
func loadTemplate(name string) (*template.Template, error) {
	return template.ParseFS(adminTemplates, "templates/"+name)
}

// ✅ GET /admin/login
func GetLoginHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
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
		requestID := middleware.GetRequestID(r.Context())
		logger := ctx.Logger.With(
			zap.String("request_id", requestID),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
		)

		if err := r.ParseForm(); err != nil {
			logger.Warn("login failed: parse form error", zap.Error(err))
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")

		// Don't log password, but log login attempt
		logger.Info("admin login attempt", zap.String("email", email))
		cfgEmail := ctx.AdminEmail
		cfgPassword := ctx.AdminPassword

		// Prefer DB-backed admin users when available.
		var dbAdmin storage.AdminUser
		if ctx.DB != nil {
			if err := ctx.DB.Where("email = ?", email).First(&dbAdmin).Error; err == nil {
				if !dbAdmin.Active {
					http.Error(w, "account disabled", http.StatusUnauthorized)
					return
				}
				if err := bcrypt.CompareHashAndPassword([]byte(dbAdmin.PasswordHash), []byte(password)); err != nil {
					logger.Warn("admin login failed: invalid password", zap.String("email", email))
					http.Error(w, "invalid credentials", http.StatusUnauthorized)
					return
				}

				now := time.Now()
				_ = ctx.DB.Model(&storage.AdminUser{}).Where("id = ?", dbAdmin.ID).Update("last_login", &now).Error

				tokenStr, err := createToken(ctx, dbAdmin.Email, dbAdmin.Role, 24*time.Hour)
				if err != nil {
					logger.Error("admin login failed: token creation error", zap.Error(err))
					http.Error(w, "could not create token", http.StatusInternalServerError)
					return
				}
				setAdminCookie(w, tokenStr)
				logger.Info("admin login successful", zap.String("email", email), zap.String("role", dbAdmin.Role))
				http.Redirect(w, r, "/admin/", http.StatusSeeOther)
				return
			}
		}

		if cfgEmail == "" || cfgPassword == "" {
			ctx.Logger.Warn("admin credentials not set in config")
			http.Error(w, "admin credentials not configured", http.StatusInternalServerError)
			return
		}

		if email != cfgEmail {
			logger.Warn("admin login failed: invalid email",
				zap.String("email", email))
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
			logger.Warn("admin login failed: invalid password",
				zap.String("email", email))
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		tokenStr, err := createToken(ctx, cfgEmail, RoleSuperAdmin, 24*time.Hour)
		if err != nil {
			logger.Error("admin login failed: token creation error", zap.Error(err))
			http.Error(w, "could not create token", http.StatusInternalServerError)
			return
		}
		setAdminCookie(w, tokenStr)
		logger.Info("admin login successful", zap.String("email", email))
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

		renderTemplate(w, r, "dashboard.html", data, ctx.Logger)
	}
}

// ✅ GET /admin/tenants
func TenantsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type tenantView struct {
			ID        uint
			Name      string
			Email     string
			PlanType  string
			Status    string
			CreatedAt time.Time
		}

		var dbTenants []storage.Tenant
		if ctx.DB != nil {
			if err := ctx.DB.Order("created_at DESC").Find(&dbTenants).Error; err != nil {
				ctx.Logger.Warn("failed to fetch tenants", zap.Error(err))
			}
		}

		tenants := make([]tenantView, 0, len(dbTenants))
		for _, t := range dbTenants {
			tenants = append(tenants, tenantView{
				ID:        t.ID,
				Name:      t.Name,
				Email:     t.Email,
				PlanType:  t.PlanType,
				Status:    t.Status,
				CreatedAt: t.CreatedAt,
			})
		}

		data := map[string]any{
			"Page":       "tenants",
			"Tenants":    tenants,
			"TotalCount": len(tenants),
		}

		renderTemplate(w, r, "tenants.html", data, ctx.Logger)
	}
}

func SocialAccountsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type socialAccountView struct {
			TenantID     uint
			TenantName   string
			Platform     string
			PlatformName string
			Mode         string
			ModeLabel    string
			Status       string
			Connected    bool
			Username     string
			ExternalID   string
			CreatedAt    time.Time
		}

		var accounts []storage.SocialAccount
		if err := ctx.DB.Order("tenant_id ASC, platform ASC, created_at DESC").Find(&accounts).Error; err != nil {
			ctx.Logger.Warn("failed to fetch social accounts", zap.Error(err))
		}

		// Preload tenant names for display
		tenantNames := map[uint]string{}
		if len(accounts) > 0 {
			var tenants []storage.Tenant
			ids := make([]uint, 0, len(accounts))
			seen := map[uint]struct{}{}
			for _, a := range accounts {
				if _, ok := seen[a.TenantID]; !ok {
					ids = append(ids, a.TenantID)
					seen[a.TenantID] = struct{}{}
				}
			}
			if len(ids) > 0 {
				if err := ctx.DB.Where("id IN ?", ids).Find(&tenants).Error; err != nil {
					ctx.Logger.Warn("failed to fetch tenants for social accounts", zap.Error(err))
				} else {
					for _, t := range tenants {
						tenantNames[t.ID] = t.Name
					}
				}
			}
		}

		viewAccounts := make([]socialAccountView, 0, len(accounts))
		for _, a := range accounts {
			platform := strings.ToLower(a.Platform)
			mode := strings.ToLower(a.Mode)

			// WhatsApp uses Gupshup; normalize label
			modeLabel := mode
			if platform == "whatsapp" {
				modeLabel = "gupshup"
			}
			if modeLabel == "" {
				modeLabel = "oauth2"
			}

			status := "disconnected"
			if a.Connected {
				status = "connected"
			}

			platformLabel := platform
			if len(platformLabel) > 0 {
				platformLabel = strings.ToUpper(platformLabel[:1]) + platformLabel[1:]
			}

			viewAccounts = append(viewAccounts, socialAccountView{
				TenantID:     a.TenantID,
				TenantName:   tenantNames[a.TenantID],
				Platform:     platform,
				PlatformName: platformLabel,
				Mode:         mode,
				ModeLabel:    strings.ToLower(modeLabel),
				Status:       status,
				Connected:    a.Connected,
				Username:     a.Username,
				ExternalID:   a.ExternalID,
				CreatedAt:    a.CreatedAt,
			})
		}

		data := map[string]any{
			"Accounts": viewAccounts,
		}

		renderTemplate(w, r, "social.html", data, ctx.Logger)
	}
}

// ✅ POST /admin/logout
func LogoutHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearAdminCookie(w)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	}
}
