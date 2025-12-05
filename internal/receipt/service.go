package receipt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ReceiptService handles receipt generation and management
type ReceiptService struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	CloudinaryURL string // Cloudinary connection string
	PublicURL     string // Public URL for receipt links
	EmailService  interface {
		SendReceiptEmail(to, customerName, orderNumber, receiptURL, portalURL string) error
	} // Optional email service
}

// NewReceiptService creates a new ReceiptService
// cloudinaryURL can be empty (will try to get from env), publicURL is required for portal links, emailService is optional
func NewReceiptService(db *gorm.DB, logger *zap.Logger, cloudinaryURL, publicURL string, emailService interface {
	SendReceiptEmail(to, customerName, orderNumber, receiptURL, portalURL string) error
}) *ReceiptService {
	// If cloudinaryURL is empty, try to get from environment
	if cloudinaryURL == "" {
		cloudinaryURL = getCloudinaryURL()
	}

	return &ReceiptService{
		DB:            db,
		Logger:        logger,
		CloudinaryURL: cloudinaryURL,
		PublicURL:     publicURL,
		EmailService:  emailService,
	}
}

// getCloudinaryURL gets Cloudinary URL from environment or constructs it from components
func getCloudinaryURL() string {
	// First try CLOUDINARY_URL env var
	if url := os.Getenv("CLOUDINARY_URL"); url != "" {
		return url
	}
	// If not available, try constructing from individual components
	cloudName := os.Getenv("CLOUDINARY_CLOUD_NAME")
	apiKey := os.Getenv("CLOUDINARY_API_KEY")
	apiSecret := os.Getenv("CLOUDINARY_API_SECRET")
	if cloudName != "" && apiKey != "" && apiSecret != "" {
		return fmt.Sprintf("cloudinary://%s:%s@%s", apiKey, apiSecret, cloudName)
	}
	return ""
}

// GenerateReceipt generates a receipt for a confirmed payment
func (s *ReceiptService) GenerateReceipt(ctx context.Context, paymentID uint, tenantID uint) (*storage.Payment, error) {
	var payment storage.Payment
	if err := s.DB.Where("id = ? AND tenant_id = ?", paymentID, tenantID).First(&payment).Error; err != nil {
		return nil, fmt.Errorf("payment not found: %w", err)
	}

	// Check if receipt already exists
	if payment.ReceiptID != "" && payment.ReceiptURL != "" {
		s.Logger.Info("receipt already exists for payment", zap.Uint("payment_id", paymentID))
		return &payment, nil
	}

	// Get order details
	var order storage.Order
	if err := s.DB.Where("id = ? AND tenant_id = ?", payment.OrderID, tenantID).First(&order).Error; err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}

	// Get tenant business profile
	var tenant storage.Tenant
	if err := s.DB.Preload("BusinessProfile").Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	// Generate receipt ID
	receiptID := utils.GenerateReceiptID()

	// Generate receipt HTML
	receiptHTML, err := s.GenerateReceiptHTML(order, payment, tenant)
	if err != nil {
		return nil, fmt.Errorf("failed to generate receipt HTML: %w", err)
	}

	// Convert HTML to PDF and upload to Cloudinary if configured
	receiptURL := receiptHTML // Default to HTML
	if s.CloudinaryURL != "" {
		pdfData, err := ConvertHTMLToPDF(ctx, receiptHTML, s.Logger)
		if err != nil {
			s.Logger.Warn("failed to convert receipt to PDF, using HTML", zap.Error(err))
		} else {
			// Upload PDF to Cloudinary
			pdfURL, err := UploadPDFToCloudinary(ctx, pdfData, s.CloudinaryURL, tenantID, receiptID, s.Logger)
			if err != nil {
				s.Logger.Warn("failed to upload PDF to Cloudinary, using HTML", zap.Error(err))
			} else {
				receiptURL = pdfURL
				s.Logger.Info("PDF receipt uploaded to Cloudinary", zap.String("url", pdfURL))
			}
		}
	}

	// Update payment with receipt details
	payment.ReceiptID = receiptID
	payment.ReceiptURL = receiptURL

	if err := s.DB.Save(&payment).Error; err != nil {
		return nil, fmt.Errorf("failed to save receipt: %w", err)
	}

	// Send email receipt if email service is configured and customer email exists
	if s.EmailService != nil && order.CustomerEmail != "" {
		portalURL := ""
		if payment.PortalToken != "" && s.PublicURL != "" {
			portalURL = fmt.Sprintf("%s/portal/order?token=%s", s.PublicURL, payment.PortalToken)
		}

		if err := s.EmailService.SendReceiptEmail(
			order.CustomerEmail,
			order.CustomerName,
			order.OrderNumber,
			receiptURL,
			portalURL,
		); err != nil {
			s.Logger.Warn("failed to send receipt email", zap.Error(err), zap.String("email", order.CustomerEmail))
			// Don't fail receipt generation if email fails
		} else {
			s.Logger.Info("receipt email sent", zap.String("email", order.CustomerEmail))
		}
	}

	s.Logger.Info("receipt generated successfully",
		zap.Uint("payment_id", paymentID),
		zap.String("receipt_id", receiptID),
		zap.String("receipt_url", receiptURL))

	return &payment, nil
}

