package testutil

import (
	"context"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/pkg/interfaces"
	"gorm.io/gorm"
)

// MockAIClient is a mock implementation of AIClient
type MockAIClient struct {
	GenerateFunc func(ctx context.Context, messages []interfaces.Message) (string, error)
}

func (m *MockAIClient) Generate(ctx context.Context, messages []interfaces.Message) (string, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, messages)
	}
	return "mock response", nil
}

// MockMemoryService is a mock implementation of MemoryService
type MockMemoryService struct {
	StoreFunc  func(ctx context.Context, tenantID, customerID uint, text string, category string) error
	RecallFunc func(ctx context.Context, tenantID, customerID uint, query string) ([]string, error)
}

func (m *MockMemoryService) Store(ctx context.Context, tenantID, customerID uint, text string, category string) error {
	if m.StoreFunc != nil {
		return m.StoreFunc(ctx, tenantID, customerID, text, category)
	}
	return nil
}

func (m *MockMemoryService) Recall(ctx context.Context, tenantID, customerID uint, query string) ([]string, error) {
	if m.RecallFunc != nil {
		return m.RecallFunc(ctx, tenantID, customerID, query)
	}
	return []string{}, nil
}

// MockBotManager is a mock implementation of BotManager
type MockBotManager struct {
	GetBotFunc                func(tenantID uint) (interface{}, bool)
	GetOwnerIDFunc            func(tenantID uint) int64
	StartBotForTenantFunc     func(tenant *storage.Tenant) error
	StopBotFunc               func(tenantID uint)
	InitBotsFunc              func() error
	GenerateAIResponseFunc    func(ctx context.Context, tenantID uint, userMsg string) (string, error)
	TrackOrCreateCustomerFunc func(ctx context.Context, tenantID uint, channel, platformID, displayName, username, profilePic string) (*storage.Customer, error)
}

func (m *MockBotManager) GetBot(tenantID uint) (interface{}, bool) {
	if m.GetBotFunc != nil {
		return m.GetBotFunc(tenantID)
	}
	return nil, false
}

func (m *MockBotManager) GetOwnerID(tenantID uint) int64 {
	if m.GetOwnerIDFunc != nil {
		return m.GetOwnerIDFunc(tenantID)
	}
	return 0
}

func (m *MockBotManager) StartBotForTenant(tenant *storage.Tenant) error {
	if m.StartBotForTenantFunc != nil {
		return m.StartBotForTenantFunc(tenant)
	}
	return nil
}

func (m *MockBotManager) StopBot(tenantID uint) {
	if m.StopBotFunc != nil {
		m.StopBotFunc(tenantID)
	}
}

func (m *MockBotManager) InitBots() error {
	if m.InitBotsFunc != nil {
		return m.InitBotsFunc()
	}
	return nil
}

func (m *MockBotManager) GenerateAIResponse(ctx context.Context, tenantID uint, userMsg string) (string, error) {
	if m.GenerateAIResponseFunc != nil {
		return m.GenerateAIResponseFunc(ctx, tenantID, userMsg)
	}
	return "mock response", nil
}

func (m *MockBotManager) TrackOrCreateCustomer(ctx context.Context, tenantID uint, channel, platformID, displayName, username, profilePic string) (*storage.Customer, error) {
	if m.TrackOrCreateCustomerFunc != nil {
		return m.TrackOrCreateCustomerFunc(ctx, tenantID, channel, platformID, displayName, username, profilePic)
	}
	return &storage.Customer{}, nil
}

// MockRepository is a mock implementation of Repository
type MockRepository struct {
	DBFunc func() *gorm.DB
}

func (m *MockRepository) DB() *gorm.DB {
	if m.DBFunc != nil {
		return m.DBFunc()
	}
	return nil
}
