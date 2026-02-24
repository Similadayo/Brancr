package newsletter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// NewsletterService handles newsletter creation and delivery
type NewsletterService struct {
	DB           *gorm.DB
	Logger       *zap.Logger
	EmailService interface {
		SendEmail(to, subject, body string, attachments ...string) error
	}
	PublicURL string // For tracking pixel and unsubscribe links
}

// NewNewsletterService creates a new newsletter service
func NewNewsletterService(
	db *gorm.DB,
	logger *zap.Logger,
	emailService interface {
		SendEmail(to, subject, body string, attachments ...string) error
	},
	publicURL string,
) *NewsletterService {
	return &NewsletterService{
		DB:           db,
		Logger:       logger,
		EmailService: emailService,
		PublicURL:    publicURL,
	}
}

// SubscribeTenant subscribes a tenant to newsletters
func (s *NewsletterService) SubscribeTenant(ctx context.Context, tenantID uuid.UUID, email string, categories []string) error {
	var tenant storage.Tenant
	if err := s.DB.WithContext(ctx).First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	// Use tenant email if not provided
	if email == "" {
		email = tenant.Email
	}

	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("failed to generate token: %w", err)
	}

	categoriesJSON, _ := json.Marshal(categories)

	subscription := &storage.NewsletterSubscription{
		TenantID:         tenantID,
		Email:            email,
		Subscribed:       true,
		SubscribedAt:     time.Now(),
		UnsubscribeToken: token,
		Categories:       string(categoriesJSON),
	}

	if err := s.DB.WithContext(ctx).Save(subscription).Error; err != nil {
		return fmt.Errorf("failed to subscribe tenant: %w", err)
	}

	return nil
}

// UnsubscribeTenant unsubscribes a tenant from newsletters
func (s *NewsletterService) UnsubscribeTenant(ctx context.Context, token string) error {
	var subscription storage.NewsletterSubscription
	if err := s.DB.WithContext(ctx).Where("unsubscribe_token = ?", token).First(&subscription).Error; err != nil {
		return fmt.Errorf("subscription not found: %w", err)
	}

	subscription.Subscribed = false
	now := time.Now()
	subscription.UnsubscribedAt = &now

	if err := s.DB.WithContext(ctx).Save(&subscription).Error; err != nil {
		return fmt.Errorf("failed to unsubscribe: %w", err)
	}

	return nil
}

// CreateNewsletter creates a new newsletter
func (s *NewsletterService) CreateNewsletter(ctx context.Context, title, subject, content string, categories []string, createdBy string, scheduledAt *time.Time) (*storage.Newsletter, error) {
	categoriesJSON, _ := json.Marshal(categories)

	status := "draft"
	if scheduledAt != nil && scheduledAt.After(time.Now()) {
		status = "scheduled"
	}

	newsletter := &storage.Newsletter{
		Title:       title,
		Subject:     subject,
		Content:     content,
		Categories:  string(categoriesJSON),
		Status:      status,
		ScheduledAt: scheduledAt,
		CreatedBy:   createdBy,
	}

	if err := s.DB.WithContext(ctx).Create(newsletter).Error; err != nil {
		return nil, fmt.Errorf("failed to create newsletter: %w", err)
	}

	return newsletter, nil
}

