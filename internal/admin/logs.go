package admin

import (
	"net/http"
	"strconv"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// /admin/logs
// /admin/logs
func LogsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var logs []storage.LogEntry
		if err := ctx.DB.Order("created_at desc").Limit(200).Find(&logs).Error; err != nil {
			ctx.Logger.Error("failed to query logs", zap.Error(err))
			http.Error(w, "database error", 500)
			return
		}

		data := map[string]any{
			"Page": "logs",
			"Logs": logs,
		}

		renderTemplate(w, "logs.html", data, ctx.Logger)
	}
}

// /admin/logs/_partial?level=ERROR&limit=100
func LogsPartialHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		level := q.Get("level")

		limit := 100
		if n, _ := strconv.Atoi(q.Get("limit")); n > 0 && n <= 1000 {
			limit = n
		}

		var logs []storage.LogEntry
		dbq := ctx.DB.Order("created_at desc").Limit(limit)
		if level != "" {
			dbq = dbq.Where("level = ?", level)
		}
		if err := dbq.Find(&logs).Error; err != nil {
			ctx.Logger.Error("logs partial query failed", zap.Error(err))
			http.Error(w, "db error", 500)
			return
		}

		tpl, err := loadTemplate("log_partial.html")
		if err != nil {
			http.Error(w, "template error", 500)
			return
		}

		tpl.Execute(w, map[string]any{"Logs": logs})
	}
}
