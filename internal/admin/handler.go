package admin

import (
	"html/template"
	"net/http"
	"path/filepath"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// renderTemplate loads both layout and page template together
func renderTemplate(w http.ResponseWriter, page string, data any, logger *zap.Logger) {
	layout := filepath.Join("internal", "admin", "templates", "layout.html")
	pagePath := filepath.Join("internal", "admin", "templates", page)

	tpl, err := template.ParseFiles(layout, pagePath)
	if err != nil {
		logger.Error("template parse failed", zap.String("page", page), zap.Error(err))
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	err = tpl.ExecuteTemplate(w, "layout.html", data)
	if err != nil {
		logger.Error("template execute failed", zap.String("page", page), zap.Error(err))
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// simple login template loader
func loadTemplate(name string) (*template.Template, error) {
	return template.ParseFiles("internal/admin/templates/" + name)
}

// ✅ GET /admin/login
func GetLoginHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tpl, err := loadTemplate("login.html")
		if err != nil {
			ctx.Logger.Error("load login template", zap.Error(err))
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
		tpl.Execute(w, map[string]interface{}{
			"AppName": "Brancr Admin",
		})
	}
}

// ✅ POST /admin/login
func PostLoginHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")
		cfgEmail := ctx.AdminEmail
		cfgPassword := ctx.AdminPassword

		if cfgEmail == "" || cfgPassword == "" {
			ctx.Logger.Warn("admin credentials not set in config")
			http.Error(w, "admin credentials not configured", http.StatusInternalServerError)
			return
		}

		if email != cfgEmail {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		useHash := len(cfgPassword) > 3 && (cfgPassword[:3] == "$2a" || cfgPassword[:3] == "$2b")
		ok := false
		if useHash {
			if err := bcrypt.CompareHashAndPassword([]byte(cfgPassword), []byte(password)); err == nil {
				ok = true
			}
		} else if password == cfgPassword {
			ok = true
		}

		if !ok {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		tokenStr, err := createToken(ctx, cfgEmail, 24*time.Hour)
		if err != nil {
			ctx.Logger.Error("create token", zap.Error(err))
			http.Error(w, "could not create token", http.StatusInternalServerError)
			return
		}
		setAdminCookie(w, tokenStr)
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
	}
}

// ✅ GET /admin/
func DashboardHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := GetAnalyticsStats(ctx)

		data := map[string]interface{}{
			"Page":              "dashboard",
			"TotalTenants":      stats.TotalTenants,
			"ActiveBots":        stats.ActiveTenants,
			"PostsToday":        stats.EventsToday,
			"PaymentsToday":     stats.PaymentsToday,
			"DeliveriesToday":   stats.DeliveriesToday,
			"AppointmentsToday": stats.AppointmentsToday,
			"OpenRouterUsage":   stats.OpenRouterUsage,
			"CloudinaryUsage":   stats.CloudinaryUsage,
		}

		renderTemplate(w, "dashboard.html", data, ctx.Logger)
	}
}

// ✅ GET /admin/tenants
func TenantsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type Tenant struct {
			ID        uint
			Name      string
			Email     string
			PlanType  string
			Status    string
			CreatedAt time.Time
		}

		var tenants []Tenant
		if ctx.DB != nil {
			if err := ctx.DB.Model(&Tenant{}).Order("created_at DESC").Find(&tenants).Error; err != nil {
				ctx.Logger.Warn("failed to fetch tenants", zap.Error(err))
			}
		}

		data := map[string]any{
			"Page":       "tenants",
			"Tenants":    tenants,
			"TotalCount": len(tenants),
		}

		renderTemplate(w, "tenants.html", data, ctx.Logger)
	}
}

// ✅ GET /admin/tenants/{id}
func TenantDetailHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// For now, just redirect back to tenants list
		// Can be extended later to show tenant details
		http.Redirect(w, r, "/admin/tenants", http.StatusSeeOther)
	}
}

func SocialAccountsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var accounts []storage.SocialAccount
		ctx.DB.Find(&accounts)

		data := map[string]any{
			"Accounts": accounts,
		}

		renderTemplate(w, "social.html", data, ctx.Logger)
	}
}

// ✅ POST /admin/logout
func LogoutHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearAdminCookie(w)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	}
}
