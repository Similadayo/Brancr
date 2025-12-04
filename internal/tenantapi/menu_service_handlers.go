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
// Menu Item Management (Restaurants)
// =======================

// listMenuItems lists all menu items for the tenant
func (h *Handler) listMenuItems(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	menuRepo := repositories.NewMenuItemRepository(h.DB)

	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	activeOnly := r.URL.Query().Get("active_only") == "true"

	var items []storage.MenuItem
	var err error

	if search != "" {
		items, err = menuRepo.SearchByTenant(ctx, claims.TenantID, search)
	} else if category != "" {
		items, err = menuRepo.GetByCategory(ctx, claims.TenantID, category)
	} else if activeOnly {
		items, err = menuRepo.GetActiveByTenant(ctx, claims.TenantID)
	} else {
		items, err = menuRepo.GetByTenant(ctx, claims.TenantID)
	}

	if err != nil {
		h.Logger.Error("failed to fetch menu items", zap.Error(err))
		http.Error(w, "failed to fetch menu items", http.StatusInternalServerError)
		return
	}

	type menuItemResp struct {
		ID              uint     `json:"id"`
		Name            string   `json:"name"`
		Description     string   `json:"description"`
		Price           float64  `json:"price"`
		Currency        string   `json:"currency"`
		Category        string   `json:"category"`
		PreparationTime int      `json:"preparation_time"`
		SpiceLevel      string   `json:"spice_level"`
		Availability    string   `json:"availability"`
		IsActive        bool     `json:"is_active"`
		DietaryInfo     []string `json:"dietary_info,omitempty"`
		Images          []string `json:"images,omitempty"`
		CreatedAt       string   `json:"created_at"`
		UpdatedAt       string   `json:"updated_at"`
	}

	resp := make([]menuItemResp, len(items))
	for i, item := range items {
		var dietary []string
		var images []string

		if item.DietaryInfo != "" {
			json.Unmarshal([]byte(item.DietaryInfo), &dietary)
		}
		if item.Images != "" {
			json.Unmarshal([]byte(item.Images), &images)
		}

		resp[i] = menuItemResp{
			ID:              item.ID,
			Name:            item.Name,
			Description:     item.Description,
			Price:           item.Price,
			Currency:        item.Currency,
			Category:        item.Category,
			PreparationTime: item.PreparationTime,
			SpiceLevel:      item.SpiceLevel,
			Availability:    item.Availability,
			IsActive:        item.IsActive,
			DietaryInfo:     dietary,
			Images:          images,
			CreatedAt:       item.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:       item.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"menu_items": resp,
		"count":      len(resp),
	})
}

