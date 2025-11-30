package analytics

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AnalyticsScheduler handles periodic analytics aggregation jobs
type AnalyticsScheduler struct {
	DB              *gorm.DB
	Logger          *zap.Logger
	AnalyticsService *Service
}

// NewAnalyticsScheduler creates a new analytics scheduler
func NewAnalyticsScheduler(db *gorm.DB, logger *zap.Logger, service *Service) *AnalyticsScheduler {
	return &AnalyticsScheduler{
		DB:              db,
		Logger:          logger,
		AnalyticsService: service,
	}
}

// Run starts the analytics scheduler with periodic jobs
func (s *AnalyticsScheduler) Run(ctx context.Context) {
	// Daily snapshot job - runs at midnight UTC
	go s.runDailySnapshotJob(ctx)
	
	// Weekly campaign aggregation - runs every Sunday at 2 AM UTC
	go s.runWeeklyCampaignJob(ctx)
	
	// Weekly audience segments - runs every Sunday at 3 AM UTC
	go s.runWeeklyAudienceJob(ctx)
	
	s.Logger.Info("analytics scheduler started")
}

// runDailySnapshotJob runs daily platform metrics snapshot
func (s *AnalyticsScheduler) runDailySnapshotJob(ctx context.Context) {
	// Calculate time until next midnight UTC
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	initialDelay := midnight.Sub(now)
	
	// Wait until first midnight
	timer := time.NewTimer(initialDelay)
	select {
	case <-ctx.Done():
		timer.Stop()
		return
	case <-timer.C:
		// Run immediately on first tick
		s.generateDailySnapshots(ctx)
	}
	
	// Then run every 24 hours
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.generateDailySnapshots(ctx)
		}
	}
}

// runWeeklyCampaignJob runs weekly campaign aggregation
func (s *AnalyticsScheduler) runWeeklyCampaignJob(ctx context.Context) {
	// Calculate time until next Sunday 2 AM UTC
	now := time.Now().UTC()
	daysUntilSunday := int(time.Sunday - now.Weekday())
	if daysUntilSunday < 0 {
		daysUntilSunday += 7
	}
	if daysUntilSunday == 0 && now.Hour() >= 2 {
		// If it's Sunday but past 2 AM, schedule for next Sunday
		daysUntilSunday = 7
	}
	
	nextSunday := time.Date(now.Year(), now.Month(), now.Day()+daysUntilSunday, 2, 0, 0, 0, time.UTC)
	initialDelay := nextSunday.Sub(now)
	
	// Wait until first Sunday 2 AM
	timer := time.NewTimer(initialDelay)
	select {
	case <-ctx.Done():
		timer.Stop()
		return
	case <-timer.C:
		// Run immediately on first tick
		s.aggregateCampaignsWeekly(ctx)
	}
	
	// Then run every week
	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.aggregateCampaignsWeekly(ctx)
		}
	}
}

// runWeeklyAudienceJob runs weekly audience segment population
func (s *AnalyticsScheduler) runWeeklyAudienceJob(ctx context.Context) {
	// Calculate time until next Sunday 3 AM UTC
	now := time.Now().UTC()
	daysUntilSunday := int(time.Sunday - now.Weekday())
	if daysUntilSunday < 0 {
		daysUntilSunday += 7
	}
	if daysUntilSunday == 0 && now.Hour() >= 3 {
		// If it's Sunday but past 3 AM, schedule for next Sunday
		daysUntilSunday = 7
	}
	
	nextSunday := time.Date(now.Year(), now.Month(), now.Day()+daysUntilSunday, 3, 0, 0, 0, time.UTC)
	initialDelay := nextSunday.Sub(now)
	
	// Wait until first Sunday 3 AM
	timer := time.NewTimer(initialDelay)
	select {
	case <-ctx.Done():
		timer.Stop()
		return
	case <-timer.C:
		// Run immediately on first tick
		s.populateAudienceSegmentsWeekly(ctx)
	}
	
	// Then run every week
	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.populateAudienceSegmentsWeekly(ctx)
		}
	}
}

// generateDailySnapshots generates platform metrics snapshots for all active tenants
func (s *AnalyticsScheduler) generateDailySnapshots(ctx context.Context) {
	snapshotDate := time.Now().UTC().AddDate(0, 0, -1) // Yesterday's snapshot
	
	// Get all active tenants
	var tenantIDs []uint
	if err := s.DB.Model(&struct {
		ID     uint `gorm:"primaryKey"`
		Status string
	}{}).Table("tenants").Where("status = ?", "active").Pluck("id", &tenantIDs).Error; err != nil {
		s.Logger.Error("failed to fetch active tenants for daily snapshot", zap.Error(err))
		return
	}
	
	for _, tenantID := range tenantIDs {
		if err := s.AnalyticsService.GeneratePlatformMetricsSnapshot(ctx, tenantID, snapshotDate, "daily"); err != nil {
			s.Logger.Warn("failed to generate daily snapshot",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
	}
	
	s.Logger.Info("daily snapshots generated", zap.Int("tenant_count", len(tenantIDs)))
}

// aggregateCampaignsWeekly aggregates campaign analytics for all active tenants
func (s *AnalyticsScheduler) aggregateCampaignsWeekly(ctx context.Context) {
	endDate := time.Now().UTC()
	startDate := endDate.AddDate(0, 0, -7) // Last 7 days
	
	// Get all active tenants
	var tenantIDs []uint
	if err := s.DB.Model(&struct {
		ID     uint `gorm:"primaryKey"`
		Status string
	}{}).Table("tenants").Where("status = ?", "active").Pluck("id", &tenantIDs).Error; err != nil {
		s.Logger.Error("failed to fetch active tenants for campaign aggregation", zap.Error(err))
		return
	}
	
	for _, tenantID := range tenantIDs {
		if err := s.AnalyticsService.AggregateCampaignAnalytics(ctx, tenantID, startDate, endDate); err != nil {
			s.Logger.Warn("failed to aggregate campaign analytics",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
	}
	
	s.Logger.Info("weekly campaign aggregation completed", zap.Int("tenant_count", len(tenantIDs)))
}

// populateAudienceSegmentsWeekly populates audience segments for all active tenants
func (s *AnalyticsScheduler) populateAudienceSegmentsWeekly(ctx context.Context) {
	endDate := time.Now().UTC()
	startDate := endDate.AddDate(0, 0, -7) // Last 7 days
	
	// Get all active tenants
	var tenantIDs []uint
	if err := s.DB.Model(&struct {
		ID     uint `gorm:"primaryKey"`
		Status string
	}{}).Table("tenants").Where("status = ?", "active").Pluck("id", &tenantIDs).Error; err != nil {
		s.Logger.Error("failed to fetch active tenants for audience segments", zap.Error(err))
		return
	}
	
	for _, tenantID := range tenantIDs {
		if err := s.AnalyticsService.PopulateAudienceSegments(ctx, tenantID, startDate, endDate); err != nil {
			s.Logger.Warn("failed to populate audience segments",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
	}
	
	s.Logger.Info("weekly audience segments populated", zap.Int("tenant_count", len(tenantIDs)))
}

