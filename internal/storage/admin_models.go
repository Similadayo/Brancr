package storage

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// AdminMetricHourly represents hourly aggregated metrics for the admin dashboard
type AdminMetricHourly struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	MetricType  string     `gorm:"type:varchar(50);not null;index:idx_metric_type_hour"`
	TenantID    *uuid.UUID `gorm:"index:idx_tenant"` // NULL for platform-wide metrics
	MetricKey   string     `gorm:"type:varchar(100);not null;index:idx_metric_type_hour"`
	MetricValue float64    `gorm:"type:numeric(15,4)"`
	Metadata    string     `gorm:"type:jsonb;default:'{}'"`
	HourStart   time.Time  `gorm:"not null;index:idx_metric_type_hour"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (AdminMetricHourly) TableName() string {
	return "admin_metrics_hourly"
}

// AdminMetricDaily represents daily aggregated metrics for the admin dashboard
type AdminMetricDaily struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	MetricType  string     `gorm:"type:varchar(50);not null;index:idx_metric_type_date"`
	TenantID    *uuid.UUID `gorm:"index:idx_tenant"` // NULL for platform-wide metrics
	MetricKey   string     `gorm:"type:varchar(100);not null;index:idx_metric_type_date"`
	MetricValue float64    `gorm:"type:numeric(15,4)"`
	Metadata    string     `gorm:"type:jsonb;default:'{}'"`
	Date        time.Time  `gorm:"type:date;not null;index:idx_metric_type_date"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (AdminMetricDaily) TableName() string {
	return "admin_metrics_daily"
}

// AdminQualitativeInsight represents qualitative insights, notes, and risk flags
type AdminQualitativeInsight struct {
	ID             uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	InsightType    string         `gorm:"type:varchar(50);not null;index:idx_type_created"`
	TenantID       *uuid.UUID     `gorm:"index:idx_tenant"`
	ConversationID *uuid.UUID     `gorm:"index:idx_conversation"`
	Content        string         `gorm:"type:text;not null"`
	AdminEmail     string         `gorm:"type:varchar(255);not null"`
	AdminRole      string         `gorm:"type:varchar(50);not null"`
	Tags           pq.StringArray `gorm:"type:text[]"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (AdminQualitativeInsight) TableName() string {
	return "admin_qualitative_insights"
}

func (a *AdminQualitativeInsight) BeforeCreate(tx *gorm.DB) error {
	if a.Tags == nil {
		a.Tags = pq.StringArray{}
	}
	return nil
}

func (a *AdminQualitativeInsight) BeforeUpdate(tx *gorm.DB) error {
	if a.Tags == nil {
		a.Tags = pq.StringArray{}
	}
	return nil
}