// createMenuItem creates a new menu item
func (h *Handler) createMenuItem(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name            string   `json:"name"`
		Description     string   `json:"description"`
		Price           float64  `json:"price"`
		Currency        string   `json:"currency"`
		Category        string   `json:"category"`
		PreparationTime int      `json:"preparation_time"`
		SpiceLevel      string   `json:"spice_level"`
		Availability    string   `json:"availability"`
		IsActive        bool     `json:"is_active"`
		DietaryInfo     []string `json:"dietary_info"`
		Images          []string `json:"images"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	item := storage.MenuItem{
		TenantID:        claims.TenantID,
		Name:            req.Name,
		Description:     req.Description,
		Price:           req.Price,
		Currency:        req.Currency,
		Category:        req.Category,
		PreparationTime: req.PreparationTime,
		SpiceLevel:      req.SpiceLevel,
		Availability:    req.Availability,
		IsActive:        req.IsActive,
	}

	if req.DietaryInfo != nil {
		dietaryJSON, _ := json.Marshal(req.DietaryInfo)
		item.DietaryInfo = string(dietaryJSON)
	}
	if req.Images != nil {
		imagesJSON, _ := json.Marshal(req.Images)
		item.Images = string(imagesJSON)
	}

	if item.Currency == "" {
		item.Currency = "NGN"
	}
	if item.Availability == "" {
		item.Availability = "available"
	}

	ctx := r.Context()
	menuRepo := repositories.NewMenuItemRepository(h.DB)
	if err := menuRepo.Create(ctx, &item); err != nil {
		h.Logger.Error("failed to create menu item", zap.Error(err))
		http.Error(w, "failed to create menu item", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"menu_item": map[string]any{
			"id":              item.ID,
			"name":            item.Name,
			"description":     item.Description,
			"price":           item.Price,
			"currency":        item.Currency,
			"category":        item.Category,
			"preparation_time": item.PreparationTime,
			"spice_level":     item.SpiceLevel,
			"availability":    item.Availability,
			"is_active":       item.IsActive,
		},
	})
}

// getMenuItem gets a single menu item
func (h *Handler) getMenuItem(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid menu item id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	menuRepo := repositories.NewMenuItemRepository(h.DB)
	item, err := menuRepo.GetByID(ctx, uint(id))
	if err != nil {
		http.Error(w, "menu item not found", http.StatusNotFound)
		return
	}

	if item.TenantID != claims.TenantID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	var dietary []string
	var images []string

	if item.DietaryInfo != "" {
		json.Unmarshal([]byte(item.DietaryInfo), &dietary)
	}
	if item.Images != "" {
		json.Unmarshal([]byte(item.Images), &images)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"menu_item": map[string]any{
			"id":              item.ID,
			"name":            item.Name,
			"description":     item.Description,
			"price":           item.Price,
			"currency":        item.Currency,
			"category":        item.Category,
			"preparation_time": item.PreparationTime,
			"spice_level":     item.SpiceLevel,
			"availability":    item.Availability,
			"is_active":       item.IsActive,
			"dietary_info":    dietary,
			"images":          images,
			"created_at":      item.CreatedAt.Format("2006-01-02T15:04:05Z"),
			"updated_at":      item.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		},
	})
}

// updateMenuItem updates a menu item
func (h *Handler) updateMenuItem(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid menu item id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	menuRepo := repositories.NewMenuItemRepository(h.DB)
	item, err := menuRepo.GetByID(ctx, uint(id))
	if err != nil {
		http.Error(w, "menu item not found", http.StatusNotFound)
		return
	}

	if item.TenantID != claims.TenantID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		Name            *string  `json:"name"`
		Description     *string  `json:"description"`
		Price           *float64 `json:"price"`
		Currency        *string  `json:"currency"`
		Category        *string  `json:"category"`
		PreparationTime *int     `json:"preparation_time"`
		SpiceLevel      *string  `json:"spice_level"`
		Availability    *string  `json:"availability"`
		IsActive        *bool    `json:"is_active"`
		DietaryInfo     []string `json:"dietary_info"`
		Images          []string `json:"images"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		item.Name = *req.Name
	}
	if req.Description != nil {
		item.Description = *req.Description
	}
	if req.Price != nil {
		item.Price = *req.Price
	}
	if req.Currency != nil {
		item.Currency = *req.Currency
	}
	if req.Category != nil {
		item.Category = *req.Category
	}
	if req.PreparationTime != nil {
		item.PreparationTime = *req.PreparationTime
	}
	if req.SpiceLevel != nil {
		item.SpiceLevel = *req.SpiceLevel
	}
	if req.Availability != nil {
		item.Availability = *req.Availability
	}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if req.DietaryInfo != nil {
		dietaryJSON, _ := json.Marshal(req.DietaryInfo)
		item.DietaryInfo = string(dietaryJSON)
	}
	if req.Images != nil {
		imagesJSON, _ := json.Marshal(req.Images)
		item.Images = string(imagesJSON)
	}

	if err := menuRepo.Update(ctx, item); err != nil {
		h.Logger.Error("failed to update menu item", zap.Error(err))
		http.Error(w, "failed to update menu item", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"menu_item": map[string]any{
			"id":              item.ID,
			"name":            item.Name,
			"description":     item.Description,
			"price":           item.Price,
			"currency":        item.Currency,
			"category":        item.Category,
			"preparation_time": item.PreparationTime,
			"spice_level":     item.SpiceLevel,
			"availability":    item.Availability,
			"is_active":       item.IsActive,
		},
	})
}

