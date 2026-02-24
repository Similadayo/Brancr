package industry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/storage/repositories"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Service provides industry-related business logic
type Service struct {
	db           *gorm.DB
	industryRepo *repositories.IndustryRepository
	productRepo  *repositories.ProductRepository
	menuItemRepo *repositories.MenuItemRepository
	serviceRepo  *repositories.ServiceRepository
	tenantRepo   *repositories.TenantRepository
	logger       *zap.Logger
	Retrieval    *RetrievalService // Industry-aware product/menu/service retrieval
}

// NewService creates a new industry service
func NewService(db *gorm.DB, logger *zap.Logger) *Service {
	service := &Service{
		db:           db,
		industryRepo: repositories.NewIndustryRepository(db),
		productRepo:  repositories.NewProductRepository(db),
		menuItemRepo: repositories.NewMenuItemRepository(db),
		serviceRepo:  repositories.NewServiceRepository(db),
		tenantRepo:   repositories.NewTenantRepository(db),
		logger:       logger,
	}

	// Initialize retrieval service
	service.Retrieval = NewRetrievalService(
		service,
		service.productRepo,
		service.menuItemRepo,
		service.serviceRepo,
		logger,
	)

	return service
}

// GetIndustryByID retrieves an industry by ID.
func (s *Service) GetIndustryByID(ctx context.Context, id uuid.UUID) (*storage.Industry, error) {
	return s.industryRepo.GetByID(ctx, id)
}

// GetAllIndustries retrieves all available industries
func (s *Service) GetAllIndustries(ctx context.Context) ([]storage.Industry, error) {
	return s.industryRepo.GetAll(ctx)
}

// GetTenantIndustry retrieves the industry for a tenant
func (s *Service) GetTenantIndustry(ctx context.Context, tenantID uuid.UUID) (*storage.Industry, error) {
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	if tenant.IndustryID == uuid.Nil {
		// Default to "General" industry if not set
		return s.industryRepo.GetByName(ctx, "General")
	}

	return s.industryRepo.GetByID(ctx, tenant.IndustryID)
}

// SetTenantIndustry sets the industry for a tenant
func (s *Service) SetTenantIndustry(ctx context.Context, tenantID uuid.UUID, industryID uuid.UUID) error {
	// Verify industry exists
	_, err := s.industryRepo.GetByID(ctx, industryID)
	if err != nil {
		return fmt.Errorf("industry not found: %w", err)
	}

	// Update only the IndustryID field to avoid relationship issues
	// Use Updates() instead of Save() to only update specific fields
	result := s.db.WithContext(ctx).Model(&storage.Tenant{}).
		Where("id = ?", tenantID).
		Update("industry_id", industryID)

	if result.Error != nil {
		return fmt.Errorf("failed to update tenant industry: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("tenant not found")
	}

	return nil
}

// GetIndustrySettings retrieves industry settings for a tenant
func (s *Service) GetIndustrySettings(ctx context.Context, tenantID uuid.UUID) (map[string]interface{}, error) {
	industry, err := s.GetTenantIndustry(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	settings := make(map[string]interface{})
	if industry.Settings != "" {
		if err := json.Unmarshal([]byte(industry.Settings), &settings); err != nil {
			s.logger.Warn("failed to parse industry settings", zap.Error(err))
			return make(map[string]interface{}), nil
		}
	}

	// Add tenant-specific settings if any
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err == nil && tenant.IndustrySettings != "" {
		tenantSettings := make(map[string]interface{})
		if err := json.Unmarshal([]byte(tenant.IndustrySettings), &tenantSettings); err == nil {
			// Merge tenant settings (override industry defaults)
			for k, v := range tenantSettings {
				settings[k] = v
			}
		}
	}

	return settings, nil
}

// HasProducts checks if tenant's industry supports products
func (s *Service) HasProducts(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	industry, err := s.GetTenantIndustry(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return industry.HasProducts, nil
}

// HasMenu checks if tenant's industry supports menu items
func (s *Service) HasMenu(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	industry, err := s.GetTenantIndustry(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return industry.HasMenu, nil
}

// HasServices checks if tenant's industry supports services
func (s *Service) HasServices(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	industry, err := s.GetTenantIndustry(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return industry.HasServices, nil
}

// GetIndustryCapabilities returns all capabilities for a tenant's industry
func (s *Service) GetIndustryCapabilities(ctx context.Context, tenantID uuid.UUID) (*IndustryCapabilities, error) {
	industry, err := s.GetTenantIndustry(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return &IndustryCapabilities{
		HasProducts:  industry.HasProducts,
		HasMenu:      industry.HasMenu,
		HasServices:  industry.HasServices,
		IndustryID:   industry.ID,
		IndustryName: industry.Name,
	}, nil
}

// IndustryCapabilities represents what a tenant's industry supports
type IndustryCapabilities struct {
	HasProducts  bool
	HasMenu      bool
	HasServices  bool
	IndustryID   uuid.UUID
	IndustryName string
}

// FlowType constants for industry-specific AI behavior
const (
	FlowTypeProducts = "products" // Order + Delivery/Pickup flow (restaurants, retail)
	FlowTypeServices = "services" // Knowledge + Booking flow (consultancies, agencies)
	FlowTypeHybrid   = "hybrid"   // Both products and services
)

// GetTenantFlow determines the primary AI flow type for a tenant
// This controls whether AI uses order/delivery or knowledge/booking behavior
func (s *Service) GetTenantFlow(ctx context.Context, tenantID uuid.UUID) (string, error) {
	industry, err := s.GetTenantIndustry(ctx, tenantID)
	if err != nil {
		return FlowTypeProducts, err // Default to products on error
	}

	hasProducts := industry.HasProducts || industry.HasMenu
	hasServices := industry.HasServices

	if hasProducts && hasServices {
		return FlowTypeHybrid, nil
	} else if hasServices && !hasProducts {
		return FlowTypeServices, nil
	}
	return FlowTypeProducts, nil
}

// GetServiceKnowledge retrieves knowledge items for AI context
func (s *Service) GetServiceKnowledge(ctx context.Context, tenantID uuid.UUID, query string) ([]storage.ServiceKnowledge, error) {
	var knowledge []storage.ServiceKnowledge

	// Get all active knowledge for the tenant
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Order("sort_order ASC, created_at DESC").
		Find(&knowledge).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get service knowledge: %w", err)
	}

	return knowledge, nil
}

// GetServiceKnowledgeByCategory retrieves knowledge items by category
func (s *Service) GetServiceKnowledgeByCategory(ctx context.Context, tenantID uuid.UUID, category string) ([]storage.ServiceKnowledge, error) {
	var knowledge []storage.ServiceKnowledge

	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND category = ? AND is_active = ?", tenantID, category, true).
		Order("sort_order ASC").
		Find(&knowledge).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get service knowledge by category: %w", err)
	}

	return knowledge, nil
}

// BuildKnowledgeContext creates a string context for AI from knowledge items
func (s *Service) BuildKnowledgeContext(ctx context.Context, tenantID uuid.UUID) (string, error) {
	knowledge, err := s.GetServiceKnowledge(ctx, tenantID, "")
	if err != nil {
		return "", err
	}

	if len(knowledge) == 0 {
		return "", nil
	}

	var result string
	currentCategory := ""

	for _, k := range knowledge {
		if k.Category != currentCategory {
			result += fmt.Sprintf("\n## %s\n", k.Category)
			currentCategory = k.Category
		}
		result += fmt.Sprintf("### %s\n%s\n\n", k.Title, k.Content)
	}

	return result, nil
}
