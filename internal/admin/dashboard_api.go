package admin

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/quota"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

type DashboardResponse struct {
	Tenants      []TenantSummary     `json:"tenants"`
	Onboarding   OnboardingSummary   `json:"onboarding"`
	Integrations []IntegrationStatus `json:"integrations"`
	Messaging    MessagingSummary    `json:"messaging"`
	Billing      BillingSummary      `json:"billing"`
	Audit        []string            `json:"audit"`
	Suspensions  []string            `json:"suspensions"`
	Escalations  EscalationsSummary  `json:"escalations"`
}

type TenantSummary struct {
	ID                 uint     `json:"id"`
	Name               string   `json:"name"`
	Email              string   `json:"email"`
	PlanType           string   `json:"plan_type"`
	Status             string   `json:"status"`
	OnboardingStep     string   `json:"onboarding_step"`
	OnboardingComplete bool     `json:"onboarding_complete"`
	Platforms          []string `json:"platforms"`
	LastActivity       string   `json:"last_activity"`
	UsagePercent       float64  `json:"usage_percent"`
}

type OnboardingSummary struct {
	CompletionRate      float64          `json:"completion_rate"`
	DropOffs            []DropOff        `json:"drop_offs"`
	ManualInterventions int              `json:"manual_interventions"`
	StepTotals          map[string]int64 `json:"step_totals"`
}

type DropOff struct {
	Step    string  `json:"step"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type IntegrationStatus struct {
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	ErrorRate   float64 `json:"error_rate"`
	Connected   int64   `json:"connected"`
	Total       int64   `json:"total"`
	ExpiresSoon int64   `json:"expires_soon"`
}

type MessagingSummary struct {
	PlatformVolumes  []PlatformVolume `json:"platform_volumes"`
	AiPercent        float64          `json:"ai_percent"`
	HumanPercent     float64          `json:"human_percent"`
	FailedDeliveries int64            `json:"failed_deliveries"`
	LatencySeconds   float64          `json:"latency_seconds"`
}

type PlatformVolume struct {
	Platform string `json:"platform"`
	Count    int64  `json:"count"`
}

type BillingSummary struct {
	Usage          []BillingUsage `json:"usage"`
	OverageWatch   string         `json:"overage_watch"`
	FailedPayments int64          `json:"failed_payments"`
	CreditsApplied int64          `json:"credits_applied"`
}

type BillingUsage struct {
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
	Tenant  string  `json:"tenant"`
}

type EscalationsSummary struct {
	Provider   []string `json:"provider"`
	Compliance []string `json:"compliance"`
	Billing    []string `json:"billing"`
}

type actionResponse struct {
	Status string `json:"status"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type tokenRefreshRequest struct {
	Force bool `json:"force"`
}

func AdminDashboardDataHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		since24h := now.Add(-24 * time.Hour)
		resp := DashboardResponse{
			Tenants:      fetchTenantSummaries(ctx, r),
			Onboarding:   buildOnboardingSummary(ctx),
			Integrations: buildIntegrationStatuses(ctx, since24h),
			Messaging:    buildMessagingSummary(ctx, since24h),
			Billing:      buildBillingSummary(ctx, since24h),
			Audit:        fetchAuditEntries(ctx),
			Suspensions:  fetchSuspensionEntries(ctx),
			Escalations:  buildEscalationsSummary(ctx, since24h),
		}
		respondJSON(w, resp)
	}
}

