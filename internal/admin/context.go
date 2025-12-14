package admin

import (
	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/config"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AdminContext carries only the dependencies the admin layer needs.
type AdminContext struct {
	Config *config.Config
	DB     *gorm.DB
	Redis  *redis.Client
	Logger *zap.Logger

	AdminEmail    string
	AdminPassword string
	Events        *EventBroker
}
