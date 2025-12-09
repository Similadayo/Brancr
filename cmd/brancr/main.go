package main

import (
	"log"

	_ "github.com/similadayo/brancr/docs" // Import docs for Swagger
	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/core"
)

// @title           Brancr API
// @version         1.0
// @description     AI-powered social media management platform API
// @termsOfService  https://brancr.com/terms

// @contact.name   API Support
// @contact.url    https://brancr.com/support
// @contact.email  support@brancr.com

// @license.name  Proprietary
// @license.url   https://brancr.com/license

// @host      api.brancr.com
// @BasePath  /api

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

func main() {
	cfg := config.Load()

	app, err := core.NewApp(cfg)
	if err != nil {
		log.Fatalf("error initializing app: %v", err)
	}

	// 🚀 Run entire system (includes admin dashboard, tenant bots, and onboarding polling)
	if err := app.Run(); err != nil {
		log.Fatalf("run error: %v", err)
	}
}