// GenerateReceiptHTML generates HTML receipt template
func (s *ReceiptService) GenerateReceiptHTML(order storage.Order, payment storage.Payment, tenant storage.Tenant) (string, error) {
	// Parse order items
	var items []storage.OrderItem
	if err := json.Unmarshal([]byte(order.Items), &items); err != nil {
		return "", fmt.Errorf("failed to parse order items: %w", err)
	}

	// Prepare template data
	data := ReceiptData{
		ReceiptID:       payment.ReceiptID,
		OrderNumber:     order.OrderNumber,
		PaymentRef:      payment.PaymentReference,
		Date:            payment.CreatedAt.Format("January 2, 2006"),
		Time:            payment.CreatedAt.Format("3:04 PM"),
		BusinessName:    tenant.BusinessProfile.Name,
		BusinessEmail:   tenant.Email,
		BusinessPhone:   tenant.OwnerPhone,
		BusinessAddress: tenant.BusinessProfile.Location,
		CustomerName:    order.CustomerName,
		CustomerPhone:   order.CustomerPhone,
		CustomerEmail:   order.CustomerEmail,
		Items:           items,
		Subtotal:        order.TotalAmount,
		Total:           order.TotalAmount,
		Currency:        order.Currency,
		PaymentMethod:   payment.PaymentMethod,
		Status:          payment.Status,
	}

	// Parse template
	tmpl, err := template.New("receipt").Parse(receiptHTMLTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse receipt template: %w", err)
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute receipt template: %w", err)
	}

	return buf.String(), nil
}

// ReceiptData holds data for receipt template
type ReceiptData struct {
	ReceiptID       string
	OrderNumber     string
	PaymentRef      string
	Date            string
	Time            string
	BusinessName    string
	BusinessEmail   string
	BusinessPhone   string
	BusinessAddress string
	CustomerName    string
	CustomerPhone   string
	CustomerEmail   string
	Items           []storage.OrderItem
	Subtotal        float64
	Total           float64
	Currency        string
	PaymentMethod   string
	Status          string
}

