package admin

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// CostControlData represents Section 6: Usage & Cost Control (Founder only)
type CostControlData struct {
	OpenAITokensUsed     int64        `json:"openai_tokens_used"`
	EstimatedAICost      float64      `json:"estimated_ai_cost"` // USD - AI costs only
	WhatsAppMessagesSent int64        `json:"whatsapp_messages_sent"`
	WhatsAppCost         float64      `json:"whatsapp_cost"` // USD - WhatsApp costs only
	TotalCost            float64      `json:"total_cost"`    // USD - AI + WhatsApp combined
	CostPerTenant        []TenantCost `json:"cost_per_tenant"`
	TopCostTenants       []TenantCost `json:"top_cost_tenants"`
	Alerts               []CostAlert  `json:"alerts"`
	// Currency conversion rates (USD base)
	CurrencyRates map[string]float64 `json:"currency_rates,omitempty"`
}

// TenantCost represents cost for a specific tenant
type TenantCost struct {
	TenantID         uuid.UUID `json:"tenant_id"`
	TenantName       string    `json:"tenant_name"`
	Cost             float64   `json:"cost"` // USD - AI cost
	TokensUsed       int64     `json:"tokens_used"`
	WhatsAppCost     float64   `json:"whatsapp_cost"`     // USD - WhatsApp cost
	WhatsAppMessages int64     `json:"whatsapp_messages"` // WhatsApp messages sent
	TotalCost        float64   `json:"total_cost"`        // USD - AI + WhatsApp combined
}

// CostAlert represents a cost-related alert
type CostAlert struct {
	Type     string     `json:"type"` // "spike", "threshold", "outlier"
	Message  string     `json:"message"`
	TenantID *uuid.UUID `json:"tenant_id,omitempty"`
}

// Currency conversion rates (to USD as base)
// These are approximate rates - in production, fetch from an API
var currencyRates = map[string]float64{
	"USD": 1.0,
	"NGN": 0.00069, // 1 USD = ~1450 NGN (approximate, should be fetched from API)
	"EUR": 1.08,
	"GBP": 1.27,
}

