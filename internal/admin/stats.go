package admin

import (
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
)

type Metrics struct {
	TotalTenants    int
	ActiveBots      int
	PostsToday      int
	OpenRouterUsage int
}

func MetricsPartialHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var tenants, posts int64
		ctx.DB.Model(&storage.Tenant{}).Count(&tenants)
		ctx.DB.Model(&storage.MediaAsset{}).Where("created_at >= ?", time.Now().Add(-24*time.Hour)).Count(&posts)

		m := Metrics{
			TotalTenants:    int(tenants),
			ActiveBots:      0, // TODO: count bots table when implemented
			PostsToday:      int(posts),
			OpenRouterUsage: 0, // placeholder
		}

		tpl, _ := loadTemplate("metrics_partial.html")
		tpl.Execute(w, m)
	}
}