// deleteMenuItem deletes a menu item
func (h *Handler) deleteMenuItem(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid menu item id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	menuRepo := repositories.NewMenuItemRepository(h.DB)
	item, err := menuRepo.GetByID(ctx, uint(id))
	if err != nil {
		http.Error(w, "menu item not found", http.StatusNotFound)
		return
	}

	if item.TenantID != claims.TenantID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	if err := menuRepo.Delete(ctx, uint(id)); err != nil {
		h.Logger.Error("failed to delete menu item", zap.Error(err))
		http.Error(w, "failed to delete menu item", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "menu item deleted",
	})
}

// =======================
// Service Management (Consultants/Agencies)
// =======================

// listServices lists all services for the tenant
func (h *Handler) listServices(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	serviceRepo := repositories.NewServiceRepository(h.DB)

	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	activeOnly := r.URL.Query().Get("active_only") == "true"

	var services []storage.Service
	var err error

	if search != "" {
		services, err = serviceRepo.SearchByTenant(ctx, claims.TenantID, search)
	} else if category != "" {
		services, err = serviceRepo.GetByCategory(ctx, claims.TenantID, category)
	} else if activeOnly {
		services, err = serviceRepo.GetActiveByTenant(ctx, claims.TenantID)
	} else {
		services, err = serviceRepo.GetByTenant(ctx, claims.TenantID)
	}

	if err != nil {
		h.Logger.Error("failed to fetch services", zap.Error(err))
		http.Error(w, "failed to fetch services", http.StatusInternalServerError)
		return
	}

	type serviceResp struct {
		ID          uint                   `json:"id"`
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Pricing     map[string]interface{} `json:"pricing,omitempty"`
		Packages    []interface{}          `json:"packages,omitempty"`
		Duration    string                 `json:"duration"`
		Deliverables string                `json:"deliverables"`
		Category    string                 `json:"category"`
		IsActive    bool                   `json:"is_active"`
		CreatedAt   string                 `json:"created_at"`
		UpdatedAt   string                 `json:"updated_at"`
	}

	resp := make([]serviceResp, len(services))
	for i, s := range services {
		var pricing map[string]interface{}
		var packages []interface{}

		if s.Pricing != "" {
			json.Unmarshal([]byte(s.Pricing), &pricing)
		}
		if s.Packages != "" {
			json.Unmarshal([]byte(s.Packages), &packages)
		}

		resp[i] = serviceResp{
			ID:           s.ID,
			Name:         s.Name,
			Description:  s.Description,
			Pricing:      pricing,
			Packages:     packages,
			Duration:     s.Duration,
			Deliverables: s.Deliverables,
			Category:     s.Category,
			IsActive:     s.IsActive,
			CreatedAt:    s.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:    s.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"services": resp,
		"count":    len(resp),
	})
}

