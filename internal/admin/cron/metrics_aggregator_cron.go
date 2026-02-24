package cron

import (
	"context"
	"time"

	"github.com/similadayo/brancr/internal/admin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// StartMetricsAggregationCron starts the background cron job for metrics aggregation
func StartMetricsAggregationCron(ctx context.Context, db *gorm.DB, logger *zap.Logger) {
	aggregator := admin.NewMetricsAggregator(db, logger)

	// Hourly aggregation - run every hour at the top of the hour
	go func() {
		// Wait until the next hour boundary
		now := time.Now()
		nextHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
		initialDelay := nextHour.Sub(now)
		timer := time.NewTimer(initialDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		// Run immediately after the first hour boundary
		hourStart := time.Now().Truncate(time.Hour)
		if err := aggregator.RunHourlyAggregation(ctx, hourStart); err != nil {
			logger.Error("initial hourly metrics aggregation failed", zap.Error(err))
		} else {
			logger.Info("✅ Initial hourly metrics aggregation completed")
		}

		// Then run every hour
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hourStart := time.Now().Truncate(time.Hour)
				if err := aggregator.RunHourlyAggregation(ctx, hourStart); err != nil {
					logger.Error("hourly metrics aggregation failed", zap.Error(err))
				} else {
					logger.Debug("✅ Hourly metrics aggregation completed", zap.Time("hour_start", hourStart))
				}
			}
		}
	}()

	// Daily aggregation - run daily at midnight UTC
	go func() {
		// Calculate time until next midnight UTC
		now := time.Now().UTC()
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		initialDelay := nextMidnight.Sub(now)
		timer := time.NewTimer(initialDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		// Run immediately after midnight
		date := time.Now().UTC().Truncate(24 * time.Hour)
		if err := aggregator.RunDailyAggregation(ctx, date); err != nil {
			logger.Error("initial daily metrics aggregation failed", zap.Error(err))
		} else {
			logger.Info("✅ Initial daily metrics aggregation completed")
		}

		// Then run daily at midnight UTC
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				date := time.Now().UTC().Truncate(24 * time.Hour)
				if err := aggregator.RunDailyAggregation(ctx, date); err != nil {
					logger.Error("daily metrics aggregation failed", zap.Error(err))
				} else {
					logger.Info("✅ Daily metrics aggregation completed", zap.Time("date", date))
				}
			}
		}
	}()

	logger.Info("📊 Metrics aggregation cron jobs started (hourly + daily)")
}