func fetchTenantSummaries(ctx *AdminContext, r *http.Request) []TenantSummary {
	var tenants []storage.Tenant
	if err := ctx.DB.Order("updated_at desc").Limit(8).Find(&tenants).Error; err != nil {
		ctx.Logger.Warn("failed to load tenants", zap.Error(err))
		return nil
	}

	if len(tenants) == 0 {
		return nil
	}

	ids := make([]uint, 0, len(tenants))
	for _, t := range tenants {
		ids = append(ids, t.ID)
	}

	var accounts []storage.SocialAccount
	if err := ctx.DB.Where("tenant_id IN ?", ids).Find(&accounts).Error; err != nil {
		ctx.Logger.Warn("failed to load social accounts", zap.Error(err))
	}

	platformsByTenant := map[uint]map[string]struct{}{}
	for _, acct := range accounts {
		set := platformsByTenant[acct.TenantID]
		if set == nil {
			set = map[string]struct{}{}
			platformsByTenant[acct.TenantID] = set
		}
		set[strings.Title(acct.Platform)] = struct{}{}
	}

	type activityRow struct {
		TenantID uint
		LastAt   time.Time
	}
	var activityRows []activityRow
	if err := ctx.DB.Model(&storage.Interaction{}).
		Select("tenant_id, MAX(created_at) AS last_at").
		Where("tenant_id IN ?", ids).
		Group("tenant_id").
		Scan(&activityRows).Error; err != nil {
		ctx.Logger.Warn("failed to compute last activity", zap.Error(err))
	}

	lastActivity := map[uint]time.Time{}
	for _, row := range activityRows {
		lastActivity[row.TenantID] = row.LastAt
	}

	quotaSvc := quota.NewQuotaService(ctx.DB, ctx.Logger, nil)
	summaries := make([]TenantSummary, 0, len(tenants))
	for _, tenant := range tenants {
		set := platformsByTenant[tenant.ID]
		platforms := make([]string, 0, len(set))
		for name := range set {
			platforms = append(platforms, name)
		}
		sort.Strings(platforms)

		usagePercent := 0.0
		if quotaData, err := quotaSvc.GetQuotaUsage(r.Context(), tenant.ID); err == nil && quotaData != nil {
			usagePercent = maxFloat(
				percentOf(quotaData.AICallsUsed, quotaData.AICallsLimit),
				percentOf(quotaData.PostsUsed, quotaData.PostsLimit),
				percentOf(quotaData.StorageUsedMB, quotaData.StorageLimitMB),
				percentOf(quotaData.APICallsUsed, quotaData.APICallsLimit),
			)
		} else if err != nil {
			ctx.Logger.Warn("failed to load quota for tenant", zap.Uint("tenant_id", tenant.ID), zap.Error(err))
		}

		last := lastActivity[tenant.ID]
		lastText := ""
		if !last.IsZero() {
			lastText = last.Format(time.RFC3339)
		}

		summaries = append(summaries, TenantSummary{
			ID:                 tenant.ID,
			Name:               tenant.Name,
			Email:              tenant.Email,
			PlanType:           tenant.PlanType,
			Status:             tenant.Status,
			OnboardingStep:     tenant.OnboardingStep,
			OnboardingComplete: tenant.OnboardingComplete,
			Platforms:          platforms,
			LastActivity:       lastText,
			UsagePercent:       round(usagePercent, 1),
		})
	}

	return summaries
}

func buildOnboardingSummary(ctx *AdminContext) OnboardingSummary {
	var total int64
	ctx.DB.Model(&storage.Tenant{}).Count(&total)

	var completed int64
	ctx.DB.Model(&storage.Tenant{}).
		Where("onboarding_complete = ?", true).
		Count(&completed)

	rows := make([]struct {
		Step  string
		Count int64
	}, 0)
	ctx.DB.Table("tenants").
		Select("onboarding_step AS step, COUNT(*) AS count").
		Group("onboarding_step").
		Scan(&rows)

	stepMap := map[string]int64{}
	dropOffs := make([]DropOff, 0)
	for _, row := range rows {
		stepMap[row.Step] = row.Count
		if strings.EqualFold(row.Step, "complete") {
			continue
		}
		percent := 0.0
		if total > 0 {
			percent = float64(row.Count) / float64(total) * 100
		}
		dropOffs = append(dropOffs, DropOff{
			Step:    strings.Title(row.Step),
			Count:   row.Count,
			Percent: round(percent, 1),
		})
	}
	sort.Slice(dropOffs, func(i, j int) bool {
		return dropOffs[i].Count > dropOffs[j].Count
	})

	var interventions int64
	ctx.DB.Model(&storage.AdminCase{}).Where("closed_at IS NULL").Count(&interventions)

	completionRate := 0.0
	if total > 0 {
		completionRate = float64(completed) / float64(total) * 100
	}

	return OnboardingSummary{
		CompletionRate:      round(completionRate, 1),
		DropOffs:            dropOffs,
		ManualInterventions: int(interventions),
		StepTotals:          stepMap,
	}
}

