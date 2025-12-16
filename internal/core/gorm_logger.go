package core

import (
	"context"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm/logger"
)

// loggerAdapter adapts zap logger to GORM logger interface
type loggerAdapter struct {
	logger        *zap.Logger
	slowThreshold time.Duration
	// Metrics
	queryCount     *int64
	slowQueryCount *int64
	errorCount     *int64
	totalDuration  *int64
}

// LogMode implements gorm.Logger interface
func (l loggerAdapter) LogMode(level logger.LogLevel) logger.Interface {
	return l
}

// Info implements gorm.Logger interface
func (l loggerAdapter) Info(ctx context.Context, msg string, data ...interface{}) {
	l.logger.Info(msg, zap.Any("data", data))
}

// Warn implements gorm.Logger interface
func (l loggerAdapter) Warn(ctx context.Context, msg string, data ...interface{}) {
	l.logger.Warn(msg, zap.Any("data", data))
}

// Error implements gorm.Logger interface
func (l loggerAdapter) Error(ctx context.Context, msg string, data ...interface{}) {
	l.logger.Error(msg, zap.Any("data", data))
}

// Trace implements gorm.Logger interface - logs queries
func (l loggerAdapter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()

	// Update metrics
	if l.queryCount != nil {
		atomic.AddInt64(l.queryCount, 1)
		atomic.AddInt64(l.totalDuration, int64(elapsed))
	}

	fields := []zap.Field{
		zap.String("sql", sql),
		zap.Int64("rows", rows),
		zap.Duration("duration", elapsed),
	}

	if err != nil {
		if l.errorCount != nil {
			atomic.AddInt64(l.errorCount, 1)
		}
		fields = append(fields, zap.Error(err))
		l.logger.Error("database query error", fields...)
		return
	}

	// Log slow queries
	if l.slowThreshold > 0 && elapsed > l.slowThreshold {
		if l.slowQueryCount != nil {
			atomic.AddInt64(l.slowQueryCount, 1)
		}
		l.logger.Warn("slow database query",
			append(fields, zap.Duration("threshold", l.slowThreshold))...)
		return
	}

	// Log all queries in development (when slowThreshold is 0)
	if l.slowThreshold == 0 {
		l.logger.Debug("database query", fields...)
	}
}

// Stats returns query statistics
func (l loggerAdapter) Stats() QueryStats {
	var stats QueryStats
	if l.queryCount != nil {
		stats.QueryCount = atomic.LoadInt64(l.queryCount)
		stats.SlowQueryCount = atomic.LoadInt64(l.slowQueryCount)
		stats.ErrorCount = atomic.LoadInt64(l.errorCount)
		totalDur := atomic.LoadInt64(l.totalDuration)
		if stats.QueryCount > 0 {
			stats.AvgDuration = time.Duration(totalDur / stats.QueryCount)
		}
	}
	return stats
}

// QueryStats contains database query statistics
type QueryStats struct {
	QueryCount     int64
	SlowQueryCount int64
	ErrorCount     int64
	AvgDuration    time.Duration
}