// BuildCostControlData builds cost control metrics for the dashboard
// IMPORTANT: This should only return full data for RoleSuperAdmin
// Other roles get filtered/redacted data
// BuildCostControlData builds cost control metrics for the dashboard
// IMPORTANT: This should only return full data for RoleSuperAdmin
// Other roles get filtered/redacted data
func BuildCostControlData(ctx *AdminContext, period string, customStart, customEnd *time.Time, role string, reqCtx context.Context) (*CostControlData, error) {
	var since time.Time
	var unti time.Time = time.Now()

	if period == "custom" && customStart != nil && customEnd != nil {
		since = *customStart
		unti = *customEnd
		// Validate range
		if since.After(unti) {
			since, unti = unti, since
		}
	} else if period == "7d" {
		since = time.Now().Add(-7 * 24 * time.Hour)
	} else if period == "30d" {
		since = time.Now().Add(-30 * 24 * time.Hour)
	} else {
		// Default to 24h
		since = time.Now().Add(-24 * time.Hour)
	}

	data := &CostControlData{
		CostPerTenant:  []TenantCost{},
		TopCostTenants: []TenantCost{},
		Alerts:         []CostAlert{},
	}

	// Get cost data from tenant_quotas
	// Query ALL tenants with AI activity in the period, regardless of cost_used value
	// This ensures we see tenants even if cost tracking hasn't worked yet
	var tenantCosts []struct {
		TenantID    uuid.UUID
		TenantName  string
		CostUsed    float64
		AICallsUsed int64
		TokensUsed  int64
		UpdatedAt   time.Time
	}

	var quota storage.TenantQuota
	tableName := quota.TableName() // This returns "tenant_quotas" (plural)

	// Query AI costs from UsageEvent table (primary source for accurate cost tracking)
	// This is more reliable than tenant_quotas.updated_at which may not reflect recent activity
	var usageEventCosts []struct {
		TenantID    uuid.UUID
		TenantName  string
		CostUsed    float64
		AICallsUsed int64
		TokensUsed  int64
		UpdatedAt   time.Time
	}

	// Query UsageEvent for AI activity in the period (primary source)
	// GORM automatically uses "usage_events" as table name (pluralized, snake_case)
	usageEventErr := ctx.DB.WithContext(reqCtx).
		Model(&storage.UsageEvent{}).
		Select("usage_events.tenant_id, COALESCE(NULLIF(tenants.name, ''), 'Unknown') as tenant_name, "+
			"COALESCE(SUM(usage_events.estimated_cost), 0) as cost_used, "+
			"COUNT(CASE WHEN usage_events.event_type = 'ai_reply_sent' THEN 1 END) as ai_calls_used, "+
			"COALESCE(SUM(CASE WHEN usage_events.event_type = 'ai_tokens_used' THEN usage_events.quantity ELSE 0 END), 0) as tokens_used, "+
			"MAX(usage_events.event_date) as updated_at").
		Joins("LEFT JOIN tenants ON tenants.id::text = usage_events.tenant_id::text").
		Where("usage_events.event_type IN ? AND usage_events.event_date >= ? AND usage_events.event_date <= ?",
			[]string{"ai_reply_sent", "ai_tokens_used"}, since, unti).
		Group("usage_events.tenant_id, tenants.name").
		Order("cost_used DESC, ai_calls_used DESC").
		Scan(&usageEventCosts).Error

	if usageEventErr == nil && len(usageEventCosts) > 0 {
		// Use UsageEvent data as primary source
		tenantCosts = usageEventCosts
		ctx.Logger.Info("found AI costs from UsageEvent", zap.Int("tenant_count", len(tenantCosts)), zap.String("period", period))
	} else {
		// Fallback: query tenant_quotas (may have cost data even if UsageEvent doesn't)
		// Remove time restriction to get all tenants with AI activity
		ctx.Logger.Debug("no UsageEvent data found or error occurred, querying tenant_quotas",
			zap.Error(usageEventErr), zap.String("period", period))
		if err := ctx.DB.WithContext(reqCtx).
			Table(tableName).
			Select(tableName + ".tenant_id, COALESCE(NULLIF(tenants.name, ''), 'Unknown') as tenant_name, " +
				"COALESCE(SUM(" + tableName + ".cost_used), 0) as cost_used, " +
				"COALESCE(SUM(" + tableName + ".ai_calls_used), 0) as ai_calls_used, " +
				"0 as tokens_used, " + // No token tracking in tenant_quotas yet
				"MAX(" + tableName + ".updated_at) as updated_at").
			Joins("LEFT JOIN tenants ON tenants.id::text = " + tableName + ".tenant_id::text").
			Where("(" + tableName + ".ai_calls_used > 0 OR " + tableName + ".cost_used > 0)").
			Group(tableName + ".tenant_id, tenants.name").
			Order("cost_used DESC, ai_calls_used DESC").
			Scan(&tenantCosts).Error; err != nil {
			ctx.Logger.Error("failed to query tenant costs", zap.Error(err), zap.String("period", period))
			// Don't return error, try fallback query
		} else {
			ctx.Logger.Info("found AI costs from tenant_quotas", zap.Int("tenant_count", len(tenantCosts)), zap.String("period", period))
		}
	}

	// If no tenant costs found, query all tenants with ANY AI activity (even without cost_used)
	if len(tenantCosts) == 0 {
		ctx.Logger.Debug("no tenant costs found in primary query, checking for any AI activity", zap.String("period", period))
		if err := ctx.DB.WithContext(reqCtx).
			Table(tableName).
			Select(tableName + ".tenant_id, COALESCE(NULLIF(tenants.name, ''), 'Unknown') as tenant_name, " +
				"COALESCE(MAX(" + tableName + ".cost_used), 0) as cost_used, " +
				"COALESCE(MAX(" + tableName + ".ai_calls_used), 0) as ai_calls_used, " +
				"0 as tokens_used, " + // No token tracking
				"MAX(" + tableName + ".updated_at) as updated_at").
			Joins("LEFT JOIN tenants ON tenants.id = " + tableName + ".tenant_id").
			Where(tableName + ".ai_calls_used > 0").
			Group(tableName + ".tenant_id, tenants.name").
			Order("ai_calls_used DESC"). // Use the alias from SELECT (aggregated value)
			Limit(100).
			Scan(&tenantCosts).Error; err != nil {
			ctx.Logger.Warn("failed to query AI activity as fallback", zap.Error(err), zap.String("period", period))
		} else {
			ctx.Logger.Info("found tenants with AI activity but no cost tracking", zap.Int("count", len(tenantCosts)), zap.String("period", period))
		}
	}

	// Get WhatsApp message counts per tenant
	// ... (kept implicit as we jump to loop)

	// ... previous code continues until loop ...
	// Since I cannot update disjoint blocks easily, I rely on the user context having the full file.
	// But ReplaceFileContent needs contiguous block.
	// I will just replace the query block and fallback block first (Lines 112-172).
	// AND ALSO I need to update the LOOP (lines 255+).
	// I will process only the QUERY block first to fix syntax errors.
	// Then I will process the loop.
	// Wait, I can only do ONE replace call.
	// The query block is contiguous.
	// The loop is disjoint.
	// I will just fix the query block here.
	// Wait, the ReplacementContent above includes the query and fallback logic.
	// I will limit the fix to lines 112-152 (Query + Fallback).

	// If no tenant costs found, query all tenants with ANY AI activity (even without cost_used)
	if len(tenantCosts) == 0 {
		ctx.Logger.Debug("no tenant costs found in primary query, checking for any AI activity", zap.String("period", period))
		if err := ctx.DB.WithContext(reqCtx).
			Table(tableName).
			Select(tableName + ".tenant_id, COALESCE(tenants.name, 'Unknown') as tenant_name, " +
				"COALESCE(MAX(" + tableName + ".cost_used), 0) as cost_used, " +
				"COALESCE(MAX(" + tableName + ".ai_calls_used), 0) as ai_calls_used, " +
				"MAX(" + tableName + ".updated_at) as updated_at").
			Joins("LEFT JOIN tenants ON tenants.id::text = " + tableName + ".tenant_id::text").
			Where(tableName + ".ai_calls_used > 0").
			Group(tableName + ".tenant_id, tenants.name").
			Order("ai_calls_used DESC"). // Use the alias from SELECT (aggregated value)
			Limit(100).
			Scan(&tenantCosts).Error; err != nil {
			ctx.Logger.Warn("failed to query AI activity as fallback", zap.Error(err), zap.String("period", period))
		} else {
			ctx.Logger.Info("found tenants with AI activity but no cost tracking", zap.Int("count", len(tenantCosts)), zap.String("period", period))
		}
	}

	// Get WhatsApp message counts per tenant
	whatsappPerTenant := make(map[uuid.UUID]int64)
	var whatsappCounts []struct {
		TenantID uuid.UUID
		Count    int64
	}
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.Interaction{}).
		Select("tenant_id, COUNT(*) as count").
		Where("platform = ? AND created_at >= ? AND direction = ?", "whatsapp", since, "outgoing").
		Group("tenant_id").
		Scan(&whatsappCounts).Error; err == nil {
		for _, wc := range whatsappCounts {
			whatsappPerTenant[wc.TenantID] = wc.Count
		}
	} else {
		ctx.Logger.Debug("failed to count WhatsApp messages per tenant", zap.Error(err))
	}

	// Get actual Gupshup costs per tenant (if available)
	// Query GupshupUsage table for the date range
	actualWhatsAppCosts := make(map[uuid.UUID]float64)
	actualWhatsAppMessages := make(map[uuid.UUID]int64)
	if ctx.UsageSyncService != nil {
		// Get all WhatsApp apps for tenants in this period
		var whatsappApps []storage.WhatsAppApp
		if err := ctx.DB.WithContext(reqCtx).
			Where("status = ?", "live").
			Find(&whatsappApps).Error; err == nil {
			// Group apps by tenant
			appsByTenant := make(map[uuid.UUID][]storage.WhatsAppApp)
			for _, app := range whatsappApps {
				appsByTenant[app.TenantID] = append(appsByTenant[app.TenantID], app)
			}

			// For each tenant, sum up actual costs from GupshupUsage
			for tenantID, apps := range appsByTenant {
				var totalCost float64
				var totalMessages int64
				fromDateStr := since.Format("2006-01-02")
				toDateStr := unti.Format("2006-01-02")

				for _, app := range apps {
					var usageRecords []storage.GupshupUsage
					if err := ctx.DB.WithContext(reqCtx).
						Where("tenant_id = ? AND gupshup_app_id = ? AND date >= ? AND date <= ?",
							tenantID, app.GupshupAppID, fromDateStr, toDateStr).
						Find(&usageRecords).Error; err == nil {
						for _, usage := range usageRecords {
							totalCost += usage.TotalFees // TotalFees includes WhatsApp fees
							totalMessages += int64(usage.TotalMsg)
						}
					}
				}

				if totalCost > 0 {
					actualWhatsAppCosts[tenantID] = totalCost
					ctx.Logger.Debug("found actual Gupshup cost for tenant",
						zap.Stringer("tenant_id", tenantID),
						zap.Float64("actual_cost", totalCost),
						zap.Int64("messages", totalMessages))
				}
				if totalMessages > 0 {
					actualWhatsAppMessages[tenantID] = totalMessages
				}
			}
		}
	}

	// Calculate total cost and token usage
	totalCost := float64(0)
	totalTokens := int64(0)
	totalAICalls := int64(0)
	totalWhatsAppCost := float64(0)

	for _, tc := range tenantCosts {
		// Calculate tokens and cost
		// Logic:
		// 1. If we have precise TokensUsed from DB, use it.
		// 2. If we have CostUsed but no tokens, estimate tokens from cost.
		// 3. If we have neither but have AICallsUsed, estimate both.

		var tokensUsed int64 = tc.TokensUsed
		var estimatedCost float64 = tc.CostUsed

		if tc.TokensUsed > 0 {
			// We have precise token usage!
			// If cost is missing for some reason, estimate it from tokens
			if estimatedCost == 0 {
				// GPT-5-mini: ~$0.000001125 per token average
				estimatedCost = float64(tokensUsed) * 0.000001125
				ctx.Logger.Debug("estimating cost from precise tokens",
					zap.Stringer("tenant_id", tc.TenantID),
					zap.Int64("tokens", tokensUsed),
					zap.Float64("estimated_cost", estimatedCost))
			}
		} else if tc.CostUsed > 0 {
			// Use actual cost from database, estimate tokens from cost (fallback)
			// GPT-5-mini pricing: $0.25 per 1M input tokens, $2.00 per 1M output tokens
			// Average (assuming 50/50 input/output): ~$1.125 per 1M tokens = $0.000001125 per token
			// Using ~$0.000001 per token for simplicity (slightly conservative)
			tokensUsed = int64(tc.CostUsed / 0.000001)
		} else if tc.AICallsUsed > 0 {
			// No cost or tokens tracked, estimate from AI calls (fallback)
			// Estimate: ~2000 tokens per AI call on average (1000 input + 1000 output)
			// Cost per call: (1000 * $0.00000025) + (1000 * $0.000002) ≈ $0.00225
			tokensUsed = tc.AICallsUsed * 2000
			// Estimate cost from tokens: ~$0.000001125 per token (GPT-5-mini average)
			estimatedCost = float64(tokensUsed) * 0.000001125
			ctx.Logger.Debug("estimating cost from AI calls",
				zap.Stringer("tenant_id", tc.TenantID),
				zap.Int64("ai_calls", tc.AICallsUsed),
				zap.Int64("estimated_tokens", tokensUsed),
				zap.Float64("estimated_cost", estimatedCost))
		}

		totalCost += estimatedCost
		totalTokens += tokensUsed
		totalAICalls += tc.AICallsUsed

		// Calculate WhatsApp cost for this tenant
		// Try to use actual Gupshup cost first, fall back to estimated cost
		whatsappMessages := whatsappPerTenant[tc.TenantID]
		var whatsappCost float64
		if actualCost, hasActual := actualWhatsAppCosts[tc.TenantID]; hasActual && actualCost > 0 {
			// Use actual cost from Gupshup
			whatsappCost = actualCost
			// Use actual message count if available
			if actualMsgCount, hasActualMsg := actualWhatsAppMessages[tc.TenantID]; hasActualMsg && actualMsgCount > 0 {
				whatsappMessages = actualMsgCount
			}
			ctx.Logger.Debug("using actual Gupshup cost for tenant",
				zap.Stringer("tenant_id", tc.TenantID),
				zap.Float64("actual_cost", actualCost),
				zap.Int64("messages", whatsappMessages))
		} else {
			// Fall back to estimated cost: ~$0.0075 per message
			whatsappCost = float64(whatsappMessages) * 0.0075
		}
		totalWhatsAppCost += whatsappCost

		// Calculate total cost (AI + WhatsApp) for this tenant
		tenantTotalCost := estimatedCost + whatsappCost

		data.CostPerTenant = append(data.CostPerTenant, TenantCost{
			TenantID:         tc.TenantID,
			TenantName:       tc.TenantName,
			Cost:             estimatedCost, // AI cost only
			TokensUsed:       tokensUsed,
			WhatsAppCost:     whatsappCost,
			WhatsAppMessages: whatsappMessages,
			TotalCost:        tenantTotalCost, // AI + WhatsApp combined
		})
	}

	// If we still have no cost but have AI calls, calculate from total calls
	if totalCost == 0 && totalAICalls > 0 {
		ctx.Logger.Info("no cost tracked but AI calls found, estimating total cost",
			zap.Int64("total_calls", totalAICalls),
			zap.String("period", period))
		totalTokens = totalAICalls * 2000
		// GPT-5-mini: ~$0.000001125 per token average
		totalCost = float64(totalTokens) * 0.000001125
	}

	// Sort tenants by total cost (highest first) before selecting top 10
	sort.Slice(data.CostPerTenant, func(i, j int) bool {
		return data.CostPerTenant[i].TotalCost > data.CostPerTenant[j].TotalCost
	})

	// Top 10 cost tenants (sorted by total cost)
	if len(data.CostPerTenant) > 10 {
		data.TopCostTenants = data.CostPerTenant[:10]
	} else {
		data.TopCostTenants = data.CostPerTenant
	}

	// Set estimated AI cost (use calculated totalCost)
	data.EstimatedAICost = totalCost

	// Log final cost calculation for debugging
	ctx.Logger.Info("cost calculation summary",
		zap.String("period", period),
		zap.Int("tenant_count", len(tenantCosts)),
		zap.Int64("total_ai_calls", totalAICalls),
		zap.Int64("total_tokens", totalTokens),
		zap.Float64("estimated_ai_cost", totalCost))

	// WhatsApp messages sent (total count - use aggregated totalWhatsAppCost or count)
	var whatsappCount int64
	for _, count := range whatsappPerTenant {
		whatsappCount += count
	}
	data.WhatsAppMessagesSent = whatsappCount

	// Calculate total WhatsApp cost (sum of all tenant WhatsApp costs, or fallback calculation)
	// If we have actual costs, use them; otherwise fall back to estimated
	if totalWhatsAppCost > 0 {
		data.WhatsAppCost = totalWhatsAppCost
	} else {
		// Fallback: calculate from total count if per-tenant calculation didn't work
		// Try to get actual total cost from GupshupUsage if available
		if ctx.UsageSyncService != nil {
			fromDateStr := since.Format("2006-01-02")
			toDateStr := unti.Format("2006-01-02")
			var totalActualCost float64
			if err := ctx.DB.WithContext(reqCtx).
				Model(&storage.GupshupUsage{}).
				Select("COALESCE(SUM(total_fees), 0)").
				Where("date >= ? AND date <= ?", fromDateStr, toDateStr).
				Scan(&totalActualCost).Error; err == nil && totalActualCost > 0 {
				data.WhatsAppCost = totalActualCost
				ctx.Logger.Info("using actual total Gupshup cost",
					zap.Float64("actual_cost", totalActualCost),
					zap.String("from_date", fromDateStr),
					zap.String("to_date", toDateStr))
			} else {
				// Final fallback: estimate from message count
				data.WhatsAppCost = float64(whatsappCount) * 0.0075
			}
		} else {
			// No sync service available, use estimated cost
			data.WhatsAppCost = float64(whatsappCount) * 0.0075
		}
	}

	// OpenAI tokens used - use calculated total or estimate from cost
	// If we have tokens but no cost, estimate cost from tokens
	// If we have cost but no tokens, estimate tokens from cost
	if totalTokens > 0 {
		data.OpenAITokensUsed = totalTokens
		// If we have tokens but cost is still 0, estimate cost from tokens
		if totalCost == 0 {
			// GPT-5-mini: ~$0.000001125 per token average
			estimatedCostFromTokens := float64(totalTokens) * 0.000001125
			data.EstimatedAICost = estimatedCostFromTokens
			totalCost = estimatedCostFromTokens
			ctx.Logger.Info("estimated cost from tokens",
				zap.Int64("tokens", totalTokens),
				zap.Float64("estimated_cost", estimatedCostFromTokens))
		}
	} else if totalCost > 0 {
		// Estimate tokens from cost: ~$0.000001125 per token (GPT-5-mini average)
		data.OpenAITokensUsed = int64(totalCost / 0.000001125)
	} else if len(tenantCosts) > 0 {
		// If we have tenants with AI calls but no cost/token data, estimate from calls
		var totalCalls int64
		for _, tc := range tenantCosts {
			totalCalls += tc.AICallsUsed
		}
		// Estimate ~2000 tokens per call
		data.OpenAITokensUsed = totalCalls * 2000
		// Also estimate cost from this using GPT-5-mini pricing
		if totalCost == 0 && data.OpenAITokensUsed > 0 {
			data.EstimatedAICost = float64(data.OpenAITokensUsed) * 0.000001125
			totalCost = data.EstimatedAICost
		}
	}

	// Calculate total cost: AI costs + WhatsApp costs (after all estimates are done)
	data.TotalCost = data.EstimatedAICost + data.WhatsAppCost

	// Add currency conversion rates (to USD as base)
	data.CurrencyRates = currencyRates

	// Generate cost alerts
	data.Alerts = generateCostAlerts(ctx, data, since, reqCtx)

	return data, nil
}

