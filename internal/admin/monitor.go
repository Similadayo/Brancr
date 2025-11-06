package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"go.uber.org/zap"
)

// ---------------------------------------------------------------------
// STRUCTS
// ---------------------------------------------------------------------

type HealthReport struct {
	Timestamp  time.Time         `json:"timestamp"`
	DB         string            `json:"db"`
	Redis      string            `json:"redis"`
	Telegram   string            `json:"telegram"`
	Cloudinary string            `json:"cloudinary"`
	OpenRouter string            `json:"openrouter"`
	Notes      map[string]string `json:"notes,omitempty"`
}

// ---------------------------------------------------------------------
// HELPERS
// ---------------------------------------------------------------------

func pingURL(raw string) string {
	if raw == "" {
		return "not-configured"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid-url"
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(u.String())
	if err != nil {
		return "fail: " + err.Error()
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "ok"
	}
	return fmt.Sprintf("fail: http %d", resp.StatusCode)
}

// ✅ Telegram: ping /getMe endpoint
func checkTelegram() string {
	token := os.Getenv("BRANCR_ADMIN_BOT_TOKEN")
	if token == "" {
		// Check if any tenant bots are configured instead
		// This is optional - admin bot is only for onboarding
		return "not-configured (optional - admin bot only)"
	}
	resp, err := http.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token))
	if err != nil {
		return "fail: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		return "ok"
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Sprintf("fail: %d → %s", resp.StatusCode, string(body))
}

// ✅ Cloudinary: simple /resources call
func checkCloudinary() string {
	cloudName := os.Getenv("CLOUDINARY_CLOUD_NAME")
	apiKey := os.Getenv("CLOUDINARY_API_KEY")
	apiSecret := os.Getenv("CLOUDINARY_API_SECRET")
	if cloudName == "" || apiKey == "" || apiSecret == "" {
		return "not-configured"
	}
	req, _ := http.NewRequest("GET",
		fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/resources/image", cloudName),
		nil)
	req.SetBasicAuth(apiKey, apiSecret)
	client := &http.Client{Timeout: 4 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "fail: " + err.Error()
	}
	defer res.Body.Close()
	if res.StatusCode == 200 {
		return "ok"
	}
	body, _ := io.ReadAll(res.Body)
	return fmt.Sprintf("fail: %d → %s", res.StatusCode, string(body))
}

// ✅ OpenRouter: test /models endpoint
func checkOpenRouter() string {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return "not-configured"
	}
	req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "fail: " + err.Error()
	}
	defer res.Body.Close()
	if res.StatusCode == 200 {
		return "ok"
	}
	body, _ := io.ReadAll(res.Body)
	return fmt.Sprintf("fail: %d → %s", res.StatusCode, string(body))
}

// ---------------------------------------------------------------------
// CORE HEALTH BUILDER
// ---------------------------------------------------------------------

func buildHealthReport(ctx *AdminContext) map[string]interface{} {
	report := map[string]interface{}{
		"DB":         "ok",
		"Redis":      "ok",
		"Telegram":   "ok",
		"Cloudinary": "ok",
		"OpenRouter": "ok",
		"Notes":      map[string]string{},
		"Timestamp":  time.Now(),
	}

	// ✅ Database
	sqlDB, err := ctx.DB.DB()
	if err != nil || sqlDB.Ping() != nil {
		report["DB"] = "fail"
		report["Notes"].(map[string]string)["db"] = "Database ping failed"
	}

	// ✅ Redis
	start := time.Now()
	if ctx.Redis == nil {
		report["Redis"] = "fail"
	} else {
		err := ctx.Redis.Ping(context.Background()).Err()
		if err != nil {
			report["Redis"] = "fail"
			report["Notes"].(map[string]string)["redis"] = err.Error()
		} else {
			latency := time.Since(start).Milliseconds()
			report["Notes"].(map[string]string)["redis_latency"] = fmt.Sprintf("%dms", latency)
		}
	}

	// ✅ Telegram / Cloudinary / OpenRouter
	report["Telegram"] = checkTelegram()
	report["Cloudinary"] = checkCloudinary()
	report["OpenRouter"] = checkOpenRouter()

	return report
}

// ---------------------------------------------------------------------
// HANDLERS
// ---------------------------------------------------------------------

func HealthHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := buildHealthReport(ctx)
		data["Page"] = "health"
		renderTemplate(w, "health.html", data, ctx.Logger)
	}
}

func HealthJSONHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildHealthReport(ctx))
	}
}

// HealthPartialHandler returns HTML partial for HTMX updates
func HealthPartialHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := buildHealthReport(ctx)
		
		// Load the health partial template
		tpl, err := template.ParseFiles("internal/admin/templates/health_partial.html")
		if err != nil {
			ctx.Logger.Error("health partial template error", zap.Error(err))
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
		
		if err := tpl.Execute(w, data); err != nil {
			ctx.Logger.Error("health partial execute error", zap.Error(err))
			http.Error(w, "render error", http.StatusInternalServerError)
		}
	}
}
