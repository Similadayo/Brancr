package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/checkpoint"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// CheckpointStatsHandler returns aggregated stats for checkpoints
func CheckpointStatsHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var stats struct {
			Total         int64            `json:"total"`
			Responded     int64            `json:"responded"`
			ActivePending int64            `json:"active_pending"`
			ByType        map[string]int64 `json:"by_type"`
			TrustScores   map[int]int64    `json:"trust_scores"` // 1-5
		}
		stats.ByType = make(map[string]int64)
		stats.TrustScores = make(map[int]int64)

		// Total Checkpoints
		ctx.DB.Model(&storage.ExperienceCheckpoint{}).Count(&stats.Total)

		// Responded
		ctx.DB.Model(&storage.ExperienceCheckpoint{}).Where("status = ?", "responded").Count(&stats.Responded)

		// Active Pending
		ctx.DB.Model(&storage.ExperienceCheckpoint{}).Where("status = ? AND expires_at > ?", "pending", time.Now()).Count(&stats.ActivePending)

		// By Type
		type result struct {
			EventType string
			Count     int64
		}
		var results []result
		ctx.DB.Model(&storage.ExperienceCheckpoint{}).Select("event_type, count(*) as count").Group("event_type").Scan(&results)
		for _, row := range results {
			stats.ByType[row.EventType] = row.Count
		}

		// Trust Scores
		type scoreResult struct {
			TrustScore int
			Count      int64
		}
		var scoreResults []scoreResult
		ctx.DB.Model(&storage.CheckpointResponse{}).Where("trust_score IS NOT NULL").Select("trust_score, count(*) as count").Group("trust_score").Scan(&scoreResults)
		for _, row := range scoreResults {
			stats.TrustScores[row.TrustScore] = row.Count
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

// CheckpointResponsesHandler lists recent checkpoint responses
func CheckpointResponsesHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var responses []storage.CheckpointResponse

		query := ctx.DB.Order("created_at DESC").Limit(50)

		// Filter by event type if provided
		if eventType := r.URL.Query().Get("event_type"); eventType != "" {
			query = query.Where("event_type = ?", eventType)
		}

		if err := query.Find(&responses).Error; err != nil {
			ctx.Logger.Error("failed to list checkpoint responses", zap.Error(err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// DTO for explicit JSON tags to match frontend/snake_case expectations
		type CheckpointResponseDTO struct {
			ID             string `json:"id"`
			CreatedAt      string `json:"created_at"`
			TenantID       string `json:"tenant_id"`
			TenantName     string `json:"tenant_name"`
			EventType      string `json:"event_type"`
			SelectedReason string `json:"selected_reason"`
			TrustScore     *int   `json:"trust_score"`
			FreeText       string `json:"free_text"`
		}

		dtos := make([]CheckpointResponseDTO, 0, len(responses))
		for _, resp := range responses {
			var tenant storage.Tenant
			name := "Unknown"
			if err := ctx.DB.Select("name").First(&tenant, resp.TenantID).Error; err == nil {
				name = tenant.Name
			}

			dtos = append(dtos, CheckpointResponseDTO{
				ID:             resp.ID.String(),
				CreatedAt:      resp.CreatedAt.Format(time.RFC3339),
				TenantID:       resp.TenantID.String(),
				TenantName:     name,
				EventType:      resp.EventType,
				SelectedReason: resp.SelectedReason,
				TrustScore:     resp.TrustScore,
				FreeText:       resp.FreeText,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"responses": dtos,
		})
	}
}

// TriggerCheckpointHandler allows admins to manually trigger a checkpoint structure for a tenant or group
func TriggerCheckpointHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			TargetType  string `json:"target_type"`  // "individual" or "group"
			TargetValue string `json:"target_value"` // tenant_id or "new", "active", "all"
			TenantID    string `json:"tenant_id"`    // Legacy support / fallback for individual
			EventType   string `json:"event_type"`   // first_reply, escalation, ai_toggle, onboarding_48h
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Normalize input
		if req.TargetType == "" {
			req.TargetType = "individual" // Default
		}
		if req.TargetType == "individual" && req.TargetValue == "" {
			req.TargetValue = req.TenantID
		}

		svc := checkpoint.NewService(ctx.DB, ctx.Logger)
		eventID := uuid.New() // Shared event ID for this batch

		var tenants []storage.Tenant

		if req.TargetType == "individual" {
			tenantUUID, err := uuid.Parse(req.TargetValue)
			if err != nil {
				http.Error(w, "invalid tenant_id", http.StatusBadRequest)
				return
			}
			// Just trigger for one
			if err := svc.TriggerCheckpoint(r.Context(), tenantUUID, req.EventType, eventID); err != nil {
				ctx.Logger.Error("failed to trigger checkpoint", zap.Error(err))
				http.Error(w, "failed to trigger checkpoint: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else if req.TargetType == "group" {
			query := ctx.DB.Model(&storage.Tenant{})

			switch req.TargetValue {
			case "new":
				// Last 7 days
				query = query.Where("created_at > ?", time.Now().AddDate(0, 0, -7))
			case "active":
				query = query.Where("status = ?", "active")
			case "all":
				// No filter
			default:
				http.Error(w, "invalid group filter", http.StatusBadRequest)
				return
			}

			if err := query.Find(&tenants).Error; err != nil {
				http.Error(w, "failed to fetch tenants", http.StatusInternalServerError)
				return
			}

			// Process asynchronously if too many, but for now we do it synchronously for feedback
			// Limit to avoid timeouts? Let's cap at 500 for safety or loop.
			// Ideally this should be a background job.

			go func() {
				count := 0
				for _, t := range tenants {
					if err := svc.TriggerCheckpoint(context.Background(), t.ID, req.EventType, eventID); err == nil {
						count++
					}
				}
				ctx.Logger.Info("bulk checkpoint trigger completed",
					zap.String("group", req.TargetValue),
					zap.Int("count", count))
			}()

			// Return immediately saying passing to background
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":      true,
				"message":      "Bulk trigger started in background.",
				"target_count": len(tenants),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Checkpoint triggered successfully.",
		})
	}
}
