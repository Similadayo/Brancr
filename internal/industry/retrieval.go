package industry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/storage/repositories"
	"go.uber.org/zap"
)

// RetrievalService provides industry-aware product/menu/service retrieval
type RetrievalService struct {
	industryService *Service
	productRepo     *repositories.ProductRepository
	menuItemRepo    *repositories.MenuItemRepository
	serviceRepo     *repositories.ServiceRepository
	logger          *zap.Logger
}

// NewRetrievalService creates a new retrieval service
func NewRetrievalService(industryService *Service, productRepo *repositories.ProductRepository, menuItemRepo *repositories.MenuItemRepository, serviceRepo *repositories.ServiceRepository, logger *zap.Logger) *RetrievalService {
	return &RetrievalService{
		industryService: industryService,
		productRepo:     productRepo,
		menuItemRepo:    menuItemRepo,
		serviceRepo:     serviceRepo,
		logger:          logger,
	}
}

// RetrievalResult represents a retrieved item (product, menu item, or service)
type RetrievalResult struct {
	Type        string // "product", "menu_item", "service"
	Name        string
	Description string
	Price       float64
	Currency    string
	Category    string
	Available   bool
	ImageURLs   []string               // Image URLs for the item
	Metadata    map[string]interface{} // Additional fields (variants, dietary info, etc.)
}

type negotiationPolicy struct {
	Mode     string
	MinPrice float64
	MaxPrice float64
}

const (
	negotiationModeDefault    = "default"
	negotiationModeFixed      = "fixed"
	negotiationModeNegotiable = "negotiable"
)

