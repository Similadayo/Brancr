package admin

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// SystemHealthAPIHandler returns only system health section
// GET /admin/api/dashboard/system-health?period=24h|7d
func SystemHealthAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "24h"
		}

		var customStart, customEnd *time.Time
		if period == "custom" {
			startDateStr := r.URL.Query().Get("start_date")
			endDateStr := r.URL.Query().Get("end_date")
			if startDateStr != "" && endDateStr != "" {
				s, err1 := time.Parse("2006-01-02", startDateStr)
				e, err2 := time.Parse("2006-01-02", endDateStr)
				if err1 == nil && err2 == nil {
					e = e.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
					customStart = &s
					customEnd = &e
				}
			}
		}

		reqCtx := r.Context()
		if _, hasDeadline := reqCtx.Deadline(); !hasDeadline {
			var cancel func()
			reqCtx, cancel = context.WithTimeout(reqCtx, 30*time.Second)
			defer cancel()
		}
		data, err := BuildSystemHealthData(ctx, period, customStart, customEnd, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build system health data", zap.Error(err))
			http.Error(w, "failed to build system health data", http.StatusInternalServerError)
			return
		}

		respondJSON(w, data)
	}
}

// AIBehaviorAPIHandler returns only AI behavior section
// GET /admin/api/dashboard/ai-behavior?period=24h|7d
func AIBehaviorAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "24h"
		}

		var customStart, customEnd *time.Time
		if period == "custom" {
			startDateStr := r.URL.Query().Get("start_date")
			endDateStr := r.URL.Query().Get("end_date")
			if startDateStr != "" && endDateStr != "" {
				s, err1 := time.Parse("2006-01-02", startDateStr)
				e, err2 := time.Parse("2006-01-02", endDateStr)
				if err1 == nil && err2 == nil {
					e = e.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
					customStart = &s
					customEnd = &e
				}
			}
		}

		reqCtx := r.Context()
		if _, hasDeadline := reqCtx.Deadline(); !hasDeadline {
			var cancel func()
			reqCtx, cancel = context.WithTimeout(reqCtx, 30*time.Second)
			defer cancel()
		}
		data, err := BuildAIBehaviorData(ctx, period, customStart, customEnd, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build AI behavior data", zap.Error(err))
			http.Error(w, "failed to build AI behavior data", http.StatusInternalServerError)
			return
		}

		respondJSON(w, data)
	}
}

// TenantOverviewAPIHandler returns only tenant overview section
// GET /admin/api/dashboard/tenant-overview
func TenantOverviewAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filters := map[string]interface{}{}
		// Parse query params for filters if needed
		if tags := r.URL.Query().Get("tags"); tags != "" {
			// tags would be comma-separated
			// filters["tags"] = strings.Split(tags, ",")
		}

		// Pagination
		limit := 50 // Default limit
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		offset := 0
		if o := r.URL.Query().Get("offset"); o != "" {
			if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
				offset = parsed
			}
		}

		reqCtx := r.Context()
		if _, hasDeadline := reqCtx.Deadline(); !hasDeadline {
			var cancel func()
			reqCtx, cancel = context.WithTimeout(reqCtx, 30*time.Second)
			defer cancel()
		}
		data, err := BuildTenantOverviewData(ctx, filters, limit, offset, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build tenant overview data", zap.Error(err))
			http.Error(w, "failed to build tenant overview data", http.StatusInternalServerError)
			return
		}

		respondJSON(w, data)
	}
}

// MessagingActivityAPIHandler returns only messaging activity section
// GET /admin/api/dashboard/messaging-activity?period=24h|7d
func MessagingActivityAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "24h"
		}

		var customStart, customEnd *time.Time
		if period == "custom" {
			startDateStr := r.URL.Query().Get("start_date")
			endDateStr := r.URL.Query().Get("end_date")
			if startDateStr != "" && endDateStr != "" {
				s, err1 := time.Parse("2006-01-02", startDateStr)
				e, err2 := time.Parse("2006-01-02", endDateStr)
				if err1 == nil && err2 == nil {
					e = e.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
					customStart = &s
					customEnd = &e
				}
			}
		}

		reqCtx := r.Context()
		if _, hasDeadline := reqCtx.Deadline(); !hasDeadline {
			var cancel func()
			reqCtx, cancel = context.WithTimeout(reqCtx, 30*time.Second)
			defer cancel()
		}
		data, err := BuildMessagingActivityData(ctx, period, customStart, customEnd, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build messaging activity data", zap.Error(err))
			http.Error(w, "failed to build messaging activity data", http.StatusInternalServerError)
			return
		}

		respondJSON(w, data)
	}
}

