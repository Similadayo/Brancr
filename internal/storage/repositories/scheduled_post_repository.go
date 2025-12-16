package repositories

import (
	"context"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// ScheduledPostRepository handles scheduled post operations
type ScheduledPostRepository struct {
	db *gorm.DB
}

// NewScheduledPostRepository creates a new scheduled post repository
func NewScheduledPostRepository(db *gorm.DB) *ScheduledPostRepository {
	return &ScheduledPostRepository{db: db}
}

// GetByID retrieves a scheduled post by ID
func (r *ScheduledPostRepository) GetByID(ctx context.Context, id uint) (*storage.ScheduledPost, error) {
	var post storage.ScheduledPost
	if err := r.db.WithContext(ctx).First(&post, id).Error; err != nil {
		return nil, err
	}
	return &post, nil
}

// GetDuePosts retrieves posts that are due for posting
func (r *ScheduledPostRepository) GetDuePosts(ctx context.Context, limit int) ([]storage.ScheduledPost, error) {
	var posts []storage.ScheduledPost
	now := time.Now()
	if err := r.db.WithContext(ctx).
		Where("status = ? AND scheduled_at <= ?", "scheduled", now).
		Limit(limit).
		Find(&posts).Error; err != nil {
		return nil, err
	}
	return posts, nil
}

// Create creates a new scheduled post
func (r *ScheduledPostRepository) Create(ctx context.Context, post *storage.ScheduledPost) error {
	return r.db.WithContext(ctx).Create(post).Error
}

// UpdateStatus updates the status of a scheduled post
func (r *ScheduledPostRepository) UpdateStatus(ctx context.Context, id uint, status string) error {
	return r.db.WithContext(ctx).Model(&storage.ScheduledPost{}).
		Where("id = ?", id).
		Update("status", status).Error
}

// UpdateStatusWithAttempts updates status and attempts
func (r *ScheduledPostRepository) UpdateStatusWithAttempts(ctx context.Context, id uint, status string, attempts int, lastError string) error {
	updates := map[string]interface{}{
		"status":     status,
		"attempts":   attempts,
		"last_error": lastError,
	}
	return r.db.WithContext(ctx).Model(&storage.ScheduledPost{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// MarkAsPosted marks a post as successfully posted
func (r *ScheduledPostRepository) MarkAsPosted(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&storage.ScheduledPost{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":    "posted",
			"posted_at": &now,
		}).Error
}
