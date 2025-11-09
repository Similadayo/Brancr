package admin

import (
	"context"
	"time"
)

type AnalyticsStats struct {
	TotalTenants      int64
	ActiveTenants     int64
	EventsToday       int64
	PaymentsToday     int64
	DeliveriesToday   int64
	AppointmentsToday int64
	OpenRouterUsage   int64
	CloudinaryUsage   int64
}

func countEvents(ctx *AdminContext, since time.Time) int64 {
	type EventLog struct {
		ID        uint
		CreatedAt time.Time
	}
	var count int64
	ctx.DB.Model(&EventLog{}).Where("created_at > ?", since).Count(&count)
	return count
}

func countActiveBots(ctx *AdminContext) int64 {
	var count int64
	ctx.DB.Table("tenants").Where("status = ?", "Active").Count(&count)
	return count
}

func getOpenRouterUsage(ctx *AdminContext, since time.Time) int64 {
	if ctx.Redis == nil {
		return 0
	}

	val, err := ctx.Redis.Get(context.Background(), "openrouter_tokens_used").Int64()
	if err != nil {
		return 0
	}
	return val
}

func GetAnalyticsStats(ctx *AdminContext) AnalyticsStats {
	var s AnalyticsStats

	ctx.DB.Table("tenants").Count(&s.TotalTenants)
	ctx.DB.Table("tenants").Where("status = ?", "Active").Count(&s.ActiveTenants)

	// Event breakdown (last 24h)
	since := time.Now().Add(-24 * time.Hour)
	ctx.DB.Table("event_logs").Where("created_at > ?", since).Count(&s.EventsToday)
	ctx.DB.Table("event_logs").Where("event_type = 'payment' AND created_at > ?", since).Count(&s.PaymentsToday)
	ctx.DB.Table("event_logs").Where("event_type = 'delivery' AND created_at > ?", since).Count(&s.DeliveriesToday)
	ctx.DB.Table("event_logs").Where("event_type = 'appointment' AND created_at > ?", since).Count(&s.AppointmentsToday)

	// Mock integrations (replace later with actual API metrics)
	s.OpenRouterUsage = getOpenRouterUsage(ctx, since)
	s.CloudinaryUsage = 512 // placeholder MB used

	return s
}