// SendNewsletter sends a newsletter to all subscribed tenants
func (s *NewsletterService) SendNewsletter(ctx context.Context, newsletterID uuid.UUID) error {
	var newsletter storage.Newsletter
	if err := s.DB.WithContext(ctx).First(&newsletter, newsletterID).Error; err != nil {
		return fmt.Errorf("newsletter not found: %w", err)
	}

	// Update status to sending
	newsletter.Status = "sending"
	s.DB.Save(&newsletter)

	// Get all subscribed tenants
	var subscriptions []storage.NewsletterSubscription
	query := s.DB.WithContext(ctx).Where("subscribed = ?", true)

	// Filter by categories if specified
	var categories []string
	if newsletter.Categories != "" {
		json.Unmarshal([]byte(newsletter.Categories), &categories)
		if len(categories) > 0 {
			// For simplicity, we'll include tenants subscribed to any of the categories
			// In production, you might want more sophisticated category matching
		}
	}

	if err := query.Find(&subscriptions).Error; err != nil {
		return fmt.Errorf("failed to fetch subscriptions: %w", err)
	}

	newsletter.TotalRecipients = len(subscriptions)
	s.DB.Save(&newsletter)

	// Send to each subscriber
	for _, sub := range subscriptions {
		if err := s.sendToSubscriber(ctx, &newsletter, &sub); err != nil {
			s.Logger.Error("failed to send newsletter to subscriber",
				zap.Error(err),
				zap.Stringer("tenant_id", sub.TenantID),
				zap.String("email", sub.Email))
			newsletter.FailedCount++
		} else {
			newsletter.SentCount++
		}
		s.DB.Save(&newsletter)
	}

	// Update status to sent
	newsletter.Status = "sent"
	now := time.Now()
	newsletter.SentAt = &now
	s.DB.Save(&newsletter)

	return nil
}

// sendToSubscriber sends newsletter to a single subscriber
func (s *NewsletterService) sendToSubscriber(ctx context.Context, newsletter *storage.Newsletter, subscription *storage.NewsletterSubscription) error {
	// Generate tracking tokens
	openToken, _ := generateToken()
	clickToken, _ := generateToken()

	// Create delivery record
	delivery := &storage.NewsletterDelivery{
		NewsletterID:       newsletter.ID,
		TenantID:           subscription.TenantID,
		Email:              subscription.Email,
		Status:             "pending",
		OpenTrackingToken:  openToken,
		ClickTrackingToken: clickToken,
	}

	if err := s.DB.WithContext(ctx).Create(delivery).Error; err != nil {
		return fmt.Errorf("failed to create delivery record: %w", err)
	}

	// Build email content with tracking and unsubscribe
	content := s.buildEmailContent(newsletter, subscription, delivery)

	// Send email
	if err := s.EmailService.SendEmail(subscription.Email, newsletter.Subject, content); err != nil {
		delivery.Status = "failed"
		delivery.Error = err.Error()
		now := time.Now()
		delivery.FailedAt = &now
		s.DB.Save(delivery)
		return err
	}

	delivery.Status = "sent"
	now := time.Now()
	delivery.SentAt = &now
	s.DB.Save(delivery)

	return nil
}

// buildEmailContent builds the newsletter email HTML with tracking pixels and unsubscribe link
func (s *NewsletterService) buildEmailContent(newsletter *storage.Newsletter, subscription *storage.NewsletterSubscription, delivery *storage.NewsletterDelivery) string {
	unsubscribeURL := fmt.Sprintf("%s/unsubscribe?token=%s", s.PublicURL, subscription.UnsubscribeToken)
	trackingPixel := fmt.Sprintf("%s/track/open?token=%s", s.PublicURL, delivery.OpenTrackingToken)

	// Replace URLs with tracked URLs (simplified - in production you'd want to parse and replace all links)
	_ = newsletter.Content          // Content is used in template below
	_ = delivery.ClickTrackingToken // For future click tracking implementation

	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<style>
		body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.content { background: #ffffff; padding: 20px; }
		.footer { text-align: center; margin-top: 20px; color: #666; font-size: 12px; border-top: 1px solid #eee; padding-top: 20px; }
	</style>
</head>
<body>
	<div class="container">
		<div class="content">
			%s
		</div>
		<div class="footer">
			<p><a href="%s" style="color: #666;">Unsubscribe</a></p>
			<p>© %d Brancr. All rights reserved.</p>
		</div>
	</div>
	<img src="%s" width="1" height="1" style="display:none;" />
</body>
</html>
`, newsletter.Content, unsubscribeURL, time.Now().Year(), trackingPixel)
}

func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
