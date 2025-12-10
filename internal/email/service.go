package email

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"gopkg.in/gomail.v2"
)

// EmailService handles sending emails
type EmailService struct {
	Logger   *zap.Logger
	From     string
	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
}

// NewEmailService creates a new email service
func NewEmailService(logger *zap.Logger) *EmailService {
	smtpPort := 587
	if port := os.Getenv("SMTP_PORT"); port != "" {
		fmt.Sscanf(port, "%d", &smtpPort)
	}

	return &EmailService{
		Logger:   logger,
		From:     getEnv("SMTP_FROM", "noreply@brancr.com"),
		SMTPHost: getEnv("SMTP_HOST", ""),
		SMTPPort: smtpPort,
		SMTPUser: os.Getenv("SMTP_USER"),
		SMTPPass: os.Getenv("SMTP_PASS"),
	}
}

// SendEmail sends an email
func (s *EmailService) SendEmail(to, subject, body string, attachments ...string) error {
	if s.SMTPHost == "" || s.SMTPUser == "" || s.SMTPPass == "" {
		s.Logger.Warn("SMTP not configured, skipping email", zap.String("to", to), zap.String("subject", subject))
		return fmt.Errorf("SMTP not configured")
	}

	m := gomail.NewMessage()
	m.SetHeader("From", s.From)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body)

	// Add attachments
	for _, att := range attachments {
		m.Attach(att)
	}

	d := gomail.NewDialer(s.SMTPHost, s.SMTPPort, s.SMTPUser, s.SMTPPass)

	if err := d.DialAndSend(m); err != nil {
		s.Logger.Error("failed to send email", zap.Error(err), zap.String("to", to))
		return err
	}

	s.Logger.Info("email sent successfully", zap.String("to", to), zap.String("subject", subject))
	return nil
}