// createService creates a new service
func (h *Handler) createService(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name         string                 `json:"name"`
		Description  string                 `json:"description"`
		Pricing      map[string]interface{} `json:"pricing"`
		Packages     []interface{}          `json:"packages"`
		Duration     string                 `json:"duration"`
		Deliverables string                 `json:"deliverables"`
		Category     string                 `json:"category"`
		IsActive     bool                   `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	service := storage.Service{
		TenantID:    claims.TenantID,
		Name:        req.Name,
		Description: req.Description,
		Duration:    req.Duration,
		Deliverables: req.Deliverables,
		Category:    req.Category,
		IsActive:    req.IsActive,
	}

	if req.Pricing != nil {
		pricingJSON, _ := json.Marshal(req.Pricing)
		service.Pricing = string(pricingJSON)
	}
	if req.Packages != nil {
		packagesJSON, _ := json.Marshal(req.Packages)
		service.Packages = string(packagesJSON)
	}

	ctx := r.Context()
	serviceRepo := repositories.NewServiceRepository(h.DB)
	if err := serviceRepo.Create(ctx, &service); err != nil {
		h.Logger.Error("failed to create service", zap.Error(err))
		http.Error(w, "failed to create service", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"service": map[string]any{
			"id":          service.ID,
			"name":        service.Name,
			"description": service.Description,
			"duration":    service.Duration,
			"category":    service.Category,
			"is_active":   service.IsActive,
		},
	})
}

// getService gets a single service
func (h *Handler) getService(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid service id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	serviceRepo := repositories.NewServiceRepository(h.DB)
	service, err := serviceRepo.GetByID(ctx, uint(id))
	if err != nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}

	if service.TenantID != claims.TenantID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	var pricing map[string]interface{}
	var packages []interface{}

	if service.Pricing != "" {
		json.Unmarshal([]byte(service.Pricing), &pricing)
	}
	if service.Packages != "" {
		json.Unmarshal([]byte(service.Packages), &packages)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"service": map[string]any{
			"id":           service.ID,
			"name":         service.Name,
			"description":  service.Description,
			"pricing":      pricing,
			"packages":     packages,
			"duration":     service.Duration,
			"deliverables": service.Deliverables,
			"category":     service.Category,
			"is_active":    service.IsActive,
			"created_at":   service.CreatedAt.Format("2006-01-02T15:04:05Z"),
			"updated_at":   service.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		},
	})
}

// updateService updates a service
func (h *Handler) updateService(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid service id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	serviceRepo := repositories.NewServiceRepository(h.DB)
	service, err := serviceRepo.GetByID(ctx, uint(id))
	if err != nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}

	if service.TenantID != claims.TenantID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		Name         *string                `json:"name"`
		Description  *string                `json:"description"`
		Pricing      map[string]interface{} `json:"pricing"`
		Packages     []interface{}          `json:"packages"`
		Duration     *string                `json:"duration"`
		Deliverables *string                `json:"deliverables"`
		Category     *string                `json:"category"`
		IsActive     *bool                  `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		service.Name = *req.Name
	}
	if req.Description != nil {
		service.Description = *req.Description
	}
	if req.Duration != nil {
		service.Duration = *req.Duration
	}
	if req.Deliverables != nil {
		service.Deliverables = *req.Deliverables
	}
	if req.Category != nil {
		service.Category = *req.Category
	}
	if req.IsActive != nil {
		service.IsActive = *req.IsActive
	}
	if req.Pricing != nil {
		pricingJSON, _ := json.Marshal(req.Pricing)
		service.Pricing = string(pricingJSON)
	}
	if req.Packages != nil {
		packagesJSON, _ := json.Marshal(req.Packages)
		service.Packages = string(packagesJSON)
	}

	if err := serviceRepo.Update(ctx, service); err != nil {
		h.Logger.Error("failed to update service", zap.Error(err))
		http.Error(w, "failed to update service", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"service": map[string]any{
			"id":          service.ID,
			"name":        service.Name,
			"description": service.Description,
			"duration":    service.Duration,
			"category":    service.Category,
			"is_active":   service.IsActive,
		},
	})
}

// deleteService deletes a service
func (h *Handler) deleteService(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid service id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	serviceRepo := repositories.NewServiceRepository(h.DB)
	service, err := serviceRepo.GetByID(ctx, uint(id))
	if err != nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}

	if service.TenantID != claims.TenantID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	if err := serviceRepo.Delete(ctx, uint(id)); err != nil {
		h.Logger.Error("failed to delete service", zap.Error(err))
		http.Error(w, "failed to delete service", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "service deleted",
	})
}

