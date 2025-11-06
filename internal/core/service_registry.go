package core

import (
	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/config"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ServiceRegistry provides access to core services
// This is a legacy structure - prefer using Services directly
type ServiceRegistry struct {
	DB     *gorm.DB
	Redis  *redis.Client
	Logger *zap.Logger
	Config *config.Config
}

// NewRegistry creates a ServiceRegistry from an App instance
// DEPRECATED: Use app.Services directly instead
func NewRegistry(app *App) *ServiceRegistry {
	return &ServiceRegistry{
		DB:     app.Services.DB,
		Redis:  app.Services.Redis,
		Logger: app.Services.Logger,
		Config: app.Config,
	}
}
