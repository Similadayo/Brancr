package core

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm/logger"
)

// loggerAdapter adapts zap logger to GORM logger interface
type loggerAdapter struct {
	logger        *zap.Logger
	slowThreshold time.Duration
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

	fields := []zap.Field{
		zap.String("sql", sql),
		zap.Int64("rows", rows),
		zap.Duration("duration", elapsed),
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		l.logger.Error("database query error", fields...)
		return
	}

	// Log slow queries
	if l.slowThreshold > 0 && elapsed > l.slowThreshold {
		l.logger.Warn("slow database query",
			append(fields, zap.Duration("threshold", l.slowThreshold))...)
		return
	}

	// Log all queries in development (when slowThreshold is 0)
	if l.slowThreshold == 0 {
		l.logger.Debug("database query", fields...)
	}
}