func buildIntegrationStatuses(ctx *AdminContext, since time.Time) []IntegrationStatus {
	definitions := []struct {
		Name        string
		Platforms   []string
		LogKeywords []string
	}{
		{"Meta APIs", []string{"instagram", "facebook"}, []string{"meta", "instagram", "facebook"}},
		{"WhatsApp", []string{"whatsapp"}, []string{"whatsapp"}},
		{"TikTok API", []string{"tiktok"}, []string{"tiktok"}},
		{"Billing Gateway", nil, []string{"payment", "billing", "stripe", "paystack"}},
	}

	results := make([]IntegrationStatus, 0, len(definitions))
	for _, def := range definitions {
		connected, total := countSocialAccounts(ctx, def.Platforms)
		if def.Name == "Billing Gateway" {
			total = countPaymentAccounts(ctx)
			connected = countPaymentAccountsActive(ctx)
		}

		errCount, totalLogs := countLogEntries(ctx, since, def.LogKeywords)
		errorRate := 0.0
		if totalLogs > 0 {
			errorRate = float64(errCount) / float64(totalLogs) * 100
		}

		status := "Stable"
		if errorRate > 1.5 || (total > 0 && connected < total/2) {
			status = "Warning"
		}

		results = append(results, IntegrationStatus{
			Name:        def.Name,
			Status:      status,
			ErrorRate:   round(errorRate, 1),
			Connected:   connected,
			Total:       total,
			ExpiresSoon: countExpiringAccounts(ctx, def.Platforms),
		})
	}
	return results
}

func countSocialAccounts(ctx *AdminContext, platforms []string) (int64, int64) {
	if len(platforms) == 0 {
		return 0, 0
	}
	var total int64
	db := ctx.DB.Model(&storage.SocialAccount{}).Where("platform IN ?", platforms)
	db.Count(&total)

	var connected int64
	db.Where("connected = ?", true).Count(&connected)

	return connected, total
}

func countLogEntries(ctx *AdminContext, since time.Time, keywords []string) (int64, int64) {
	cond, args := buildSourceCondition(keywords)
	base := ctx.DB.Model(&storage.LogEntry{}).Where("created_at > ?", since)
	errorBase := base
	if cond != "" {
		base = base.Where(cond, args...)
		errorBase = errorBase.Where(cond, args...)
	}

	var total int64
	base.Count(&total)

	var errCount int64
	errorBase.Where("level = ?", "ERROR").Count(&errCount)

	return errCount, total
}

func buildSourceCondition(keys []string) (string, []interface{}) {
	if len(keys) == 0 {
		return "", nil
	}
	conds := make([]string, 0, len(keys))
	args := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		conds = append(conds, "source ILIKE ?")
		args = append(args, "%"+key+"%")
	}
	return strings.Join(conds, " OR "), args
}

func countExpiringAccounts(ctx *AdminContext, platforms []string) int64 {
	if len(platforms) == 0 {
		return 0
	}
	threshold := time.Now().Add(72 * time.Hour)
	var count int64
	ctx.DB.Model(&storage.SocialAccount{}).
		Where("platform IN ?", platforms).
		Where("expires_at IS NOT NULL AND expires_at < ?", threshold).
		Count(&count)
	return count
}

func countPaymentAccounts(ctx *AdminContext) int64 {
	var total int64
	ctx.DB.Model(&storage.TenantPaymentAccount{}).Count(&total)
	return total
}

func countPaymentAccountsActive(ctx *AdminContext) int64 {
	var active int64
	ctx.DB.Model(&storage.TenantPaymentAccount{}).Where("is_active = ?", true).Count(&active)
	return active
}

