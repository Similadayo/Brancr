package admin

import (
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

type escalationRow struct {
	InteractionID  uint
	TenantID       uint
	TenantName     string
	CustomerID     uint
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

		tenantIDs := make([]uint, 0, len(interactions))
		customerIDs := make([]uint, 0, len(interactions))
		seenTenants := map[uint]struct{}{}
		seenCustomers := map[uint]struct{}{}
		for _, it := range interactions {
			if _, ok := seenTenants[it.TenantID]; !ok {
				seenTenants[it.TenantID] = struct{}{}
				tenantIDs = append(tenantIDs, it.TenantID)
			}
			if _, ok := seenCustomers[it.CustomerID]; !ok {
				seenCustomers[it.CustomerID] = struct{}{}
				customerIDs = append(customerIDs, it.CustomerID)
			}
		}

		tenantNames := map[uint]string{}
		if len(tenantIDs) > 0 {
			var tenants []storage.Tenant
			_ = ctx.DB.Where("id IN ?", tenantIDs).Find(&tenants).Error
			for _, t := range tenants {
				tenantNames[t.ID] = t.Name
			}
		}

		customerNames := map[uint]string{}
		if len(customerIDs) > 0 {
			var customers []storage.Customer
			_ = ctx.DB.Where("id IN ?", customerIDs).Find(&customers).Error
			for _, c := range customers {
				name := c.DisplayName
				if name == "" {
					name = c.Username
				}
				if name == "" {
					name = c.PlatformID
				}
				customerNames[c.ID] = name
			}
		}

		rows := make([]escalationRow, 0, len(interactions))
		for _, it := range interactions {
			rows = append(rows, escalationRow{
				InteractionID:  it.ID,
				TenantID:       it.TenantID,
				TenantName:     tenantNames[it.TenantID],
				CustomerID:     it.CustomerID,
				CustomerName:   customerNames[it.CustomerID],
				Platform:       it.Platform,
				Intent:         it.DetectedIntent,
				Tone:           it.DetectedTone,
				Content:        it.Content,
				ContentPreview: previewText(it.Content, 180),
				CreatedAt:      it.CreatedAt.Format("Jan 2, 2006 15:04"),
				Status:         it.ResponseStatus,
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

func EscalationResolveHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id64, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id64 == 0 {
			http.Error(w, "invalid interaction id", http.StatusBadRequest)
			return
		}

		action := r.URL.Query().Get("action")
		status := "resolved"
		if action == "ignore" {
			status = "ignored"
		}

		if err := ctx.DB.Model(&storage.Interaction{}).
			Where("id = ?", uint(id64)).
			Update("response_status", status).Error; err != nil {
			ctx.Logger.Error("failed to update escalation status", zap.Uint("interaction_id", uint(id64)), zap.Error(err))
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/admin/escalations", http.StatusSeeOther)
	}
}
