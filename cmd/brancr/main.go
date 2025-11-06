package main

import (
	"log"

	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/core"
)

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
