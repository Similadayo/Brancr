package tenantapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/storage/repositories"
	"go.uber.org/zap"
)

// =======================
// Industry Management
// =======================

// listIndustries returns all available industries
func (h *Handler) listIndustries(w http.ResponseWriter, r *http.Request) {
	if h.IndustryService == nil {
		http.Error(w, "industry service not available", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	industries, err := h.IndustryService.GetAllIndustries(ctx)
	if err != nil {
		h.Logger.Error("failed to fetch industries", zap.Error(err))
		http.Error(w, "failed to fetch industries", http.StatusInternalServerError)
		return
	}

	type industryResp struct {
		ID          uint   `json:"id"`
		Name        string `json:"name"`
		Category    string `json:"category"`
		Description string `json:"description"`
		HasProducts bool   `json:"has_products"`
		HasMenu     bool   `json:"has_menu"`
		HasServices bool   `json:"has_services"`
	}

	resp := make([]industryResp, len(industries))
	for i, ind := range industries {
		resp[i] = industryResp{
			ID:          ind.ID,
			Name:        ind.Name,
			Category:    ind.Category,
			Description: ind.Description,
			HasProducts: ind.HasProducts,
			HasMenu:     ind.HasMenu,
			HasServices: ind.HasServices,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"industries": resp,
	})
}

// getTenantIndustry returns the tenant's current industry
func (h *Handler) getTenantIndustry(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.IndustryService == nil {
		http.Error(w, "industry service not available", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	industry, err := h.IndustryService.GetTenantIndustry(ctx, claims.TenantID)
	if err != nil {
		h.Logger.Error("failed to get tenant industry", zap.Error(err))
		http.Error(w, "failed to get industry", http.StatusInternalServerError)
		return
	}

	capabilities, err := h.IndustryService.GetIndustryCapabilities(ctx, claims.TenantID)
	if err != nil {
		h.Logger.Warn("failed to get capabilities", zap.Error(err))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"industry_id":   industry.ID,
		"industry_name": industry.Name,
		"category":      industry.Category,
		"description":   industry.Description,
		"capabilities": map[string]bool{
			"has_products": capabilities.HasProducts,
			"has_menu":     capabilities.HasMenu,
			"has_services": capabilities.HasServices,
		},
	})
}

// setTenantIndustry sets the tenant's industry
func (h *Handler) setTenantIndustry(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.IndustryService == nil {
		http.Error(w, "industry service not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		IndustryID uint `json:"industry_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.IndustryID == 0 {
		http.Error(w, "industry_id is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if err := h.IndustryService.SetTenantIndustry(ctx, claims.TenantID, req.IndustryID); err != nil {
		h.Logger.Error("failed to set tenant industry", zap.Error(err))
		http.Error(w, "failed to set industry", http.StatusInternalServerError)
		return
	}

	industry, err := h.IndustryService.GetIndustryByID(ctx, req.IndustryID)
	if err != nil {
		h.Logger.Error("failed to get industry", zap.Error(err))
		http.Error(w, "failed to get industry", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"industry": map[string]any{
			"id":          industry.ID,
			"name":        industry.Name,
			"category":    industry.Category,
			"description": industry.Description,
		},
	})
}

// setOnboardingIndustry sets industry during onboarding
func (h *Handler) setOnboardingIndustry(w http.ResponseWriter, r *http.Request) {
	// Same as setTenantIndustry but can be extended for onboarding-specific logic
	h.setTenantIndustry(w, r)
}

// =======================
// Product Management (E-commerce)
// =======================

// listProducts lists all products for the tenant
func (h *Handler) listProducts(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	productRepo := repositories.NewProductRepository(h.DB)

	// Get query parameters
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	activeOnly := r.URL.Query().Get("active_only") == "true"

	var products []storage.Product
	var err error

	if search != "" {
		products, err = productRepo.SearchByTenant(ctx, claims.TenantID, search)
	} else if category != "" {
		products, err = productRepo.GetByCategory(ctx, claims.TenantID, category)
	} else if activeOnly {
		products, err = productRepo.GetActiveByTenant(ctx, claims.TenantID)
	} else {
		products, err = productRepo.GetByTenant(ctx, claims.TenantID)
	}

	if err != nil {
		h.Logger.Error("failed to fetch products", zap.Error(err))
		http.Error(w, "failed to fetch products", http.StatusInternalServerError)
		return
	}

	type productResp struct {
		ID           uint                   `json:"id"`
		Name         string                 `json:"name"`
		Description  string                 `json:"description"`
		Price        float64                `json:"price"`
		Currency     string                 `json:"currency"`
		Category     string                 `json:"category"`
		StockCount   int                    `json:"stock_count"`
		Availability string                 `json:"availability"`
		IsActive     bool                   `json:"is_active"`
		SKU          string                 `json:"sku"`
		Variants     map[string]interface{} `json:"variants,omitempty"`
		Tags         []string               `json:"tags,omitempty"`
		Images       []string               `json:"images,omitempty"`
		CreatedAt    string                 `json:"created_at"`
		UpdatedAt    string                 `json:"updated_at"`
	}

	resp := make([]productResp, len(products))
	for i, p := range products {
		var variants map[string]interface{}
		var tags []string
		var images []string

		if p.Variants != "" {
			json.Unmarshal([]byte(p.Variants), &variants)
		}
		if p.Tags != "" {
			json.Unmarshal([]byte(p.Tags), &tags)
		}
		if p.Images != "" {
			json.Unmarshal([]byte(p.Images), &images)
		}

		resp[i] = productResp{
			ID:           p.ID,
			Name:         p.Name,
			Description:  p.Description,
			Price:        p.Price,
			Currency:     p.Currency,
			Category:     p.Category,
			StockCount:   p.StockCount,
			Availability: p.Availability,
			IsActive:     p.IsActive,
			SKU:          p.SKU,
			Variants:     variants,
			Tags:         tags,
			Images:       images,
			CreatedAt:    p.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:    p.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"products": resp,
		"count":    len(resp),
	})
}

// createProduct creates a new product
func (h *Handler) createProduct(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name         string                 `json:"name"`
		Description  string                 `json:"description"`
		Price        float64                `json:"price"`
		Currency     string                 `json:"currency"`
		Category     string                 `json:"category"`
		StockCount   int                    `json:"stock_count"`
		Availability string                 `json:"availability"`
		IsActive     bool                   `json:"is_active"`
		SKU          string                 `json:"sku"`
		Variants     map[string]interface{} `json:"variants"`
		Tags         []string               `json:"tags"`
		Images       []string               `json:"images"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	product := storage.Product{
		TenantID:     claims.TenantID,
		Name:         req.Name,
		Description:  req.Description,
		Price:        req.Price,
		Currency:     req.Currency,
		Category:     req.Category,
		StockCount:   req.StockCount,
		Availability: req.Availability,
		IsActive:     req.IsActive,
		SKU:          req.SKU,
	}

	if req.Variants != nil {
		variantsJSON, _ := json.Marshal(req.Variants)
		product.Variants = string(variantsJSON)
	}
	if req.Tags != nil {
		tagsJSON, _ := json.Marshal(req.Tags)
		product.Tags = string(tagsJSON)
	}
	if req.Images != nil {
		imagesJSON, _ := json.Marshal(req.Images)
		product.Images = string(imagesJSON)
	}

	if product.Currency == "" {
		product.Currency = "NGN"
	}
	if product.Availability == "" {
		product.Availability = "in_stock"
	}

	ctx := r.Context()
	productRepo := repositories.NewProductRepository(h.DB)
	if err := productRepo.Create(ctx, &product); err != nil {
		h.Logger.Error("failed to create product", zap.Error(err))
		http.Error(w, "failed to create product", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"product": map[string]any{
			"id":           product.ID,
			"name":         product.Name,
			"description":  product.Description,
			"price":        product.Price,
			"currency":     product.Currency,
			"category":     product.Category,
			"stock_count":  product.StockCount,
			"availability": product.Availability,
			"is_active":    product.IsActive,
		},
	})
}

// getProduct gets a single product
func (h *Handler) getProduct(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	productRepo := repositories.NewProductRepository(h.DB)
	product, err := productRepo.GetByID(ctx, uint(id))
	if err != nil {
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}

	if product.TenantID != claims.TenantID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	var variants map[string]interface{}
	var tags []string
	var images []string

	if product.Variants != "" {
		json.Unmarshal([]byte(product.Variants), &variants)
	}
	if product.Tags != "" {
		json.Unmarshal([]byte(product.Tags), &tags)
	}
	if product.Images != "" {
		json.Unmarshal([]byte(product.Images), &images)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"product": map[string]any{
			"id":           product.ID,
			"name":         product.Name,
			"description":  product.Description,
			"price":        product.Price,
			"currency":     product.Currency,
			"category":     product.Category,
			"stock_count":  product.StockCount,
			"availability": product.Availability,
			"is_active":    product.IsActive,
			"sku":          product.SKU,
			"variants":     variants,
			"tags":         tags,
			"images":       images,
			"created_at":   product.CreatedAt.Format("2006-01-02T15:04:05Z"),
			"updated_at":   product.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		},
	})
}

// updateProduct updates a product
func (h *Handler) updateProduct(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	productRepo := repositories.NewProductRepository(h.DB)
	product, err := productRepo.GetByID(ctx, uint(id))
	if err != nil {
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}

	if product.TenantID != claims.TenantID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		Name         *string                `json:"name"`
		Description  *string                `json:"description"`
		Price        *float64               `json:"price"`
		Currency     *string                `json:"currency"`
		Category     *string                `json:"category"`
		StockCount   *int                   `json:"stock_count"`
		Availability *string                `json:"availability"`
		IsActive     *bool                  `json:"is_active"`
		SKU          *string                `json:"sku"`
		Variants     map[string]interface{} `json:"variants"`
		Tags         []string               `json:"tags"`
		Images       []string               `json:"images"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		product.Name = *req.Name
	}
	if req.Description != nil {
		product.Description = *req.Description
	}
	if req.Price != nil {
		product.Price = *req.Price
	}
	if req.Currency != nil {
		product.Currency = *req.Currency
	}
	if req.Category != nil {
		product.Category = *req.Category
	}
	if req.StockCount != nil {
		product.StockCount = *req.StockCount
	}
	if req.Availability != nil {
		product.Availability = *req.Availability
	}
	if req.IsActive != nil {
		product.IsActive = *req.IsActive
	}
	if req.SKU != nil {
		product.SKU = *req.SKU
	}
	if req.Variants != nil {
		variantsJSON, _ := json.Marshal(req.Variants)
		product.Variants = string(variantsJSON)
	}
	if req.Tags != nil {
		tagsJSON, _ := json.Marshal(req.Tags)
		product.Tags = string(tagsJSON)
	}
	if req.Images != nil {
		imagesJSON, _ := json.Marshal(req.Images)
		product.Images = string(imagesJSON)
	}

	if err := productRepo.Update(ctx, product); err != nil {
		h.Logger.Error("failed to update product", zap.Error(err))
		http.Error(w, "failed to update product", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"product": map[string]any{
			"id":           product.ID,
			"name":         product.Name,
			"description":  product.Description,
			"price":        product.Price,
			"currency":     product.Currency,
			"category":     product.Category,
			"stock_count":  product.StockCount,
			"availability": product.Availability,
			"is_active":    product.IsActive,
		},
	})
}

// deleteProduct deletes a product
func (h *Handler) deleteProduct(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	productRepo := repositories.NewProductRepository(h.DB)
	product, err := productRepo.GetByID(ctx, uint(id))
	if err != nil {
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}

	if product.TenantID != claims.TenantID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	if err := productRepo.Delete(ctx, uint(id)); err != nil {
		h.Logger.Error("failed to delete product", zap.Error(err))
		http.Error(w, "failed to delete product", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "product deleted",
	})
}