func buildMessagingSummary(ctx *AdminContext, since time.Time) MessagingSummary {
	var rows []struct {
		Platform string
		Count    int64
	}
	ctx.DB.Model(&storage.Interaction{}).
		Select("platform, COUNT(*) AS count").
		Where("created_at > ?", since).
		Group("platform").
		Scan(&rows)

	volumes := make([]PlatformVolume, 0, len(rows))
	for _, row := range rows {
		volumes = append(volumes, PlatformVolume{
			Platform: strings.Title(row.Platform),
			Count:    row.Count,
		})
	}

	var total int64
	ctx.DB.Model(&storage.Interaction{}).Where("created_at > ?", since).Count(&total)

	var aiCount int64
	ctx.DB.Model(&storage.Interaction{}).
		Where("created_at > ? AND response_type = ?", since, "auto_reply").
		Count(&aiCount)

	var humanCount int64
	ctx.DB.Model(&storage.Interaction{}).
		Where("created_at > ? AND response_type IN ?", since, []string{"manual", "escalated"}).
		Count(&humanCount)

	latency := averageLatency(ctx, since)

	var failedDeliveries int64
	ctx.DB.Model(&storage.EventLog{}).
		Where("event_type = ? AND created_at > ?", "delivery", since).
		Count(&failedDeliveries)

	return MessagingSummary{
		PlatformVolumes:  volumes,
		AiPercent:        round(percentage(aiCount, total), 1),
		HumanPercent:     round(percentage(humanCount, total), 1),
		FailedDeliveries: failedDeliveries,
		LatencySeconds:   round(latency, 1),
	}
}

