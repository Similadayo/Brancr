package core

import (
	"testing"

	"github.com/similadayo/brancr/internal/testutil"
)

func TestNewServices(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	logger := testutil.TestLogger(t)
	cfg := testutil.TestConfig()
	redis := testutil.TestRedis(t)

	services, err := NewServices(cfg, db, redis, logger)
	if err != nil {
		t.Fatalf("NewServices() error = %v", err)
	}

	if services == nil {
		t.Fatal("NewServices() returned nil")
	}

	// Verify core services are initialized
	if services.DB == nil {
		t.Error("Services.DB is nil")
	}

	if services.Logger == nil {
		t.Error("Services.Logger is nil")
	}

	if services.Config == nil {
		t.Error("Services.Config is nil")
	}

	// Verify AI services are initialized
	if services.AIClient == nil {
		t.Error("Services.AIClient is nil")
	}

	if services.MemoryService == nil {
		t.Error("Services.MemoryService is nil")
	}

	if services.IntentDetector == nil {
		t.Error("Services.IntentDetector is nil")
	}

	if services.CustomerResponder == nil {
		t.Error("Services.CustomerResponder is nil")
	}

	if services.CaptionOptimizer == nil {
		t.Error("Services.CaptionOptimizer is nil")
	}

	// Verify Bot services
	if services.BotManager == nil {
		t.Error("Services.BotManager is nil")
	}

	// Verify Social services
	if services.MessageHandler == nil {
		t.Error("Services.MessageHandler is nil")
	}

	if services.MetaWebhookHandler == nil {
		t.Error("Services.MetaWebhookHandler is nil")
	}

	if services.Scheduler == nil {
		t.Error("Services.Scheduler is nil")
	}
}