// SendReceiptEmail sends a receipt email to customer
func (s *EmailService) SendReceiptEmail(to, customerName, orderNumber, receiptURL, portalURL string) error {
	subject := fmt.Sprintf("Receipt for Order %s", orderNumber)

	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<style>
		body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.header { background: #2563eb; color: white; padding: 20px; text-align: center; border-radius: 8px 8px 0 0; }
		.content { background: #f9fafb; padding: 20px; border-radius: 0 0 8px 8px; }
		.button { display: inline-block; padding: 12px 24px; background: #2563eb; color: white; text-decoration: none; border-radius: 6px; margin: 10px 0; }
		.footer { text-align: center; margin-top: 20px; color: #666; font-size: 12px; }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<h1>Thank You for Your Order!</h1>
		</div>
		<div class="content">
			<p>Hi %s,</p>
			<p>Thank you for your purchase! Your order <strong>%s</strong> has been confirmed.</p>
			<p>Your receipt is attached to this email. You can also:</p>
			<p>
				<a href="%s" class="button">View Receipt Online</a><br>
				<a href="%s" class="button">Track Your Order</a>
			</p>
			<p>If you have any questions, please don't hesitate to contact us.</p>
			<p>Best regards,<br>Brancr Team</p>
		</div>
		<div class="footer">
			<p>This is an automated email. Please do not reply.</p>
		</div>
	</div>
</body>
</html>
`, customerName, orderNumber, receiptURL, portalURL)

	return s.SendEmail(to, subject, body)
}

// SendOrderStatusEmail sends order status update email
func (s *EmailService) SendOrderStatusEmail(to, customerName, orderNumber, status, portalURL string) error {
	subject := fmt.Sprintf("Order %s Status Update", orderNumber)

	statusMessages := map[string]string{
		"confirmed":  "Your order has been confirmed and payment received.",
		"processing": "Your order is now being processed.",
		"completed":  "Your order has been completed and is ready!",
		"cancelled":  "Your order has been cancelled.",
	}

	message := statusMessages[status]
	if message == "" {
		message = fmt.Sprintf("Your order status has been updated to: %s", status)
	}

	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<style>
		body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.header { background: #2563eb; color: white; padding: 20px; text-align: center; border-radius: 8px 8px 0 0; }
		.content { background: #f9fafb; padding: 20px; border-radius: 0 0 8px 8px; }
		.button { display: inline-block; padding: 12px 24px; background: #2563eb; color: white; text-decoration: none; border-radius: 6px; margin: 10px 0; }
		.status { display: inline-block; padding: 6px 12px; background: #dbeafe; color: #1e40af; border-radius: 4px; font-weight: bold; }
		.footer { text-align: center; margin-top: 20px; color: #666; font-size: 12px; }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<h1>Order Status Update</h1>
		</div>
		<div class="content">
			<p>Hi %s,</p>
			<p>%s</p>
			<p>Order Number: <strong>%s</strong></p>
			<p>Status: <span class="status">%s</span></p>
			<p>
				<a href="%s" class="button">Track Your Order</a>
			</p>
			<p>If you have any questions, please don't hesitate to contact us.</p>
			<p>Best regards,<br>Brancr Team</p>
		</div>
		<div class="footer">
			<p>This is an automated email. Please do not reply.</p>
		</div>
	</div>
</body>
</html>
`, customerName, message, orderNumber, status, portalURL)

	return s.SendEmail(to, subject, body)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// SendPaymentReceiptEmail sends payment receipt to customer via email
func (s *EmailService) SendPaymentReceiptEmail(to, customerName, businessName, paymentRef string, amount float64, currency, paymentMethod string, pdfAttachment string) error {
	subject := fmt.Sprintf("Payment Receipt - %s", paymentRef)

	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<style>
		body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; background: #f3f4f6; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.receipt { background: white; border-radius: 12px; overflow: hidden; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
		.header { background: linear-gradient(135deg, #10b981, #059669); color: white; padding: 30px 20px; text-align: center; }
		.header h1 { margin: 0; font-size: 28px; }
		.checkmark { font-size: 48px; margin-bottom: 10px; }
		.content { padding: 30px 20px; }
		.detail-row { display: flex; justify-content: space-between; padding: 12px 0; border-bottom: 1px solid #e5e7eb; }
		.detail-label { font-weight: 600; color: #6b7280; }
		.detail-value { color: #111827; }
		.amount { font-size: 32px; font-weight: bold; color: #10b981; text-align: center; margin: 20px 0; }
		.footer { background: #f9fafb; padding: 20px; text-align: center; color: #6b7280; font-size: 14px; }
		.button { display: inline-block; padding: 12px 24px; background: #10b981; color: white; text-decoration: none; border-radius: 6px; margin: 20px 0; font-weight: 600; }
	</style>
</head>
<body>
	<div class="container">
		<div class="receipt">
			<div class="header">
				<div class="checkmark">✓</div>
				<h1>Payment Confirmed</h1>
				<p style="margin: 10px 0 0 0;">Thank you for your payment</p>
			</div>
			<div class="content">
				<p>Hi %s,</p>
				<p>This is to confirm that we have received your payment. Your receipt is attached as a PDF.</p>
				
				<div class="amount">%s %.2f</div>
				
				<div class="detail-row">
					<span class="detail-label">Receipt Number</span>
					<span class="detail-value">%s</span>
				</div>
				<div class="detail-row">
					<span class="detail-label">Business</span>
					<span class="detail-value">%s</span>
				</div>
				<div class="detail-row">
					<span class="detail-label">Payment Method</span>
					<span class="detail-value">%s</span>
				</div>
				<div class="detail-row">
					<span class="detail-label">Status</span>
					<span class="detail-value" style="color: #10b981; font-weight: 600;">✓ Verified</span>
				</div>
				
				<p style="margin-top: 30px;">If you have any questions about this payment, please contact us.</p>
			</div>
			<div class="footer">
				<p><strong>%s</strong></p>
				<p style="margin: 5px 0;">Powered by Brancr</p>
				<p style="font-size: 12px; color: #9ca3af;">This is an automated receipt. Please keep it for your records.</p>
			</div>
		</div>
	</div>
</body>
</html>
`, customerName, currency, amount, paymentRef, businessName, paymentMethod, businessName)

	// Send with PDF attachment if provided
	if pdfAttachment != "" {
		return s.SendEmail(to, subject, body, pdfAttachment)
	}
	return s.SendEmail(to, subject, body)
}
