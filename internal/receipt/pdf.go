package receipt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"go.uber.org/zap"
)

// ConvertHTMLToPDF converts HTML content to PDF using Chrome headless
func ConvertHTMLToPDF(ctx context.Context, htmlContent string, logger *zap.Logger) ([]byte, error) {
	// Create a temporary HTML file
	tmpFile, err := os.CreateTemp("", "receipt-*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(htmlContent); err != nil {
		return nil, fmt.Errorf("failed to write HTML: %w", err)
	}
	tmpFile.Close()

	// Use chromedp to convert HTML to PDF
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox,
		chromedp.Headless,
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	ctx, cancel = chromedp.NewContext(allocCtx, chromedp.WithLogf(logger.Sugar().Infof))
	defer cancel()

	var pdfBuf []byte
	err = chromedp.Run(ctx,
		chromedp.Navigate(fmt.Sprintf("file://%s", tmpFile.Name())),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Sleep(1*time.Second), // Wait for styles to load
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			// Use page.PrintToPDF via cdproto
			pdfBuf, _, err = page.PrintToPDF().WithPrintBackground(true).Do(ctx)
			return err
		}),
	)

	if err != nil {
		// Fallback to wkhtmltopdf if chromedp fails
		return convertWithWkhtmltopdf(tmpFile.Name(), logger)
	}

	return pdfBuf, nil
}

// convertWithWkhtmltopdf converts HTML to PDF using wkhtmltopdf (fallback)
func convertWithWkhtmltopdf(htmlFile string, logger *zap.Logger) ([]byte, error) {
	// Check if wkhtmltopdf is available
	if _, err := exec.LookPath("wkhtmltopdf"); err != nil {
		return nil, fmt.Errorf("neither chromedp nor wkhtmltopdf available: %w", err)
	}

	// Create temporary PDF file
	pdfFile, err := os.CreateTemp("", "receipt-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp PDF file: %w", err)
	}
	defer os.Remove(pdfFile.Name())
	defer pdfFile.Close()

	// Run wkhtmltopdf
	cmd := exec.Command("wkhtmltopdf", "--page-size", "A4", "--margin-top", "0mm", "--margin-bottom", "0mm", "--margin-left", "0mm", "--margin-right", "0mm", htmlFile, pdfFile.Name())
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("wkhtmltopdf failed: %w", err)
	}

	// Read PDF file
	pdfBuf, err := os.ReadFile(pdfFile.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF: %w", err)
	}

	return pdfBuf, nil
}

// UploadPDFToCloudinary uploads PDF to Cloudinary
func UploadPDFToCloudinary(ctx context.Context, pdfData []byte, cloudinaryURL string, tenantID uint, receiptID string, logger *zap.Logger) (string, error) {
	if cloudinaryURL == "" {
		return "", fmt.Errorf("cloudinary URL not configured")
	}

	cld, err := cloudinary.NewFromURL(cloudinaryURL)
	if err != nil {
		return "", fmt.Errorf("failed to init cloudinary: %w", err)
	}

	folder := fmt.Sprintf("brancr/receipts/tenant_%d", tenantID)
	publicID := fmt.Sprintf("receipt_%s", receiptID)

	reader := bytes.NewReader(pdfData)

	res, err := cld.Upload.Upload(ctx, reader, uploader.UploadParams{
		PublicID:     publicID,
		Folder:       folder,
		ResourceType: "raw", // PDFs are raw files
		Format:       "pdf",
	})

	if err != nil {
		return "", fmt.Errorf("failed to upload PDF: %w", err)
	}

	logger.Info("PDF receipt uploaded to Cloudinary",
		zap.String("receipt_id", receiptID),
		zap.String("url", res.SecureURL),
		zap.Uint("tenant_id", tenantID))

	return res.SecureURL, nil
}