// generateCostAlerts generates cost-related alerts
func generateCostAlerts(ctx *AdminContext, data *CostControlData, since time.Time, reqCtx context.Context) []CostAlert {
	alerts := []CostAlert{}

	// Check for cost spikes (day-over-day comparison)
	yesterday := since.Add(-24 * time.Hour)
	var yesterdayCost float64
	if err := ctx.DB.WithContext(reqCtx).
		Model(&storage.TenantQuota{}).
		Select("COALESCE(SUM(cost_used), 0)").
		Where("updated_at >= ? AND updated_at < ?", yesterday, since).
		Scan(&yesterdayCost).Error; err != nil {
		ctx.Logger.Debug("failed to calculate yesterday cost", zap.Error(err))
		yesterdayCost = 0
	}

	if yesterdayCost > 0 && data.EstimatedAICost > 0 {
		increase := (data.EstimatedAICost - yesterdayCost) / yesterdayCost * 100
		if increase > 50 {
			alerts = append(alerts, CostAlert{
				Type:    "spike",
				Message: fmt.Sprintf("Cost increased by %.1f%% compared to previous period", increase),
			})
		}
	}

	// Check for tenants exceeding thresholds
	for _, tc := range data.TopCostTenants {
		var quota storage.TenantQuota
		if err := ctx.DB.WithContext(reqCtx).
			Where("tenant_id = ?", tc.TenantID).
			First(&quota).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				ctx.Logger.Debug("failed to fetch quota for tenant", zap.Stringer("tenant_id", tc.TenantID), zap.Error(err))
			}
			continue
		}
		if quota.CostLimit > 0 && tc.Cost >= quota.CostLimit {
			alerts = append(alerts, CostAlert{
				Type:     "threshold",
				Message:  fmt.Sprintf("Tenant %s exceeded cost limit (%.2f / %.2f)", tc.TenantName, tc.Cost, quota.CostLimit),
				TenantID: &tc.TenantID,
			})
		}
	}

	// Check for outliers (tenants with unusually high cost)
	if len(data.CostPerTenant) > 0 {
		avgCost := data.EstimatedAICost / float64(len(data.CostPerTenant))
		for _, tc := range data.CostPerTenant {
			if avgCost > 0 && tc.Cost > avgCost*3 {
				alerts = append(alerts, CostAlert{
					Type:     "outlier",
					Message:  fmt.Sprintf("Tenant %s has unusually high cost (%.2f vs avg %.2f)", tc.TenantName, tc.Cost, avgCost),
					TenantID: &tc.TenantID,
				})
			}
		}
	}

	return alerts
}