func averageLatency(ctx *AdminContext, since time.Time) float64 {
	var interactions []storage.Interaction
	ctx.DB.Model(&storage.Interaction{}).
		Select("created_at, escalated_at").
		Where("created_at > ?", since).
		Order("created_at desc").
		Limit(500).
		Find(&interactions)

	total := 0.0
	count := 0
	for _, interaction := range interactions {
		if interaction.EscalatedAt == nil {
			continue
		}
		lat := interaction.EscalatedAt.Sub(interaction.CreatedAt).Seconds()
		if lat < 0 {
			continue
		}
		total += lat
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func buildBillingSummary(ctx *AdminContext, since time.Time) BillingSummary {
	var quotas []storage.TenantQuota
	ctx.DB.Order("ai_calls_used desc").Limit(3).Find(&quotas)

	tenantIDs := make([]uint, 0, len(quotas))
	for _, quota := range quotas {
		tenantIDs = append(tenantIDs, quota.TenantID)
	}

	var tenants []storage.Tenant
	if len(tenantIDs) > 0 {
		ctx.DB.Where("id IN ?", tenantIDs).Find(&tenants)
	}
	nameMap := map[uint]string{}
	for _, tenant := range tenants {
		nameMap[tenant.ID] = tenant.Name
	}

	usage := make([]BillingUsage, 0, len(quotas))
	for _, quota := range quotas {
		percent := maxFloat(
			percentOf(quota.AICallsUsed, quota.AICallsLimit),
			percentOf(quota.PostsUsed, quota.PostsLimit),
			percentOf(quota.StorageUsedMB, quota.StorageLimitMB),
			percentOf(quota.APICallsUsed, quota.APICallsLimit),
		)
		usage = append(usage, BillingUsage{
			Name:    fmt.Sprintf("%s", nameMap[quota.TenantID]),
			Tenant:  nameMap[quota.TenantID],
			Percent: round(percent, 1),
		})
	}

	var overageCount int64
	ctx.DB.Model(&storage.TenantQuota{}).
		Where("(ai_calls_limit > 0 AND ai_calls_used >= ai_calls_limit) OR " +
			"(posts_limit > 0 AND posts_used >= posts_limit) OR " +
			"(storage_limit_mb > 0 AND storage_used_mb >= storage_limit_mb) OR " +
			"(api_calls_limit > 0 AND api_calls_used >= api_calls_limit)").
		Count(&overageCount)

	var failedPayments int64
	ctx.DB.Model(&storage.Payment{}).
		Where("status IN ? AND created_at > ?", []string{"failed", "disputed", "refunded"}, since.Add(-7*24*time.Hour)).
		Count(&failedPayments)

	var creditsApplied int64
	ctx.DB.Model(&storage.AdminAuditLog{}).
		Where("action LIKE ?", "%credit%").
		Count(&creditsApplied)

	overageWatch := "None"
	if overageCount > 0 {
		overageWatch = fmt.Sprintf("%d tenants exceeding limits", overageCount)
	}

	return BillingSummary{
		Usage:          usage,
		OverageWatch:   overageWatch,
		FailedPayments: failedPayments,
		CreditsApplied: creditsApplied,
	}
}

func fetchAuditEntries(ctx *AdminContext) []string {
	var logs []storage.AdminAuditLog
	ctx.DB.Order("created_at desc").Limit(5).Find(&logs)

	messages := make([]string, 0, len(logs))
	for _, log := range logs {
		meta := map[string]interface{}{}
		if log.Metadata != "" {
			_ = json.Unmarshal([]byte(log.Metadata), &meta)
		}
		messages = append(messages, fmt.Sprintf("%s • %s • %s", log.Action, log.AdminEmail, formatMetadata(meta)))
	}
	return messages
}

func fetchSuspensionEntries(ctx *AdminContext) []string {
	var logs []storage.AdminAuditLog
	ctx.DB.Order("created_at desc").Where("action = ?", "tenant.status_change").Limit(5).Find(&logs)

	entries := make([]string, 0, len(logs))
	for _, log := range logs {
		meta := map[string]interface{}{}
		_ = json.Unmarshal([]byte(log.Metadata), &meta)
		status := fmt.Sprintf("%v", meta["status"])
		tenantID := fmt.Sprintf("%v", meta["tenant_id"])
		entries = append(entries, fmt.Sprintf("Tenant #%s %s by %s", tenantID, status, log.AdminEmail))
	}
	return entries
}

func buildEscalationsSummary(ctx *AdminContext, since time.Time) EscalationsSummary {
	provider := fetchLogMessages(ctx, since, []string{"whatsapp", "meta", "tiktok"})
	compliance := fetchAdminCases(ctx)
	billing := fetchPaymentEscalations(ctx, since)

	return EscalationsSummary{
		Provider:   provider,
		Compliance: compliance,
		Billing:    billing,
	}
}

func fetchLogMessages(ctx *AdminContext, since time.Time, keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}
	cond, args := buildSourceCondition(keywords)
	query := ctx.DB.Model(&storage.LogEntry{}).
		Where("created_at > ?", since).
		Order("created_at desc").
		Limit(3)
	if cond != "" {
		query = query.Where(cond, args...)
	}

	var logs []storage.LogEntry
	query.Find(&logs)

	results := make([]string, 0, len(logs))
	for _, log := range logs {
		results = append(results, fmt.Sprintf("%s: %s", strings.Title(log.Source), truncate(log.Message, 60)))
	}
	if len(results) == 0 {
		results = append(results, "No provider escalations")
	}
	return results
}

func fetchAdminCases(ctx *AdminContext) []string {
	var cases []storage.AdminCase
	ctx.DB.Where("closed_at IS NULL").Order("created_at desc").Limit(3).Find(&cases)

	results := make([]string, 0, len(cases))
	for _, ac := range cases {
		results = append(results, fmt.Sprintf("Case #%d • %s • %s", ac.ID, ac.Status, ac.Reason))
	}
	if len(results) == 0 {
		results = append(results, "No active admin cases")
	}
	return results
}

func fetchPaymentEscalations(ctx *AdminContext, since time.Time) []string {
	var payments []storage.Payment
	ctx.DB.Where("status IN ? AND created_at > ?", []string{"failed", "disputed", "refunded"}, since.Add(-7*24*time.Hour)).
		Order("created_at desc").
		Limit(3).
		Find(&payments)

	results := make([]string, 0, len(payments))
	for _, payment := range payments {
		results = append(results, fmt.Sprintf("%s • %s • ₦%.2f", payment.PaymentReference, payment.Status, payment.Amount))
	}
	if len(results) == 0 {
		results = append(results, "No recent billing escalations")
	}
	return results
}

func formatMetadata(meta map[string]interface{}) string {
	if len(meta) == 0 {
		return "no metadata"
	}
	parts := make([]string, 0, len(meta))
	for k, v := range meta {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func percentage(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func percentOf(value, limit int) float64 {
	if limit <= 0 {
		return 0
	}
	return float64(value) / float64(limit) * 100
}

func maxFloat(values ...float64) float64 {
	max := 0.0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}

func round(value float64, decimals int) float64 {
	factor := math.Pow(10, float64(decimals))
	return math.Round(value*factor) / factor
}

func truncate(value string, length int) string {
	if len(value) <= length {
		return value
	}
	runes := []rune(value)
	if len(runes) <= length {
		return value
	}
	return string(runes[:length]) + "…"
}

func AdminTenantStatusAPIHandler(ctx *AdminContext) http.HandlerFunc {
	allowed := map[string]struct{}{"active": {}, "suspended": {}, "pending": {}}
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var payload statusRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		status := strings.ToLower(payload.Status)
		if _, ok := allowed[status]; !ok {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.status_change",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"status":    status,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).Where("id = ?", tenantID).Update("status", status).Error; err != nil {
			ctx.Logger.Warn("failed to update tenant status", zap.Uint("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to update status", http.StatusInternalServerError)
			return
		}

		respondJSON(w, actionResponse{Status: "ok"})
	}
}

func AdminTenantOnboardingResetHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var tenant storage.Tenant
		if err := ctx.DB.Select("onboarding_step").First(&tenant, tenantID).Error; err != nil {
			http.Error(w, "tenant not found", http.StatusNotFound)
			return
		}

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.onboarding_reset",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id":     tenantID,
				"previous_step": tenant.OnboardingStep,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		if err := ctx.DB.Model(&storage.Tenant{}).
			Where("id = ?", tenantID).
			Updates(map[string]interface{}{
				"onboarding_step":     "business_profile",
				"onboarding_complete": false,
			}).Error; err != nil {
			ctx.Logger.Warn("failed to reset onboarding", zap.Uint("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to reset onboarding", http.StatusInternalServerError)
			return
		}

		respondJSON(w, actionResponse{Status: "ok"})
	}
}

func AdminTenantTokenRefreshHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := parseTenantParam(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var payload tokenRefreshRequest
		_ = json.NewDecoder(r.Body).Decode(&payload) // payload optional

		entry := AdminAuditEntry{
			AdminEmail: AdminEmailFromRequest(r),
			AdminRole:  AdminRoleFromRequest(r),
			Action:     "tenant.tokens_refresh",
			TenantID:   &tenantID,
			Metadata: map[string]interface{}{
				"tenant_id": tenantID,
				"force":     payload.Force,
			},
		}
		if err := auditWriter(r.Context(), ctx, entry); err != nil {
			http.Error(w, "audit failed", http.StatusInternalServerError)
			return
		}

		newExpiry := time.Now().Add(72 * time.Hour)
		if err := ctx.DB.Model(&storage.SocialAccount{}).
			Where("tenant_id = ?", tenantID).
			Update("expires_at", newExpiry).Error; err != nil {
			ctx.Logger.Warn("failed to refresh tokens", zap.Uint("tenant_id", tenantID), zap.Error(err))
			http.Error(w, "failed to refresh tokens", http.StatusInternalServerError)
			return
		}

		respondJSON(w, actionResponse{Status: "ok"})
	}
}

func parseTenantParam(r *http.Request) (uint, error) {
	idStr := chi.URLParam(r, "id")
	id64, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id64 == 0 {
		return 0, fmt.Errorf("invalid tenant id")
	}
	return uint(id64), nil
}
