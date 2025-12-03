package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Cache provides a Redis-based caching layer
type Cache struct {
	rdb    *redis.Client
	logger *zap.Logger
	ttl    time.Duration // Default TTL
}

// NewCache creates a new cache instance
func NewCache(rdb *redis.Client, logger *zap.Logger) *Cache {
	return &Cache{
		rdb:    rdb,
		logger: logger,
		ttl:    15 * time.Minute, // Default 15 minutes
	}
}

// Set stores a value in cache with default TTL
func (c *Cache) Set(ctx context.Context, key string, value interface{}) error {
	return c.SetWithTTL(ctx, key, value, c.ttl)
}

// SetWithTTL stores a value in cache with custom TTL
func (c *Cache) SetWithTTL(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if c.rdb == nil {
		return nil // Cache disabled, silently skip
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal cache value: %w", err)
	}

	if err := c.rdb.Set(ctx, key, data, ttl).Err(); err != nil {
		if c.logger != nil {
			c.logger.Warn("cache set failed", zap.String("key", key), zap.Error(err))
		}
		return err
	}

	return nil
}

// Get retrieves a value from cache
func (c *Cache) Get(ctx context.Context, key string, dest interface{}) error {
	if c.rdb == nil {
		return ErrCacheMiss
	}

	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return ErrCacheMiss
	}
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("cache get failed", zap.String("key", key), zap.Error(err))
		}
		return err
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("unmarshal cache value: %w", err)
	}

	return nil
}

// Delete removes a value from cache
func (c *Cache) Delete(ctx context.Context, key string) error {
	if c.rdb == nil {
		return nil
	}
	return c.rdb.Del(ctx, key).Err()
}

// DeletePattern removes all keys matching a pattern
func (c *Cache) DeletePattern(ctx context.Context, pattern string) error {
	if c.rdb == nil {
		return nil
	}

	iter := c.rdb.Scan(ctx, 0, pattern, 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}

	if len(keys) > 0 {
		return c.rdb.Del(ctx, keys...).Err()
	}
	return nil
}

// Exists checks if a key exists in cache
func (c *Cache) Exists(ctx context.Context, key string) (bool, error) {
	if c.rdb == nil {
		return false, nil
	}
	count, err := c.rdb.Exists(ctx, key).Result()
	return count > 0, err
}

// Increment increments a counter in cache
func (c *Cache) Increment(ctx context.Context, key string) (int64, error) {
	if c.rdb == nil {
		return 0, nil
	}
	return c.rdb.Incr(ctx, key).Result()
}

// SetDefaultTTL sets the default TTL for cache operations
func (c *Cache) SetDefaultTTL(ttl time.Duration) {
	c.ttl = ttl
}

// Cache key generators
func TenantKey(tenantID uint) string {
	return fmt.Sprintf("tenant:%d", tenantID)
}

func TenantSettingsKey(tenantID uint) string {
	return fmt.Sprintf("tenant:%d:settings", tenantID)
}

func SocialAccountKey(tenantID uint, platform string) string {
	return fmt.Sprintf("tenant:%d:account:%s", tenantID, platform)
}

func TokenKey(accountID uint) string {
	return fmt.Sprintf("token:%d", accountID)
}

func AICacheKey(tenantID uint, query string) string {
	return fmt.Sprintf("ai:%d:%s", tenantID, query)
}

func WebhookIdempotencyKey(platform, eventID string) string {
	return fmt.Sprintf("webhook:idempotency:%s:%s", platform, eventID)
}

var ErrCacheMiss = fmt.Errorf("cache miss")