// SearchItems searches for items based on tenant's industry and query
func (r *RetrievalService) SearchItems(ctx context.Context, tenantID uuid.UUID, query string, limit int) ([]RetrievalResult, error) {
	if limit <= 0 {
		limit = 10
	}

	// Get tenant's industry capabilities
	capabilities, err := r.industryService.GetIndustryCapabilities(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get industry capabilities: %w", err)
	}

	var results []RetrievalResult
	negDefault := r.getNegotiationDefault(ctx, tenantID)

	// Search based on industry type
	if capabilities.HasProducts {
		products, err := r.productRepo.SearchByTenant(ctx, tenantID, query)
		if err != nil {
			r.logger.Warn("failed to search products", zap.Error(err))
		} else {
			for _, p := range products {
				results = append(results, r.productToResultWithNegotiation(p, negDefault))
			}
		}
	}

	if capabilities.HasMenu {
		menuItems, err := r.menuItemRepo.SearchByTenant(ctx, tenantID, query)
		if err != nil {
			r.logger.Warn("failed to search menu items", zap.Error(err))
		} else {
			for _, m := range menuItems {
				results = append(results, r.menuItemToResultWithNegotiation(m, negDefault))
			}
		}
	}

	if capabilities.HasServices {
		services, err := r.serviceRepo.SearchByTenant(ctx, tenantID, query)
		if err != nil {
			r.logger.Warn("failed to search services", zap.Error(err))
		} else {
			for _, s := range services {
				results = append(results, r.serviceToResultWithNegotiation(s, negDefault))
			}
		}
	}

	// Limit results
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetByCategory retrieves items by category based on tenant's industry
func (r *RetrievalService) GetByCategory(ctx context.Context, tenantID uuid.UUID, category string, limit int) ([]RetrievalResult, error) {
	if limit <= 0 {
		limit = 20
	}

	capabilities, err := r.industryService.GetIndustryCapabilities(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get industry capabilities: %w", err)
	}

	var results []RetrievalResult
	negDefault := r.getNegotiationDefault(ctx, tenantID)

	if capabilities.HasProducts {
		products, err := r.productRepo.GetByCategory(ctx, tenantID, category)
		if err != nil {
			r.logger.Warn("failed to get products by category", zap.Error(err))
		} else {
			for _, p := range products {
				results = append(results, r.productToResultWithNegotiation(p, negDefault))
			}
		}
	}

	if capabilities.HasMenu {
		menuItems, err := r.menuItemRepo.GetByCategory(ctx, tenantID, category)
		if err != nil {
			r.logger.Warn("failed to get menu items by category", zap.Error(err))
		} else {
			for _, m := range menuItems {
				results = append(results, r.menuItemToResultWithNegotiation(m, negDefault))
			}
		}
	}

	if capabilities.HasServices {
		services, err := r.serviceRepo.GetByCategory(ctx, tenantID, category)
		if err != nil {
			r.logger.Warn("failed to get services by category", zap.Error(err))
		} else {
			for _, s := range services {
				results = append(results, r.serviceToResultWithNegotiation(s, negDefault))
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetAllActive retrieves all active items for a tenant
func (r *RetrievalService) GetAllActive(ctx context.Context, tenantID uuid.UUID, limit int) ([]RetrievalResult, error) {
	if limit <= 0 {
		limit = 50
	}

	capabilities, err := r.industryService.GetIndustryCapabilities(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get industry capabilities: %w", err)
	}

	var results []RetrievalResult
	negDefault := r.getNegotiationDefault(ctx, tenantID)

	if capabilities.HasProducts {
		products, err := r.productRepo.GetActiveByTenant(ctx, tenantID)
		if err != nil {
			r.logger.Warn("failed to get active products", zap.Error(err))
		} else {
			for _, p := range products {
				results = append(results, r.productToResultWithNegotiation(p, negDefault))
			}
		}
	}

	if capabilities.HasMenu {
		menuItems, err := r.menuItemRepo.GetActiveByTenant(ctx, tenantID)
		if err != nil {
			r.logger.Warn("failed to get active menu items", zap.Error(err))
		} else {
			for _, m := range menuItems {
				results = append(results, r.menuItemToResultWithNegotiation(m, negDefault))
			}
		}
	}

	if capabilities.HasServices {
		services, err := r.serviceRepo.GetActiveByTenant(ctx, tenantID)
		if err != nil {
			r.logger.Warn("failed to get active services", zap.Error(err))
		} else {
			for _, s := range services {
				results = append(results, r.serviceToResultWithNegotiation(s, negDefault))
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetItemsWithImages returns image URLs for items that match the customer's message
func (r *RetrievalService) GetItemsWithImages(ctx context.Context, tenantID uuid.UUID, message string) ([]string, error) {
	messageLower := strings.ToLower(message)

	// Check if customer explicitly wants to see images (stronger detection)
	wantsImages := strings.Contains(messageLower, "image") ||
		strings.Contains(messageLower, "picture") ||
		strings.Contains(messageLower, "photo") ||
		strings.Contains(messageLower, "pic") ||
		strings.Contains(messageLower, "show me") ||
		strings.Contains(messageLower, "show") && (strings.Contains(messageLower, "picture") || strings.Contains(messageLower, "image") || strings.Contains(messageLower, "photo")) ||
		(strings.Contains(messageLower, "see") && (strings.Contains(messageLower, "picture") || strings.Contains(messageLower, "image") || strings.Contains(messageLower, "photo"))) ||
		(strings.Contains(messageLower, "send") && (strings.Contains(messageLower, "picture") || strings.Contains(messageLower, "image") || strings.Contains(messageLower, "photo"))) ||
		strings.Contains(messageLower, "can i see") ||
		strings.Contains(messageLower, "i want to see")

	// Also check for menu questions that would benefit from images
	isMenuQuestion := strings.Contains(messageLower, "menu") ||
		strings.Contains(messageLower, "what do you have") ||
		strings.Contains(messageLower, "what's available") ||
		strings.Contains(messageLower, "what can i get")

	if !wantsImages && !isMenuQuestion {
		return nil, nil
	}

	// Get items based on message
	var results []RetrievalResult
	var err error

	// If customer asks for picture of specific item, search for that item
	if wantsImages {
		// Extract item name from message (e.g., "can i see the image of the banana bread?" -> "banana bread")
		query := messageLower
		// Remove common picture-related phrases and words to get the item name
		query = strings.ReplaceAll(query, "can i see", "")
		query = strings.ReplaceAll(query, "i want to see", "")
		query = strings.ReplaceAll(query, "show me", "")
		query = strings.ReplaceAll(query, "picture", "")
		query = strings.ReplaceAll(query, "image", "")
		query = strings.ReplaceAll(query, "photo", "")
		query = strings.ReplaceAll(query, "pic", "")
		query = strings.ReplaceAll(query, "of", "")
		query = strings.ReplaceAll(query, "the", "")
		query = strings.ReplaceAll(query, "a", "")
		query = strings.ReplaceAll(query, "an", "")
		query = strings.ReplaceAll(query, "?", "")
		query = strings.ReplaceAll(query, ".", "")
		// Remove multiple spaces
		query = strings.Join(strings.Fields(query), " ")
		query = strings.TrimSpace(query)

		if len(query) > 3 && !strings.Contains(messageLower, "menu") {
			// Search for specific item
			if len(query) > 50 {
				query = query[:50]
			}
			r.logger.Info("🔍 Searching for items with query",
				zap.String("query", query),
				zap.Stringer("tenant_id", tenantID),
				zap.String("original_message", message))
			results, err = r.SearchItems(ctx, tenantID, query, 5)
			if err != nil {
				r.logger.Warn("Search failed, falling back to GetAllActive", zap.Error(err))
				results, err = r.GetAllActive(ctx, tenantID, 10)
			}
			r.logger.Info("🔍 Search results",
				zap.Int("result_count", len(results)),
				zap.String("query", query))

			// If search returned no results, try getting all active items as fallback
			if len(results) == 0 {
				r.logger.Info("⚠️ Search returned no results, trying GetAllActive as fallback")
				results, err = r.GetAllActive(ctx, tenantID, 10)
				if err != nil {
					r.logger.Warn("GetAllActive also failed", zap.Error(err))
				} else {
					r.logger.Info("📋 Fallback GetAllActive returned items",
						zap.Int("count", len(results)))
				}
			}
		} else {
			// Get all items if asking about menu or general
			r.logger.Info("📋 Getting all active items (menu question or generic request)")
			results, err = r.GetAllActive(ctx, tenantID, 10)
		}
	} else if isMenuQuestion {
		// Get all items for menu questions
		results, err = r.GetAllActive(ctx, tenantID, 10)
	} else {
		// Search for specific items
		query := message
		if len(query) > 50 {
			query = query[:50]
		}
		results, err = r.SearchItems(ctx, tenantID, query, 5)
	}

	if err != nil {
		return nil, err
	}

	// Extract image URLs - match items to what customer specifically asked for
	var imageURLs []string
	messageLower = strings.ToLower(message)

	r.logger.Info("🔍 Processing search results for images",
		zap.Int("result_count", len(results)),
		zap.String("original_message", message))

	// If customer asks for specific item, only return that item's images
	// Otherwise, return images from all matching items
	for i, item := range results {
		r.logger.Info("🔍 Checking item",
			zap.Int("item_index", i),
			zap.String("item_name", item.Name),
			zap.Int("image_count", len(item.ImageURLs)))

		if len(item.ImageURLs) == 0 {
			r.logger.Info("⚠️ Item has no images, skipping",
				zap.String("item_name", item.Name))
			continue
		}

		// Check if this item matches what customer asked for
		itemNameLower := strings.ToLower(item.Name)
		itemDescLower := strings.ToLower(item.Description)

		// If customer asked for specific item (e.g., "picture of banana bread")
		// Since we already searched for the item, if it's in results, it matches
		// Only do additional filtering if we got ALL items (not a specific search)
		matchesQuery := true
		extractedQuery := ""
		if wantsImages && len(results) < 10 {
			// We did a specific search, so all results should match
			// But let's still verify the item name matches what customer asked for
			extractedQuery = messageLower
			extractedQuery = strings.ReplaceAll(extractedQuery, "can i see", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "i want to see", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "show me", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "picture", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "image", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "photo", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "pic", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "of", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "the", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "a", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "an", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, "?", "")
			extractedQuery = strings.ReplaceAll(extractedQuery, ".", "")
			extractedQuery = strings.TrimSpace(extractedQuery)

			// If we have a specific query, verify at least one word matches
			if len(extractedQuery) > 3 {
				queryWords := strings.Fields(extractedQuery)
				matches := 0
				for _, word := range queryWords {
					if len(word) > 2 {
						if strings.Contains(itemNameLower, word) || strings.Contains(itemDescLower, word) {
							matches++
						}
					}
				}
				// If no words match, this might be a false positive from search
				// But be lenient - if search found it, probably it's relevant
				matchesQuery = matches > 0 || len(queryWords) == 0
				r.logger.Info("🔍 Verifying match",
					zap.String("item_name", item.Name),
					zap.String("query", extractedQuery),
					zap.Int("matches", matches),
					zap.Bool("matches_query", matchesQuery))
			}
		}

		if matchesQuery {
			// Add first image from matching item
			imageURLs = append(imageURLs, item.ImageURLs[0])
			r.logger.Info("✅ Matched item for image",
				zap.String("item_name", item.Name),
				zap.String("image_url", item.ImageURLs[0]))
			if len(imageURLs) >= 5 {
				break
			}
		} else {
			r.logger.Info("❌ Item did not match query",
				zap.String("item_name", item.Name),
				zap.String("extracted_query", extractedQuery))
		}
	}

	r.logger.Info("📸 Final image URLs extracted",
		zap.Int("count", len(imageURLs)),
		zap.Strings("urls", imageURLs),
		zap.String("original_message", message))

	// Final fallback: if no images found but we have results, just return first item with images
	if len(imageURLs) == 0 && len(results) > 0 {
		r.logger.Warn("⚠️ No images extracted from search results, trying fallback",
			zap.Int("search_result_count", len(results)),
			zap.String("message", message))

		// Just return the first item that has images (no filtering)
		for _, item := range results {
			if len(item.ImageURLs) > 0 {
				imageURLs = append(imageURLs, item.ImageURLs[0])
				r.logger.Info("✅ Fallback: Using first item with images",
					zap.String("item_name", item.Name),
					zap.String("image_url", item.ImageURLs[0]))
				break
			}
		}
	}

	if len(imageURLs) == 0 {
		r.logger.Warn("⚠️ No images found at all",
			zap.Int("search_result_count", len(results)),
			zap.String("message", message),
			zap.Stringer("tenant_id", tenantID))
	}

	return imageURLs, nil
}

// FormatForAI formats retrieval results as a string for AI context
func (r *RetrievalService) FormatForAI(items []RetrievalResult) string {
	if len(items) == 0 {
		return "No items available."
	}

	var parts []string
	for _, item := range items {
		priceStr := fmt.Sprintf("%.2f %s", item.Price, item.Currency)
		if item.Price == 0 {
			priceStr = "Price on request"
		}
		negStr := formatNegotiationForAI(item)
		if negStr != "" {
			priceStr = priceStr + " (" + negStr + ")"
		}

		line := fmt.Sprintf("- %s (%s): %s - %s", item.Name, item.Category, priceStr, item.Description)

		// Add image URLs if available (for AI context - images will be sent separately)
		if len(item.ImageURLs) > 0 {
			line += fmt.Sprintf(" [Has %d image(s)]", len(item.ImageURLs))
		}

		// Add metadata if available
		if len(item.Metadata) > 0 {
			var metaParts []string
			if variants, ok := item.Metadata["variants"].(map[string]interface{}); ok {
				var variantStrs []string
				for k, v := range variants {
					variantStrs = append(variantStrs, fmt.Sprintf("%s: %v", k, v))
				}
				if len(variantStrs) > 0 {
					metaParts = append(metaParts, "Variants: "+strings.Join(variantStrs, ", "))
				}
			}
			if dietary, ok := item.Metadata["dietary_info"].([]string); ok && len(dietary) > 0 {
				metaParts = append(metaParts, "Dietary: "+strings.Join(dietary, ", "))
			}
			if prepTime, ok := item.Metadata["preparation_time"].(int); ok && prepTime > 0 {
				metaParts = append(metaParts, fmt.Sprintf("Prep time: %d mins", prepTime))
			}
			if stock, ok := item.Metadata["stock"].(int); ok {
				if stock >= 0 {
					metaParts = append(metaParts, fmt.Sprintf("Stock: %d", stock))
				}
			}
			if len(metaParts) > 0 {
				line += " [" + strings.Join(metaParts, "; ") + "]"
			}
		}

		if !item.Available {
			line += " (Currently unavailable)"
		}

		parts = append(parts, line)
	}

	return strings.Join(parts, "\n")
}

func formatNegotiationForAI(item RetrievalResult) string {
	mode, _ := item.Metadata["negotiation_mode"].(string)
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case negotiationModeFixed:
		return "Fixed price"
	case negotiationModeNegotiable:
		min, _ := item.Metadata["negotiation_min_price"].(float64)
		max, _ := item.Metadata["negotiation_max_price"].(float64)
		if min > 0 && max > 0 {
			// For AI internal use, mark negotiable but DO NOT emit numeric min/max in plaintext used for customer-facing prompt.
			return "Negotiable - internal range hidden"
		}
		return "Negotiable"
	default:
		return ""
	}
}

// Helper methods to convert models to RetrievalResult

func (r *RetrievalService) productToResult(p storage.Product) RetrievalResult {
	result := RetrievalResult{
		Type:        "product",
		Name:        p.Name,
		Description: p.Description,
		Price:       p.Price,
		Currency:    p.Currency,
		Category:    p.Category,
		Available:   p.IsActive && (p.StockCount < 0 || p.StockCount > 0),
		Metadata:    make(map[string]interface{}),
	}

	// Parse variants
	if p.Variants != "" {
		var variants map[string]interface{}
		if err := json.Unmarshal([]byte(p.Variants), &variants); err == nil {
			result.Metadata["variants"] = variants
		}
	}

	// Parse tags
	if p.Tags != "" {
		var tags []string
		if err := json.Unmarshal([]byte(p.Tags), &tags); err == nil {
			result.Metadata["tags"] = tags
		}
	}

	// Stock info
	result.Metadata["stock"] = p.StockCount
	result.Metadata["availability"] = p.Availability

	// Parse images
	if p.Images != "" {
		var images []string
		if err := json.Unmarshal([]byte(p.Images), &images); err == nil && len(images) > 0 {
			result.ImageURLs = images
		}
	}

	return result
}

func (r *RetrievalService) productToResultWithNegotiation(p storage.Product, def negotiationPolicy) RetrievalResult {
	result := r.productToResult(p)
	applyNegotiationToResult(&result, p.NegotiationMode, p.NegotiationMinPrice, p.NegotiationMaxPrice, def)
	return result
}

func (r *RetrievalService) menuItemToResult(m storage.MenuItem) RetrievalResult {
	result := RetrievalResult{
		Type:        "menu_item",
		Name:        m.Name,
		Description: m.Description,
		Price:       m.Price,
		Currency:    m.Currency,
		Category:    m.Category,
		Available:   m.IsActive && m.Availability != "unavailable",
		Metadata:    make(map[string]interface{}),
	}

	// Parse dietary info
	if m.DietaryInfo != "" {
		var dietary []string
		if err := json.Unmarshal([]byte(m.DietaryInfo), &dietary); err == nil {
			result.Metadata["dietary_info"] = dietary
		}
	}

	// Preparation time
	if m.PreparationTime > 0 {
		result.Metadata["preparation_time"] = m.PreparationTime
	}

	// Spice level
	if m.SpiceLevel != "" {
		result.Metadata["spice_level"] = m.SpiceLevel
	}

	result.Metadata["availability"] = m.Availability

	// Parse images
	if m.Images != "" {
		var images []string
		if err := json.Unmarshal([]byte(m.Images), &images); err == nil && len(images) > 0 {
			result.ImageURLs = images
		}
	}

	return result
}

func (r *RetrievalService) menuItemToResultWithNegotiation(m storage.MenuItem, def negotiationPolicy) RetrievalResult {
	result := r.menuItemToResult(m)
	applyNegotiationToResult(&result, m.NegotiationMode, m.NegotiationMinPrice, m.NegotiationMaxPrice, def)
	return result
}

func (r *RetrievalService) serviceToResult(s storage.Service) RetrievalResult {
	result := RetrievalResult{
		Type:        "service",
		Name:        s.Name,
		Description: s.Description,
		Price:       0, // Services may have complex pricing
		Currency:    "NGN",
		Category:    s.Category,
		Available:   s.IsActive,
		Metadata:    make(map[string]interface{}),
	}

	// Parse pricing
	if s.Pricing != "" {
		var pricing map[string]interface{}
		if err := json.Unmarshal([]byte(s.Pricing), &pricing); err == nil {
			result.Metadata["pricing"] = pricing
			if amount, ok := pricing["amount"].(float64); ok {
				result.Price = amount
			}
		}
	}

	// Parse packages
	if s.Packages != "" {
		var packages []interface{}
		if err := json.Unmarshal([]byte(s.Packages), &packages); err == nil {
			result.Metadata["packages"] = packages
		}
	}

	// Duration and deliverables
	if s.Duration != "" {
		result.Metadata["duration"] = s.Duration
	}
	if s.Deliverables != "" {
		result.Metadata["deliverables"] = s.Deliverables
	}

	return result
}

func (r *RetrievalService) serviceToResultWithNegotiation(s storage.Service, def negotiationPolicy) RetrievalResult {
	result := r.serviceToResult(s)
	applyNegotiationToResult(&result, s.NegotiationMode, s.NegotiationMinPrice, s.NegotiationMaxPrice, def)
	return result
}

func (r *RetrievalService) getNegotiationDefault(ctx context.Context, tenantID uuid.UUID) negotiationPolicy {
	def := negotiationPolicy{Mode: negotiationModeFixed, MinPrice: 0, MaxPrice: 0}
	if r.industryService == nil {
		return def
	}
	settings, err := r.industryService.GetIndustrySettings(ctx, tenantID)
	if err != nil {
		return def
	}
	raw, ok := settings["negotiation_default"].(map[string]interface{})
	if !ok {
		return def
	}
	if mode, ok := raw["mode"].(string); ok {
		def.Mode = strings.TrimSpace(strings.ToLower(mode))
	}
	if min, ok := raw["min_price"].(float64); ok {
		def.MinPrice = min
	}
	if max, ok := raw["max_price"].(float64); ok {
		def.MaxPrice = max
	}
	if def.Mode != negotiationModeFixed && def.Mode != negotiationModeNegotiable {
		def.Mode = negotiationModeFixed
		def.MinPrice = 0
		def.MaxPrice = 0
		return def
	}
	if def.Mode == negotiationModeNegotiable {
		if !(def.MinPrice > 0 && def.MaxPrice > 0 && def.MinPrice <= def.MaxPrice) {
			def.Mode = negotiationModeFixed
			def.MinPrice = 0
			def.MaxPrice = 0
		}
	}
	return def
}

func applyNegotiationToResult(result *RetrievalResult, itemMode string, itemMin, itemMax float64, def negotiationPolicy) {
	mode := strings.TrimSpace(strings.ToLower(itemMode))
	if mode == "" {
		mode = negotiationModeDefault
	}

	effective := negotiationPolicy{Mode: negotiationModeFixed, MinPrice: 0, MaxPrice: 0}
	switch mode {
	case negotiationModeFixed:
		effective = negotiationPolicy{Mode: negotiationModeFixed, MinPrice: 0, MaxPrice: 0}
	case negotiationModeNegotiable:
		if itemMin > 0 && itemMax > 0 && itemMin <= itemMax {
			effective = negotiationPolicy{Mode: negotiationModeNegotiable, MinPrice: itemMin, MaxPrice: itemMax}
		} else {
			effective = negotiationPolicy{Mode: negotiationModeFixed, MinPrice: 0, MaxPrice: 0}
		}
	default:
		effective = def
	}

	if result.Metadata == nil {
		result.Metadata = make(map[string]interface{})
	}
	result.Metadata["negotiation_mode"] = effective.Mode
	result.Metadata["negotiation_min_price"] = effective.MinPrice
	result.Metadata["negotiation_max_price"] = effective.MaxPrice
}
