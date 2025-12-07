package industry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	Type        string   // "product", "menu_item", "service"
	Name        string
	Description string
	Price        float64
	Currency     string
	Category     string
	Available    bool
	ImageURLs    []string // Image URLs for the item
	Metadata     map[string]interface{} // Additional fields (variants, dietary info, etc.)
}

// SearchItems searches for items based on tenant's industry and query
func (r *RetrievalService) SearchItems(ctx context.Context, tenantID uint, query string, limit int) ([]RetrievalResult, error) {
	if limit <= 0 {
		limit = 10
	}

	// Get tenant's industry capabilities
	capabilities, err := r.industryService.GetIndustryCapabilities(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get industry capabilities: %w", err)
	}

	var results []RetrievalResult

	// Search based on industry type
	if capabilities.HasProducts {
		products, err := r.productRepo.SearchByTenant(ctx, tenantID, query)
		if err != nil {
			r.logger.Warn("failed to search products", zap.Error(err))
		} else {
			for _, p := range products {
				results = append(results, r.productToResult(p))
			}
		}
	}

	if capabilities.HasMenu {
		menuItems, err := r.menuItemRepo.SearchByTenant(ctx, tenantID, query)
		if err != nil {
			r.logger.Warn("failed to search menu items", zap.Error(err))
		} else {
			for _, m := range menuItems {
				results = append(results, r.menuItemToResult(m))
			}
		}
	}

	if capabilities.HasServices {
		services, err := r.serviceRepo.SearchByTenant(ctx, tenantID, query)
		if err != nil {
			r.logger.Warn("failed to search services", zap.Error(err))
		} else {
			for _, s := range services {
				results = append(results, r.serviceToResult(s))
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
func (r *RetrievalService) GetByCategory(ctx context.Context, tenantID uint, category string, limit int) ([]RetrievalResult, error) {
	if limit <= 0 {
		limit = 20
	}

	capabilities, err := r.industryService.GetIndustryCapabilities(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get industry capabilities: %w", err)
	}

	var results []RetrievalResult

	if capabilities.HasProducts {
		products, err := r.productRepo.GetByCategory(ctx, tenantID, category)
		if err != nil {
			r.logger.Warn("failed to get products by category", zap.Error(err))
		} else {
			for _, p := range products {
				results = append(results, r.productToResult(p))
			}
		}
	}

	if capabilities.HasMenu {
		menuItems, err := r.menuItemRepo.GetByCategory(ctx, tenantID, category)
		if err != nil {
			r.logger.Warn("failed to get menu items by category", zap.Error(err))
		} else {
			for _, m := range menuItems {
				results = append(results, r.menuItemToResult(m))
			}
		}
	}

	if capabilities.HasServices {
		services, err := r.serviceRepo.GetByCategory(ctx, tenantID, category)
		if err != nil {
			r.logger.Warn("failed to get services by category", zap.Error(err))
		} else {
			for _, s := range services {
				results = append(results, r.serviceToResult(s))
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetAllActive retrieves all active items for a tenant
func (r *RetrievalService) GetAllActive(ctx context.Context, tenantID uint, limit int) ([]RetrievalResult, error) {
	if limit <= 0 {
		limit = 50
	}

	capabilities, err := r.industryService.GetIndustryCapabilities(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get industry capabilities: %w", err)
	}

	var results []RetrievalResult

	if capabilities.HasProducts {
		products, err := r.productRepo.GetActiveByTenant(ctx, tenantID)
		if err != nil {
			r.logger.Warn("failed to get active products", zap.Error(err))
		} else {
			for _, p := range products {
				results = append(results, r.productToResult(p))
			}
		}
	}

	if capabilities.HasMenu {
		menuItems, err := r.menuItemRepo.GetActiveByTenant(ctx, tenantID)
		if err != nil {
			r.logger.Warn("failed to get active menu items", zap.Error(err))
		} else {
			for _, m := range menuItems {
				results = append(results, r.menuItemToResult(m))
			}
		}
	}

	if capabilities.HasServices {
		services, err := r.serviceRepo.GetActiveByTenant(ctx, tenantID)
		if err != nil {
			r.logger.Warn("failed to get active services", zap.Error(err))
		} else {
			for _, s := range services {
				results = append(results, r.serviceToResult(s))
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetItemsWithImages returns image URLs for items that match the customer's message
func (r *RetrievalService) GetItemsWithImages(ctx context.Context, tenantID uint, message string) ([]string, error) {
	messageLower := strings.ToLower(message)
	
	// Check if customer wants to see images
	wantsImages := strings.Contains(messageLower, "image") ||
		strings.Contains(messageLower, "picture") ||
		strings.Contains(messageLower, "photo") ||
		strings.Contains(messageLower, "show me") ||
		strings.Contains(messageLower, "see") ||
		strings.Contains(messageLower, "menu") ||
		strings.Contains(messageLower, "what do you have") ||
		strings.Contains(messageLower, "what's available")
	
	if !wantsImages {
		return nil, nil
	}
	
	// Get items based on message
	var results []RetrievalResult
	var err error
	
	if strings.Contains(messageLower, "menu") || strings.Contains(messageLower, "what") || strings.Contains(messageLower, "list") {
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
	
	// Extract image URLs (limit to first 5 images)
	var imageURLs []string
	for _, item := range results {
		if len(item.ImageURLs) > 0 {
			// Add first image from each item
			imageURLs = append(imageURLs, item.ImageURLs[0])
			if len(imageURLs) >= 5 {
				break
			}
		}
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

