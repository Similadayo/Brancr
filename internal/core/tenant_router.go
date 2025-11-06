package core

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/bot"
)

func TenantRouter(app *App) http.Handler {
	r := chi.NewRouter()

	// ✅ Dynamic path capture: /bot/tenant/{tenantID}/webhook
	r.Post("/tenant/{tenantID}/webhook", func(w http.ResponseWriter, r *http.Request) {
		tenantID := chi.URLParam(r, "tenantID")
		bot.NewTenantWebhookHandler(app.Services.BotManager).ServeHTTPWithID(w, r, tenantID)
	})

	return r
}
