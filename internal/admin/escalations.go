package admin

import (
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

type escalationRow struct {
	InteractionID  uuid.UUID
	TenantID       uuid.UUID
	TenantName     string
	CustomerID     uuid.UUID
	CustomerName   string
	Platform       string
	Intent         string
	Tone           string
	Content        string
	ContentPreview string
	CreatedAt      string
	Status         string
}

func previewText(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	out := make([]rune, 0, maxRunes)
	for _, r := range s {
		out = append(out, r)
		if len(out) >= maxRunes {
			break
		}
	}
	return string(out) + "…"
}

func EscalationsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var interactions []storage.Interaction
		if err := ctx.DB.
			Where("response_type = ? AND response_status = ?", "escalated", "pending").
			Order("created_at DESC").
			Limit(200).
			Find(&interactions).Error; err != nil {
			ctx.Logger.Error("failed to fetch escalations", zap.Error(err))
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		tenantIDs := make([]uuid.UUID, 0, len(interactions))
		seenTenants := map[uuid.UUID]struct{}{}
		for _, it := range interactions {
			if _, ok := seenTenants[it.TenantID]; !ok {
				seenTenants[it.TenantID] = struct{}{}
				tenantIDs = append(tenantIDs, it.TenantID)
			}
		}

		tenantNames := map[uuid.UUID]string{}
		if len(tenantIDs) > 0 {
			var tenants []storage.Tenant
			_ = ctx.DB.Where("id IN ?", tenantIDs).Find(&tenants).Error
			for _, t := range tenants {
				tenantNames[t.ID] = t.Name
			}
		}

		// Only show allowed fields and redacted preview (30-50 chars, mask PII)
		rows := make([]escalationRow, 0, len(interactions))
		for _, it := range interactions {
			preview := maskPII(previewText(it.Content, 40))
			rows = append(rows, escalationRow{
				InteractionID:  it.ID,
				TenantID:       it.TenantID,
				TenantName:     tenantNames[it.TenantID],
				Platform:       it.Platform,
				CreatedAt:      it.CreatedAt.Format("Jan 2, 2006 15:04"),
				Status:         it.ResponseStatus,
				ContentPreview: preview,
			})
		}

		data := map[string]any{
			"Page":        "escalations",
			"Escalations": rows,
			"Total":       len(rows),
		}
		renderTemplate(w, r, "escalations.html", data, ctx.Logger)
	}
}

// maskPII masks phone numbers, emails, credit card numbers and long numeric sequences in a string
func maskPII(s string) string {
	// Simple regex-based masking for phone numbers and emails
	// (For brevity, not full RFC/Luhn compliance)
	emailRe := regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	phoneRe := regexp.MustCompile(`\+?\d[\d\s\-]{7,}\d`)
	// match 13-16 digit sequences allowing spaces or dashes (common CC patterns)
	cardRe := regexp.MustCompile(`(?i)\b(?:\d[ -]*?){13,16}\b`)
	// match long numeric sequences (e.g., account numbers)
	longNumRe := regexp.MustCompile(`\b\d{9,}\b`)

	s = emailRe.ReplaceAllString(s, "[redacted email]")
	s = phoneRe.ReplaceAllString(s, "[redacted phone]")
	s = cardRe.ReplaceAllString(s, "[redacted card]")
	// mask any long numeric sequences not already caught
	s = longNumRe.ReplaceAllString(s, "[redacted number]")
	return s
}

func EscalationResolveHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only PlatformAdmin and SuperAdmin can resolve/ignore escalations
		role := AdminRoleFromRequest(r)
		if role != RoleSuperAdmin && role != RolePlatformAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil || id == uuid.Nil {
			http.Error(w, "invalid interaction id", http.StatusBadRequest)
			return
		}

		action := r.URL.Query().Get("action")
		status := "resolved"
		if action == "ignore" {
			status = "ignored"
		}

		if err := ctx.DB.Model(&storage.Interaction{}).
			Where("id = ?", id).
			Update("response_status", status).Error; err != nil {
			ctx.Logger.Error("failed to update escalation status", zap.Stringer("interaction_id", id), zap.Error(err))
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		// Send SSE notification for escalation resolution
		if ctx.Events != nil {
			msg := "Escalation " + idStr + " " + status + " by " + AdminEmailFromRequest(r)
			ctx.Events.Publish(msg)
		}

		http.Redirect(w, r, "/admin/escalations", http.StatusSeeOther)
	}
}
