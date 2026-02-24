package tenantapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

type MagicProfileRequest struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

type MagicProfileResponse struct {
	Success bool        `json:"success"`
	Profile interface{} `json:"profile"` // ai.BusinessProfileResult
	Message string      `json:"message,omitempty"`
}

// MagicProfile handles the AI-driven "magic setup" by analyzing a URL or description
func (h *Handler) MagicProfile(w http.ResponseWriter, r *http.Request) {
	var req MagicProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input: need at least one
	if req.URL == "" && req.Description == "" {
		http.Error(w, "Please provide either a Website URL or a Business Description", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var scrapedContent string
	var err error

	// 1. Scrape if URL provided
	if req.URL != "" {
		// Basic validation/cleanup
		if !strings.HasPrefix(req.URL, "http") {
			req.URL = "https://" + req.URL
		}
		scrapedContent, err = utils.ScrapeWebsiteContent(req.URL)
		if err != nil {
			h.Logger.Warn("Failed to scrape website in MagicProfile",
				zap.String("url", req.URL),
				zap.Error(err),
			)
			// Continue with empty scraped content, relying on description or just failing gracefully
			scrapedContent = "(Scraping failed or no content found for " + req.URL + ")"
		}
	}

	// 2. Call AI
	profile, err := h.AIClient.GenerateBusinessProfile(ctx, scrapedContent, req.Description)
	if err != nil {
		h.Logger.Error("AI Generation failed for MagicProfile",
			zap.Error(err),
		)
		http.Error(w, "Failed to analyze business profile. Please try manual setup.", http.StatusInternalServerError)
		return
	}

	// 3. Return Response
	resp := MagicProfileResponse{
		Success: true,
		Profile: profile,
	}

	if profile.ConfidenceScore == "low" {
		resp.Message = "We couldn't find enough information to be 100% sure. Please review carefully."
		if profile.LowConfidenceMsg != "" {
			resp.Message = profile.LowConfidenceMsg
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// MagicKnowledgeRequest input for knowledge generation
type MagicKnowledgeRequest struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// MagicKnowledge handles auto-filling Business Details (FAQs, Keywords, KB)
func (h *Handler) MagicKnowledge(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tenantID := claims.TenantID

	var req MagicKnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.URL == "" && req.Description == "" {
		// Fallback: Try to fetch existing profile description from DB if user inputs are empty
		var profile storage.BusinessProfile
		if err := h.DB.Select("description, website").Where("tenant_id = ?", tenantID).First(&profile).Error; err == nil {
			req.Description = profile.Description
			req.URL = profile.Website
		}

		if req.URL == "" && req.Description == "" {
			http.Error(w, "Please provide description or ensure profile is set", http.StatusBadRequest)
			return
		}
	}

	ctx := r.Context()
	var scrapedContent string
	var err error

	// 1. Scrape if URL provided
	if req.URL != "" {
		if !strings.HasPrefix(req.URL, "http") {
			req.URL = "https://" + req.URL
		}
		scrapedContent, err = utils.ScrapeWebsiteContent(req.URL)
		if err != nil {
			h.Logger.Warn("Failed to scrape website in MagicKnowledge", zap.String("url", req.URL), zap.Error(err))
		}
	}

	// 2. Call AI
	knowledge, err := h.AIClient.GenerateBusinessKnowledge(ctx, scrapedContent, req.Description)
	if err != nil {
		h.Logger.Error("AI Generation failed for MagicKnowledge", zap.Error(err))
		http.Error(w, "Failed to generate knowledge", http.StatusInternalServerError)
		return
	}

	// 3. Save to DB

	// A. Save FAQs
	for _, faq := range knowledge.FAQs {
		h.DB.Create(&storage.BusinessFAQ{
			TenantID: tenantID,
			Question: faq.Question,
			Answer:   faq.Answer,
		})
	}

	// B. Save Keywords
	keywordsStr := strings.Join(knowledge.Keywords, ",")
	h.DB.Model(&storage.BusinessProfile{}).Where("tenant_id = ?", tenantID).Update("keywords", keywordsStr)

	// C. Save Knowledge Base
	var kbBuilder strings.Builder
	for _, kb := range knowledge.KnowledgeBase {
		// Save structured data
		h.DB.Create(&storage.ServiceKnowledge{
			TenantID: tenantID,
			Title:    kb.Title,
			Category: kb.Category,
			Content:  kb.Content,
			IsActive: true,
		})

		// Append to text summary
		kbBuilder.WriteString(fmt.Sprintf("• %s: %s\n\n", kb.Title, kb.Content))
	}

	// Update the profile's simple KnowledgeBase text field for quick display
	h.DB.Model(&storage.BusinessProfile{}).Where("tenant_id = ?", tenantID).Update("knowledge_base", kbBuilder.String())

	h.Logger.Info("✅ MagicKnowledge applied successfully",
		zap.Stringer("tenant_id", tenantID),
		zap.Int("faqs", len(knowledge.FAQs)),
		zap.Int("kb_items", len(knowledge.KnowledgeBase)))

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    knowledge,
		"message": fmt.Sprintf("Added %d FAQs and %d Knowledge Articles!", len(knowledge.FAQs), len(knowledge.KnowledgeBase)),
	})
}

// ChecklistItem represents a single onboarding task
type ChecklistItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Complete    bool   `json:"complete"`
	ActionURL   string `json:"action_url,omitempty"`
	Icon        string `json:"icon,omitempty"`
	XP          int    `json:"xp"`
}

// GetOnboardingChecklist returns the current onboarding progress for the "Adventure" widget
func (h *Handler) GetOnboardingChecklist(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tenantID := claims.TenantID

	ctx := r.Context()

	// Fetch tenant and industry to determine offering type
	var tenant storage.Tenant
	if err := h.DB.WithContext(ctx).First(&tenant, tenantID).Error; err != nil {
		h.Logger.Error("Failed to fetch tenant for checklist", zap.Error(err))
		http.Error(w, "Failed to load checklist", http.StatusInternalServerError)
		return
	}

	// Fetch industry details (if set)
	var industry storage.Industry
	hasIndustry := false
	if tenant.IndustryID != uuid.Nil {
		if err := h.DB.WithContext(ctx).First(&industry, tenant.IndustryID).Error; err == nil {
			hasIndustry = true
		}
	}

	// Check various completion states
	var profileCount, telegramCount, socialCount, postCount, billingCount, offeringCount int64

	// 1. Business Profile
	h.DB.WithContext(ctx).Table("business_profiles").Where("tenant_id = ?", tenantID).Count(&profileCount)

	// 2. Telegram Bot connected
	h.DB.WithContext(ctx).Table("tenants").Where("id = ? AND telegram_bot_token != '' AND telegram_bot_token IS NOT NULL", tenantID).Count(&telegramCount)

	// 3. Social Account connected (Instagram, Facebook, etc)
	h.DB.WithContext(ctx).Table("social_accounts").Where("tenant_id = ? AND connected = true", tenantID).Count(&socialCount)

	// 4. First post scheduled
	h.DB.WithContext(ctx).Table("scheduled_posts").Where("tenant_id = ?", tenantID).Count(&postCount)

	// 5. Billing setup (has a plan or payment method)
	h.DB.WithContext(ctx).Table("tenants").Where("id = ? AND plan_type != 'trial'", tenantID).Count(&billingCount)

	// 6. Check for product/menu/service based on industry
	offeringTitle := "Add Your First Product"
	offeringDesc := "List a product you sell to start accepting orders."
	offeringURL := "/app/products/create"
	offeringIcon := "🛍️"

	if hasIndustry {
		if industry.HasMenu {
			// Food & Beverage: Menu Items
			h.DB.WithContext(ctx).Table("menu_items").Where("tenant_id = ? AND is_active = true", tenantID).Count(&offeringCount)
			offeringTitle = "Create Your First Menu Item"
			offeringDesc = "Add a delicious item to your menu for customers to order."
			offeringURL = "/app/menu/create"
			offeringIcon = "🍽️"
		} else if industry.HasServices {
			// Consultant/Agency: Services
			h.DB.WithContext(ctx).Table("services").Where("tenant_id = ? AND is_active = true", tenantID).Count(&offeringCount)
			offeringTitle = "Add Your First Service"
			offeringDesc = "List a service you offer so clients can book you."
			offeringURL = "/app/services/create"
			offeringIcon = "💼"
		} else if industry.HasProducts {
			// E-commerce: Products
			h.DB.WithContext(ctx).Table("products").Where("tenant_id = ? AND is_active = true", tenantID).Count(&offeringCount)
			offeringTitle = "Add Your First Product"
			offeringDesc = "List a product you sell to start accepting orders."
			offeringURL = "/app/products/create"
			offeringIcon = "🛍️"
		} else {
			// Fallback: default to products
			h.DB.WithContext(ctx).Table("products").Where("tenant_id = ? AND is_active = true", tenantID).Count(&offeringCount)
		}
	} else {
		// No industry set: default to products
		h.DB.WithContext(ctx).Table("products").Where("tenant_id = ? AND is_active = true", tenantID).Count(&offeringCount)
	}

	checklist := []ChecklistItem{
		{
			ID:          "profile",
			Title:       "Set Up Your Brand",
			Description: "Tell us about your business so our AI can help you better.",
			Complete:    profileCount > 0,
			ActionURL:   "/app/onboarding/profile",
			Icon:        "✨",
			XP:          100,
		},
		{
			ID:          "telegram",
			Title:       "Connect Telegram",
			Description: "Enable AI-powered customer support on Telegram.",
			Complete:    telegramCount > 0,
			ActionURL:   "/app/settings/telegram",
			Icon:        "💬",
			XP:          200,
		},
		{
			ID:          "offering",
			Title:       offeringTitle,
			Description: offeringDesc,
			Complete:    offeringCount > 0,
			ActionURL:   offeringURL,
			Icon:        offeringIcon,
			XP:          300,
		},
		{
			ID:          "social",
			Title:       "Connect Social Media",
			Description: "Link your Instagram, Facebook, or TikTok to schedule posts.",
			Complete:    socialCount > 0,
			ActionURL:   "/app/settings/social",
			Icon:        "📱",
			XP:          200,
		},
		{
			ID:          "post",
			Title:       "Schedule Your First Post",
			Description: "Create and schedule your first AI-optimized social media post.",
			Complete:    postCount > 0,
			ActionURL:   "/app/posts/create",
			Icon:        "📅",
			XP:          500,
		},
		{
			ID:          "billing",
			Title:       "Upgrade Your Plan",
			Description: "Unlock unlimited features and remove trial limits.",
			Complete:    billingCount > 0,
			ActionURL:   "/app/billing",
			Icon:        "💳",
			XP:          300,
		},
	}

	// Calculate total XP and progress
	totalXP := 0
	earnedXP := 0
	completedCount := 0
	for _, item := range checklist {
		totalXP += item.XP
		if item.Complete {
			earnedXP += item.XP
			completedCount++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"checklist":       checklist,
		"total_tasks":     len(checklist),
		"completed_tasks": completedCount,
		"progress":        float64(completedCount) / float64(len(checklist)) * 100,
		"earned_xp":       earnedXP,
		"total_xp":        totalXP,
	})
}

// ApplyIndustryPackRequest is the request body for applying an industry pack
type ApplyIndustryPackRequest struct {
	IndustryID string `json:"industry_id"`
}

// IndustryPackResponse is the response after applying an industry pack
type IndustryPackResponse struct {
	Success   bool     `json:"success"`
	Industry  string   `json:"industry"`
	Tone      string   `json:"tone"`
	Templates []string `json:"templates_added"`
	Message   string   `json:"message"`
}

// ApplyIndustryPack applies industry-specific defaults (tone, templates, escalation rules) to a tenant
func (h *Handler) ApplyIndustryPack(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tenantID := claims.TenantID
	ctx := r.Context()

	var req ApplyIndustryPackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.IndustryID == "" {
		http.Error(w, "industry_id is required", http.StatusBadRequest)
		return
	}

	// Parse industry ID
	industryID, err := parseUUID(req.IndustryID)
	if err != nil {
		http.Error(w, "Invalid industry_id format", http.StatusBadRequest)
		return
	}

	// 1. Fetch Industry
	var industry storage.Industry
	if err := h.DB.WithContext(ctx).First(&industry, industryID).Error; err != nil {
		http.Error(w, "Industry not found", http.StatusNotFound)
		return
	}

	// 2. Fetch Industry Defaults (if exists)
	var defaults storage.IndustryDefaults
	hasDefaults := true
	if err := h.DB.WithContext(ctx).Where("industry_id = ?", industryID).First(&defaults).Error; err != nil {
		hasDefaults = false
	}

	// 3. Update Tenant's Industry
	if err := h.DB.WithContext(ctx).Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("industry_id", industryID).Error; err != nil {
		h.Logger.Error("Failed to update tenant industry", zap.Error(err))
		http.Error(w, "Failed to apply industry", http.StatusInternalServerError)
		return
	}

	// 4. Apply Persona Defaults
	appliedTone := "Friendly and professional"
	if hasDefaults && defaults.DefaultTone != "" {
		appliedTone = defaults.DefaultTone
	}

	// Upsert Persona
	persona := storage.Persona{
		TenantID: tenantID,
		Tone:     appliedTone,
	}
	if hasDefaults && defaults.DefaultStyle != "" {
		persona.StyleNotes = defaults.DefaultStyle
	}
	h.DB.WithContext(ctx).Where("tenant_id = ?", tenantID).Assign(persona).FirstOrCreate(&persona)

	// 5. Copy Industry Templates to Tenant (as RecurringTemplates or just return info)
	var industryTemplates []storage.IndustryTemplate
	h.DB.WithContext(ctx).Where("industry_id = ? AND is_active = true", industryID).Order("sort_order").Find(&industryTemplates)

	templateNames := make([]string, 0, len(industryTemplates))
	for _, t := range industryTemplates {
		templateNames = append(templateNames, t.Name)
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(IndustryPackResponse{
		Success:   true,
		Industry:  industry.Name,
		Tone:      appliedTone,
		Templates: templateNames,
		Message:   "Industry pack applied! Your AI is now configured for " + industry.Name + ".",
	})
}

// parseUUID is a helper to parse UUID strings
func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

// GetTelegramConnectLink generates a one-time deep link for Telegram bot connection
// GET /tenant/telegram-connect-link
func (h *Handler) GetTelegramConnectLink(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Generate a secure random token
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		h.Logger.Error("Failed to generate connect token", zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(tokenBytes)

	// Token expires in 24 hours (extended to prevent premature expiration)
	expires := time.Now().Add(24 * time.Hour)

	// Save token to tenant
	if err := h.DB.Model(&storage.Tenant{}).Where("id = ?", claims.TenantID).Updates(map[string]interface{}{
		"telegram_connect_token":   token,
		"telegram_connect_expires": expires,
	}).Error; err != nil {
		h.Logger.Error("Failed to save connect token", zap.Error(err))
		http.Error(w, "Failed to generate link", http.StatusInternalServerError)
		return
	}

	// Get bot username from environment or default
	botUsername := os.Getenv("BRANCR_BOT_USERNAME")
	if botUsername == "" {
		botUsername = "BrancrBot" // Default
	}

	// Generate deep link
	deepLink := fmt.Sprintf("https://t.me/%s?start=%s", botUsername, token)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"link":       deepLink,
		"expires_at": expires.Format(time.RFC3339),
		"expires_in": 900, // seconds
	})
}