// FilterCostDataForRole filters cost data for non-founder roles
// Non-founders see percentages and trends, not dollar amounts
func FilterCostDataForRole(data *CostControlData, role string) *CostControlData {
	if role == RoleSuperAdmin {
		return data // Full access
	}

	// For other roles, redact dollar amounts but keep relative data
	filtered := &CostControlData{
		OpenAITokensUsed:     data.OpenAITokensUsed,
		EstimatedAICost:      0, // Hide actual cost
		WhatsAppMessagesSent: data.WhatsAppMessagesSent,
		WhatsAppCost:         0, // Hide WhatsApp cost
		TotalCost:            0, // Hide total cost
		CostPerTenant:        []TenantCost{},
		TopCostTenants:       []TenantCost{},
		Alerts:               []CostAlert{}, // Hide alerts for non-founders
	}

	// Show relative cost percentages instead of dollar amounts
	totalCost := data.EstimatedAICost
	if totalCost > 0 {
		for _, tc := range data.CostPerTenant {
			percentage := (tc.Cost / totalCost) * 100
			filtered.CostPerTenant = append(filtered.CostPerTenant, TenantCost{
				TenantID:   tc.TenantID,
				TenantName: tc.TenantName,
				Cost:       percentage, // Store as percentage instead
				TokensUsed: 0,
			})
		}
	}

	// Top 10 by percentage
	if len(filtered.CostPerTenant) > 10 {
		filtered.TopCostTenants = filtered.CostPerTenant[:10]
	} else {
		filtered.TopCostTenants = filtered.CostPerTenant
	}

	return filtered
}