// receiptHTMLTemplate is the HTML template for receipts
const receiptHTMLTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Receipt - {{.OrderNumber}}</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            line-height: 1.6;
            color: #333;
            background: #f5f5f5;
            padding: 20px;
        }
        .receipt-container {
            max-width: 800px;
            margin: 0 auto;
            background: white;
            padding: 40px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        .header {
            text-align: center;
            border-bottom: 2px solid #e0e0e0;
            padding-bottom: 20px;
            margin-bottom: 30px;
        }
        .header h1 {
            color: #2563eb;
            font-size: 28px;
            margin-bottom: 10px;
        }
        .header p {
            color: #666;
            font-size: 14px;
        }
        .receipt-info {
            display: flex;
            justify-content: space-between;
            margin-bottom: 30px;
            flex-wrap: wrap;
        }
        .info-section {
            flex: 1;
            min-width: 200px;
            margin-bottom: 20px;
        }
        .info-section h3 {
            font-size: 12px;
            text-transform: uppercase;
            color: #666;
            margin-bottom: 8px;
            letter-spacing: 0.5px;
        }
        .info-section p {
            font-size: 14px;
            color: #333;
            margin: 4px 0;
        }
        .items-table {
            width: 100%;
            border-collapse: collapse;
            margin: 30px 0;
        }
        .items-table thead {
            background: #f8f9fa;
        }
        .items-table th {
            padding: 12px;
            text-align: left;
            font-size: 12px;
            text-transform: uppercase;
            color: #666;
            border-bottom: 2px solid #e0e0e0;
        }
        .items-table td {
            padding: 12px;
            border-bottom: 1px solid #e0e0e0;
            font-size: 14px;
        }
        .items-table tbody tr:hover {
            background: #f8f9fa;
        }
        .text-right {
            text-align: right;
        }
        .totals {
            margin-top: 20px;
            padding-top: 20px;
            border-top: 2px solid #e0e0e0;
        }
        .total-row {
            display: flex;
            justify-content: space-between;
            padding: 8px 0;
            font-size: 14px;
        }
        .total-row.grand-total {
            font-size: 18px;
            font-weight: bold;
            color: #2563eb;
            padding-top: 10px;
            border-top: 1px solid #e0e0e0;
            margin-top: 10px;
        }
        .footer {
            margin-top: 40px;
            padding-top: 20px;
            border-top: 2px solid #e0e0e0;
            text-align: center;
            color: #666;
            font-size: 12px;
        }
        .status-badge {
            display: inline-block;
            padding: 4px 12px;
            border-radius: 12px;
            font-size: 12px;
            font-weight: 500;
            text-transform: uppercase;
        }
        .status-confirmed {
            background: #d1fae5;
            color: #065f46;
        }
        .status-verified {
            background: #dbeafe;
            color: #1e40af;
        }
        @media print {
            body {
                background: white;
                padding: 0;
            }
            .receipt-container {
                box-shadow: none;
            }
        }
    </style>
</head>
<body>
    <div class="receipt-container">
        <div class="header">
            <h1>Receipt</h1>
            <p>{{.BusinessName}}</p>
        </div>

        <div class="receipt-info">
            <div class="info-section">
                <h3>Receipt Details</h3>
                <p><strong>Receipt ID:</strong> {{.ReceiptID}}</p>
                <p><strong>Order Number:</strong> {{.OrderNumber}}</p>
                <p><strong>Payment Ref:</strong> {{.PaymentRef}}</p>
                <p><strong>Date:</strong> {{.Date}}</p>
                <p><strong>Time:</strong> {{.Time}}</p>
            </div>

            <div class="info-section">
                <h3>Business Information</h3>
                <p><strong>{{.BusinessName}}</strong></p>
                {{if .BusinessAddress}}<p>{{.BusinessAddress}}</p>{{end}}
                {{if .BusinessPhone}}<p>{{.BusinessPhone}}</p>{{end}}
                {{if .BusinessEmail}}<p>{{.BusinessEmail}}</p>{{end}}
            </div>

            <div class="info-section">
                <h3>Customer Information</h3>
                <p><strong>{{.CustomerName}}</strong></p>
                {{if .CustomerPhone}}<p>{{.CustomerPhone}}</p>{{end}}
                {{if .CustomerEmail}}<p>{{.CustomerEmail}}</p>{{end}}
            </div>
        </div>

        <table class="items-table">
            <thead>
                <tr>
                    <th>Item</th>
                    <th class="text-right">Quantity</th>
                    <th class="text-right">Unit Price</th>
                    <th class="text-right">Total</th>
                </tr>
            </thead>
            <tbody>
                {{range .Items}}
                <tr>
                    <td>
                        <strong>{{.Name}}</strong>
                        {{if .Description}}<br><small style="color: #666;">{{.Description}}</small>{{end}}
                    </td>
                    <td class="text-right">{{.Quantity}}</td>
                    <td class="text-right">{{printf "%.2f" .UnitPrice}} {{$.Currency}}</td>
                    <td class="text-right"><strong>{{printf "%.2f" .TotalPrice}} {{$.Currency}}</strong></td>
                </tr>
                {{end}}
            </tbody>
        </table>

        <div class="totals">
            <div class="total-row">
                <span>Subtotal:</span>
                <span>{{printf "%.2f" .Subtotal}} {{.Currency}}</span>
            </div>
            <div class="total-row grand-total">
                <span>Total:</span>
                <span>{{printf "%.2f" .Total}} {{.Currency}}</span>
            </div>
        </div>

        <div style="margin-top: 30px; padding: 15px; background: #f8f9fa; border-radius: 8px;">
            <p style="margin-bottom: 8px;"><strong>Payment Method:</strong> {{.PaymentMethod}}</p>
            <p><strong>Status:</strong> <span class="status-badge status-{{.Status}}">{{.Status}}</span></p>
        </div>

        <div class="footer">
            <p>Thank you for your business!</p>
            <p>This is an official receipt. Please keep it for your records.</p>
        </div>
    </div>
</body>
</html>
`

// GeneratePortalTokenAndURL generates a portal token and URL for customer access
func (s *ReceiptService) GeneratePortalTokenAndURL(ctx context.Context, orderID uint, tenantID uint, publicURL string) (string, string, error) {
	// Check if payment exists for this order
	var payment storage.Payment
	if err := s.DB.Where("order_id = ? AND tenant_id = ?", orderID, tenantID).First(&payment).Error; err != nil {
		// If no payment exists, create a placeholder payment record for portal access
		// This allows customers to access their order even before payment
		var order storage.Order
		if err := s.DB.Where("id = ? AND tenant_id = ?", orderID, tenantID).First(&order).Error; err != nil {
			return "", "", fmt.Errorf("order not found: %w", err)
		}

		// Generate portal token
		portalToken := utils.GeneratePortalToken()

		// Create payment record with portal token
		payment = storage.Payment{
			TenantID:         tenantID,
			OrderID:          orderID,
			CustomerID:       order.CustomerID,
			PaymentReference: order.PaymentReference,
			Amount:           order.TotalAmount,
			Currency:         order.Currency,
			Status:           "pending",
			PortalToken:      portalToken,
			CreatedAt:        time.Now(),
		}

		if err := s.DB.Create(&payment).Error; err != nil {
			return "", "", fmt.Errorf("failed to create payment record: %w", err)
		}
	} else if payment.PortalToken == "" {
		// Generate portal token if not exists
		portalToken := utils.GeneratePortalToken()
		payment.PortalToken = portalToken
		if err := s.DB.Save(&payment).Error; err != nil {
			return "", "", fmt.Errorf("failed to save portal token: %w", err)
		}
	}

	// Generate portal URL
	portalURL := fmt.Sprintf("%s/portal/order?token=%s", publicURL, payment.PortalToken)
	payment.PortalURL = portalURL

	if err := s.DB.Save(&payment).Error; err != nil {
		return "", "", fmt.Errorf("failed to save portal URL: %w", err)
	}

	return payment.PortalToken, portalURL, nil
}
