package admin

import (
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ✅ GET /admin/scheduled
func ScheduledHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var posts []storage.ScheduledPost
		if err := ctx.DB.Order("scheduled_at desc").Limit(200).Find(&posts).Error; err != nil {
			ctx.Logger.Warn("fetch scheduled", zap.Error(err))
		}

		data := map[string]any{
			"Page":  "scheduled", // highlights sidebar
			"Posts": posts,
			"Now":   time.Now(),
		}

		renderTemplate(w, "scheduled.html", data, ctx.Logger)
	}
}

// ✅ GET /admin/scheduled/force_cancel_form?id=...
func ForceCancelFormHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}

		// Return partial modal HTML (loaded via HTMX)
		tpl, err := loadTemplate("force_cancel_form.html")
		if err != nil {
			ctx.Logger.Error("load force cancel form", zap.Error(err))
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}

		tpl.Execute(w, map[string]any{
			"ID": id,
		})
	}
}

// ✅ POST /admin/scheduled/force_cancel
func ForceCancelHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.FormValue("id")
		reason := r.FormValue("reason")

		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}

		// Mark post as cancelled
		err := ctx.DB.Model(&storage.ScheduledPost{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"status":     "cancelled",
				"last_error": "force-cancelled by admin: " + reason,
			}).Error

		if err != nil {
			ctx.Logger.Warn("force cancel failed", zap.Error(err))
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		ctx.Logger.Info("🛑 Force-cancelled scheduled post",
			zap.String("post_id", id),
			zap.String("reason", reason))

		http.Redirect(w, r, "/admin/scheduled", http.StatusSeeOther)
	}
}