// PostingStatusAPIHandler returns only posting status section
// GET /admin/api/dashboard/posting-status?period=24h|7d
func PostingStatusAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "24h"
		}

		var customStart, customEnd *time.Time
		if period == "custom" {
			startDateStr := r.URL.Query().Get("start_date")
			endDateStr := r.URL.Query().Get("end_date")
			if startDateStr != "" && endDateStr != "" {
				s, err1 := time.Parse("2006-01-02", startDateStr)
				e, err2 := time.Parse("2006-01-02", endDateStr)
				if err1 == nil && err2 == nil {
					e = e.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
					customStart = &s
					customEnd = &e
				}
			}
		}

		reqCtx := r.Context()
		if _, hasDeadline := reqCtx.Deadline(); !hasDeadline {
			var cancel func()
			reqCtx, cancel = context.WithTimeout(reqCtx, 30*time.Second)
			defer cancel()
		}
		data, err := BuildPostingStatusData(ctx, period, customStart, customEnd, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build posting status data", zap.Error(err))
			http.Error(w, "failed to build posting status data", http.StatusInternalServerError)
			return
		}

		respondJSON(w, data)
	}
}

// CostControlAPIHandler returns only cost control section
// GET /admin/api/dashboard/cost-control?period=24h|7d
func CostControlAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := AdminRoleFromRequest(r)
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "24h"
		}

		var customStart, customEnd *time.Time
		if period == "custom" {
			startDateStr := r.URL.Query().Get("start_date")
			endDateStr := r.URL.Query().Get("end_date")
			if startDateStr != "" && endDateStr != "" {
				s, err1 := time.Parse("2006-01-02", startDateStr)
				e, err2 := time.Parse("2006-01-02", endDateStr)
				if err1 == nil && err2 == nil {
					// Set end time to end of day
					e = e.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
					customStart = &s
					customEnd = &e
				}
			}
		}

		reqCtx := r.Context()
		if _, hasDeadline := reqCtx.Deadline(); !hasDeadline {
			var cancel func()
			reqCtx, cancel = context.WithTimeout(reqCtx, 30*time.Second)
			defer cancel()
		}
		data, err := BuildCostControlData(ctx, period, customStart, customEnd, role, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build cost control data", zap.Error(err))
			http.Error(w, "failed to build cost control data", http.StatusInternalServerError)
			return
		}

		// Filter for non-founder roles
		data = FilterCostDataForRole(data, role)
		respondJSON(w, data)
	}
}

// QualitativeInsightsAPIHandler returns only qualitative insights section
// GET /admin/api/dashboard/qualitative-insights?period=24h|7d
func QualitativeInsightsAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "24h"
		}

		var customStart, customEnd *time.Time
		if period == "custom" {
			startDateStr := r.URL.Query().Get("start_date")
			endDateStr := r.URL.Query().Get("end_date")
			if startDateStr != "" && endDateStr != "" {
				s, err1 := time.Parse("2006-01-02", startDateStr)
				e, err2 := time.Parse("2006-01-02", endDateStr)
				if err1 == nil && err2 == nil {
					e = e.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
					customStart = &s
					customEnd = &e
				}
			}
		}

		reqCtx := r.Context()
		if _, hasDeadline := reqCtx.Deadline(); !hasDeadline {
			var cancel func()
			reqCtx, cancel = context.WithTimeout(reqCtx, 30*time.Second)
			defer cancel()
		}
		data, err := BuildQualitativeInsightsData(ctx, period, customStart, customEnd, reqCtx)
		if err != nil {
			ctx.Logger.Error("failed to build qualitative insights data", zap.Error(err))
			http.Error(w, "failed to build qualitative insights data", http.StatusInternalServerError)
			return
		}

		respondJSON(w, data)
	}
}
