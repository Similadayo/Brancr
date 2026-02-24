package receipt

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// PDFGenerator generates PDF receipts for payments
type PDFGenerator struct {
	Logger    *zap.Logger
	OutputDir string // Directory to save generated PDFs
	LogoPath  string // Path to business logo (optional)
}

// NewPDFGenerator creates a new PDF generator
func NewPDFGenerator(logger *zap.Logger, outputDir string) *PDFGenerator {
	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		logger.Error("failed to create PDF output directory", zap.Error(err))
	}

	return &PDFGenerator{
		Logger:    logger,
		OutputDir: outputDir,
	}
}

// GeneratePaymentReceipt generates a PDF receipt for a payment
func (g *PDFGenerator) GeneratePaymentReceipt(
	payment *storage.Payment,
	customer *storage.Customer,
	tenant *storage.Tenant,
	businessProfile *storage.BusinessProfile,
) (string, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	// Set font
	pdf.SetFont("Arial", "", 12)

	// Header with business name
	pdf.SetFont("Arial", "B", 24)
	pdf.SetTextColor(37, 99, 235) // Blue color
	businessName := tenant.Name
	if businessProfile != nil && businessProfile.Name != "" {
		businessName = businessProfile.Name
	}
	pdf.Cell(0, 15, businessName)
	pdf.Ln(10)

	// Receipt title
	pdf.SetFont("Arial", "B", 18)
	pdf.SetTextColor(0, 0, 0)
	pdf.Cell(0, 10, "PAYMENT RECEIPT")
	pdf.Ln(12)

	// Status badge
	pdf.SetFont("Arial", "B", 14)
	pdf.SetTextColor(16, 185, 129) // Green color
	pdf.Cell(0, 8, fmt.Sprintf("Status: %s", payment.VerificationStatus))
	pdf.Ln(15)

	// Reset text color
	pdf.SetTextColor(0, 0, 0)

	// Receipt details box
	pdf.SetFont("Arial", "", 11)
	pdf.SetDrawColor(229, 231, 235)
	pdf.SetFillColor(249, 250, 251)

	// Receipt info
	g.addDetailRow(pdf, "Receipt Number:", payment.PaymentReference)
	g.addDetailRow(pdf, "Issue Date:", time.Now().Format("January 2, 2006"))
	pdf.Ln(5)

	// Payment details section
	pdf.SetFont("Arial", "B", 14)
	pdf.Cell(0, 10, "Payment Details")
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	g.addDetailRow(pdf, "Amount:", fmt.Sprintf("%s %.2f", payment.Currency, payment.Amount))
	g.addDetailRow(pdf, "Payment Method:", g.formatPaymentMethod(payment.PaymentMethod))

	if payment.ClaimedAt != nil {
		g.addDetailRow(pdf, "Payment Date:", payment.ClaimedAt.Format("January 2, 2006"))
	}

	if payment.VerifiedAt != nil {
		g.addDetailRow(pdf, "Verified Date:", payment.VerifiedAt.Format("January 2, 2006"))
	}

	pdf.Ln(5)

	// Customer details section
	pdf.SetFont("Arial", "B", 14)
	pdf.Cell(0, 10, "Customer Information")
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	g.addDetailRow(pdf, "Name:", customer.DisplayName)
	if customer.PlatformID != "" {
		g.addDetailRow(pdf, "Customer ID:", customer.PlatformID)
	}

	pdf.Ln(5)

	// Business details section
	pdf.SetFont("Arial", "B", 14)
	pdf.Cell(0, 10, "Business Information")
	pdf.Ln(8)

	pdf.SetFont("Arial", "", 11)
	if tenant.Email != "" {
		g.addDetailRow(pdf, "Email:", tenant.Email)
	}
	if tenant.OwnerPhone != "" {
		g.addDetailRow(pdf, "Phone:", tenant.OwnerPhone)
	}
	if businessProfile != nil && businessProfile.Website != "" {
		g.addDetailRow(pdf, "Website:", businessProfile.Website)
	}

	// Footer
	pdf.SetY(-30)
	pdf.SetFont("Arial", "I", 9)
	pdf.SetTextColor(107, 114, 128)
	pdf.Cell(0, 5, "Thank you for your payment!")
	pdf.Ln(4)
	pdf.Cell(0, 5, "This is a computer-generated receipt and does not require a signature.")
	pdf.Ln(4)
	pdf.SetFont("Arial", "", 8)
	pdf.Cell(0, 5, "Powered by Brancr")

	// Generate filename
	filename := fmt.Sprintf("receipt_%s_%s.pdf", payment.PaymentReference, time.Now().Format("20060102"))
	outputPath := filepath.Join(g.OutputDir, filename)

	// Save PDF
	if err := pdf.OutputFileAndClose(outputPath); err != nil {
		g.Logger.Error("failed to generate PDF receipt",
			zap.Error(err),
			zap.String("payment_ref", payment.PaymentReference))
		return "", fmt.Errorf("failed to generate PDF: %w", err)
	}

	g.Logger.Info("PDF receipt generated",
		zap.String("payment_ref", payment.PaymentReference),
		zap.String("output_path", outputPath))

	return outputPath, nil
}

// addDetailRow adds a labeled detail row to the PDF
func (g *PDFGenerator) addDetailRow(pdf *gofpdf.Fpdf, label, value string) {
	// Label
	pdf.SetFont("Arial", "B", 11)
	pdf.SetTextColor(107, 114, 128)
	pdf.Cell(60, 8, label)

	// Value
	pdf.SetFont("Arial", "", 11)
	pdf.SetTextColor(17, 24, 39)
	pdf.Cell(0, 8, value)
	pdf.Ln(7)
}

// formatPaymentMethod formats payment method for display
func (g *PDFGenerator) formatPaymentMethod(method string) string {
	switch method {
	case "bank_transfer":
		return "Bank Transfer"
	case "mobile_money":
		return "Mobile Money"
	case "card":
		return "Card Payment"
	case "cash":
		return "Cash"
	default:
		return method
	}
}
