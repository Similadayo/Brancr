package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// FailedBot tracks bots that failed to start
type FailedBot struct {
	TenantID    uint
	TenantName  string
	LastError   string
	FailureTime time.Time
	RetryCount  int
	LastRetry   time.Time
}

var (
	failedBots     = make(map[uint]*FailedBot) // tenantID -> FailedBot
	failedBotsLock sync.RWMutex
)

// isRetryableError determines if an error is worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	
	errStr := err.Error()
	
	// Permanent errors (don't retry)
	permanentErrors := []string{
		"invalid bot token",
		"invalid ciphertext",
		"unauthorized",
		"bot token revoked",
		"decrypt",
	}
	
	for _, permanent := range permanentErrors {
		if strings.Contains(strings.ToLower(errStr), permanent) {
			return false // Permanent error, don't retry
		}
	}
	
	// Retryable errors (network issues, rate limits, temporary failures)
	retryableErrors := []string{
		"timeout",
		"connection",
		"network",
		"temporary",
		"rate limit",
		"too many requests",
		"service unavailable",
		"502",
		"503",
		"504",
	}
	
	for _, retryable := range retryableErrors {
		if strings.Contains(strings.ToLower(errStr), retryable) {
			return true
		}
	}
	
	// Default: retry unknown errors (might be temporary)
	return true
}

// retryBotStartup attempts to start a bot with exponential backoff
func (m *TenantBotManager) retryBotStartup(ctx context.Context, tenant *storage.Tenant, maxRetries int) error {
	var lastErr error
	
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Calculate exponential backoff: 2^attempt seconds (max 5 minutes)
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		if backoff > 5*time.Minute {
			backoff = 5 * time.Minute
		}
		
		if attempt > 0 {
			if m.Logger != nil {
				m.Logger.Info("🔄 Retrying bot startup",
					zap.Uint("tenant_id", tenant.ID),
					zap.String("tenant_name", tenant.Name),
					zap.Int("attempt", attempt+1),
					zap.Int("max_retries", maxRetries),
					zap.Duration("backoff", backoff))
			}
			
			// Wait before retry (with context cancellation support)
			select {
			case <-ctx.Done():
				return fmt.Errorf("retry cancelled: %w", ctx.Err())
			case <-time.After(backoff):
				// Continue with retry
			}
		}
		
		// Attempt to start the bot
		err := m.startTenantBot(tenant)
		if err == nil {
			// Success! Remove from failed bots
			failedBotsLock.Lock()
			delete(failedBots, tenant.ID)
			failedBotsLock.Unlock()
			
			if m.Logger != nil {
				m.Logger.Info("✅ Bot startup succeeded after retry",
					zap.Uint("tenant_id", tenant.ID),
					zap.String("tenant_name", tenant.Name),
					zap.Int("attempts", attempt+1))
			}
			
			return nil
		}
		
		lastErr = err
		
		// Check if error is retryable
		if !isRetryableError(err) {
			if m.Logger != nil {
				m.Logger.Warn("❌ Permanent error, not retrying",
					zap.Uint("tenant_id", tenant.ID),
					zap.String("tenant_name", tenant.Name),
					zap.Error(err))
			}
			return err
		}
		
		// Update failed bot tracking
		failedBotsLock.Lock()
		failedBots[tenant.ID] = &FailedBot{
			TenantID:    tenant.ID,
			TenantName:  tenant.Name,
			LastError:   err.Error(),
			FailureTime: time.Now(),
			RetryCount:  attempt + 1,
			LastRetry:   time.Now(),
		}
		failedBotsLock.Unlock()
	}
	
	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// StartRetryWorker starts a background worker that periodically retries failed bots
func (m *TenantBotManager) StartRetryWorker(ctx context.Context) {
	if m.Logger != nil {
		m.Logger.Info("🔄 Starting bot retry worker")
	}
	
	ticker := time.NewTicker(15 * time.Minute) // Check every 15 minutes
	defer ticker.Stop()
	
	// Initial check after 5 minutes
	time.Sleep(5 * time.Minute)
	m.retryFailedBots(ctx)
	
	for {
		select {
		case <-ctx.Done():
			if m.Logger != nil {
				m.Logger.Info("🛑 Bot retry worker stopped")
			}
			return
		case <-ticker.C:
			m.retryFailedBots(ctx)
		}
	}
}

// retryFailedBots attempts to retry all failed bots
func (m *TenantBotManager) retryFailedBots(ctx context.Context) {
	failedBotsLock.RLock()
	failedList := make([]*FailedBot, 0, len(failedBots))
	for _, fb := range failedBots {
		// Only retry if last retry was more than 15 minutes ago
		if time.Since(fb.LastRetry) >= 15*time.Minute {
			failedList = append(failedList, fb)
		}
	}
	failedBotsLock.RUnlock()
	
	if len(failedList) == 0 {
		return
	}
	
	if m.Logger != nil {
		m.Logger.Info("🔄 Retrying failed bots",
			zap.Int("count", len(failedList)))
	}
	
	for _, fb := range failedList {
		// Load tenant from database
		var tenant storage.Tenant
		if err := m.DB.First(&tenant, fb.TenantID).Error; err != nil {
			// Tenant deleted or not found, remove from failed list
			failedBotsLock.Lock()
			delete(failedBots, fb.TenantID)
			failedBotsLock.Unlock()
			continue
		}
		
		// Skip if tenant is not active
		if strings.ToLower(tenant.Status) != "active" {
			failedBotsLock.Lock()
			delete(failedBots, fb.TenantID)
			failedBotsLock.Unlock()
			continue
		}
		
		// Check if bot already started (maybe started by another process)
		if bot, ok := m.GetBot(fb.TenantID); ok && bot != nil {
			// Bot is already running, remove from failed list
			failedBotsLock.Lock()
			delete(failedBots, fb.TenantID)
			failedBotsLock.Unlock()
			continue
		}
		
		// Retry with 3 attempts
		err := m.retryBotStartup(ctx, &tenant, 3)
		if err != nil {
			// Update retry count and last retry time
			failedBotsLock.Lock()
			if existing, ok := failedBots[fb.TenantID]; ok {
				existing.LastError = err.Error()
				existing.LastRetry = time.Now()
				existing.RetryCount++
			}
			failedBotsLock.Unlock()
			
			if m.Logger != nil {
				m.Logger.Warn("❌ Bot retry failed",
					zap.Uint("tenant_id", fb.TenantID),
					zap.String("tenant_name", fb.TenantName),
					zap.Error(err))
			}
		}
		
		// Small delay between retries to avoid hammering the API
		time.Sleep(2 * time.Second)
	}
}

// GetFailedBots returns a list of currently failed bots (for monitoring/debugging)
func (m *TenantBotManager) GetFailedBots() []FailedBot {
	failedBotsLock.RLock()
	defer failedBotsLock.RUnlock()
	
	result := make([]FailedBot, 0, len(failedBots))
	for _, fb := range failedBots {
		result = append(result, *fb)
	}
	
	return result
}

