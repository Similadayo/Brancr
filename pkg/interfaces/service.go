package interfaces

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/config"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ServiceRegistry provides access to core services
type ServiceRegistry interface {
	DB() *gorm.DB
	Redis() *redis.Client
	Logger() *zap.Logger
	Config() *config.Config
}

// Service represents a service that can be initialized and started
type Service interface {
	Initialize(ctx context.Context) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
