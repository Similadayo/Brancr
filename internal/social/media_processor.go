package social

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"go.uber.org/zap"
)

// MediaProcessor handles downloading, storing, and processing media from Gupshup
type MediaProcessor struct {
	Logger     *zap.Logger
	AIClient   *ai.AIClient
	Cloudinary *cloudinary.Cloudinary
}

// NewMediaProcessor creates a new media processor
func NewMediaProcessor(logger *zap.Logger, aiClient *ai.AIClient, cloudURL string) (*MediaProcessor, error) {
	var cld *cloudinary.Cloudinary
	var err error
	if cloudURL != "" {
		cld, err = cloudinary.NewFromURL(cloudURL)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Cloudinary: %w", err)
		}
	}

	return &MediaProcessor{
		Logger:     logger,
		AIClient:   aiClient,
		Cloudinary: cld,
	}, nil
}

// ProcessMediaResult contains the results of media processing
type ProcessMediaResult struct {
	StoredURL     string            // URL where media is stored (Cloudinary)
	Transcription string            // For audio: transcribed text
	ImageAnalysis string            // For images: AI analysis/description
	DocumentText  string            // For documents: extracted text
	Metadata      map[string]string // Additional metadata (filename, content-type, etc.)
}

// ProcessMedia downloads media from Gupshup URL, stores it, and processes it
func (p *MediaProcessor) ProcessMedia(ctx context.Context, tenantID uuid.UUID, mediaURL, mediaType string) (*ProcessMediaResult, error) {
	result := &ProcessMediaResult{
		Metadata: make(map[string]string),
	}

	// Step 1: Download media from Gupshup URL
	p.Logger.Info("📥 Downloading media from Gupshup",
		zap.String("media_url", mediaURL),
		zap.String("media_type", mediaType),
		zap.Stringer("tenant_id", tenantID))

	client := &http.Client{Timeout: 60 * time.Second} // Longer timeout for media downloads
	req, err := http.NewRequestWithContext(ctx, "GET", mediaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to download media (status %d): %s", resp.StatusCode, string(body))
	}

	// Read media into memory (or stream to Cloudinary)
	mediaData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read media data: %w", err)
	}

	// Extract content type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		// Infer from media type
		switch mediaType {
		case "audio":
			contentType = "audio/ogg" // WhatsApp voice notes are typically OGG
		case "image":
			contentType = "image/jpeg"
		case "video":
			contentType = "video/mp4"
		case "document":
			contentType = "application/pdf"
		}
	}
	result.Metadata["content_type"] = contentType

	// Step 2: Upload to Cloudinary for permanent storage
	if p.Cloudinary != nil {
		resourceType := "auto" // Cloudinary auto-detects
		switch mediaType {
		case "audio":
			resourceType = "video" // Cloudinary stores audio as video type
		case "image":
			resourceType = "image"
		case "video":
			resourceType = "video"
		case "document":
			resourceType = "raw" // Documents stored as raw files
		case "sticker":
			resourceType = "image"
		}

		folder := BuildCloudinaryFolder(tenantID, resourceType, "whatsapp_media")
		if folder == "" {
			folder = fmt.Sprintf("brancr/whatsapp_media/tenant_%s", tenantID.String())
		}

		// Generate unique public ID
		randomBytes := make([]byte, 4)
		rand.Read(randomBytes)
		randomStr := base64.URLEncoding.EncodeToString(randomBytes)[:6]
		timestamp := time.Now().UnixNano()
		publicID := fmt.Sprintf("whatsapp_%s_%d_%s", mediaType, timestamp, randomStr)

		p.Logger.Info("📤 Uploading media to Cloudinary",
			zap.String("resource_type", resourceType),
			zap.String("folder", folder),
			zap.String("public_id", publicID))

		uploadResult, err := p.Cloudinary.Upload.Upload(ctx, bytes.NewReader(mediaData), uploader.UploadParams{
			PublicID:     publicID,
			Folder:       folder,
			ResourceType: resourceType,
			Timestamp:    time.Now().Unix(),
		})
		if err != nil {
			p.Logger.Warn("failed to upload media to Cloudinary, keeping original URL",
				zap.Error(err),
				zap.String("media_url", mediaURL))
			result.StoredURL = mediaURL // Fallback to original URL
		} else {
			result.StoredURL = uploadResult.SecureURL
			p.Logger.Info("✅ Media uploaded to Cloudinary",
				zap.String("stored_url", result.StoredURL))
		}
	} else {
		// No Cloudinary configured, use original URL (may expire)
		result.StoredURL = mediaURL
		p.Logger.Warn("⚠️ Cloudinary not configured, using original Gupshup URL (may expire)",
			zap.String("media_url", mediaURL))
	}

	// Step 3: Process media based on type
	switch mediaType {
	case "audio":
		// Transcribe audio using OpenAI Whisper
		transcription, err := p.transcribeAudio(ctx, bytes.NewReader(mediaData), contentType)
		if err != nil {
			p.Logger.Warn("failed to transcribe audio",
				zap.Error(err),
				zap.String("note", "Continuing without transcription"))
		} else {
			result.Transcription = transcription
			p.Logger.Info("✅ Audio transcribed",
				zap.String("transcription_preview", func() string {
					if len(transcription) > 100 {
						return transcription[:100] + "..."
					}
					return transcription
				}()))
		}

	case "image":
		// Analyze image using OpenAI Vision
		analysis, err := p.analyzeImage(ctx, result.StoredURL)
		if err != nil {
			p.Logger.Warn("failed to analyze image",
				zap.Error(err),
				zap.String("note", "Continuing without analysis"))
		} else {
			result.ImageAnalysis = analysis
			p.Logger.Info("✅ Image analyzed",
				zap.String("analysis_preview", func() string {
					if len(analysis) > 100 {
						return analysis[:100] + "..."
					}
					return analysis
				}()))
		}

	case "document":
		// Extract text from document
		text, err := p.extractDocumentText(ctx, bytes.NewReader(mediaData), contentType)
		if err != nil {
			p.Logger.Warn("failed to extract document text",
				zap.Error(err),
				zap.String("note", "Continuing without text extraction"))
		} else {
			result.DocumentText = text
			p.Logger.Info("✅ Document text extracted",
				zap.Int("text_length", len(text)),
				zap.String("text_preview", func() string {
					if len(text) > 200 {
						return text[:200] + "..."
					}
					return text
				}()))
		}

	case "video":
		// For videos, we could extract frames or generate description
		// For now, just store the video
		p.Logger.Info("📹 Video stored (no processing yet)",
			zap.String("stored_url", result.StoredURL))

	case "sticker":
		// Stickers are just images, analyze them
		analysis, err := p.analyzeImage(ctx, result.StoredURL)
		if err != nil {
			p.Logger.Warn("failed to analyze sticker",
				zap.Error(err))
		} else {
			result.ImageAnalysis = analysis
		}
	}

	return result, nil
}

// transcribeAudio transcribes audio using OpenAI Whisper API
// Whisper API endpoint: POST /v1/audio/transcriptions
// Supports: mp3, mp4, mpeg, mpga, m4a, wav, webm
func (p *MediaProcessor) transcribeAudio(ctx context.Context, audioData io.Reader, contentType string) (string, error) {
	if p.AIClient == nil || p.AIClient.APIKey == "" {
		return "", fmt.Errorf("OpenAI API key not configured")
	}

	// Read audio data into buffer
	audioBytes, err := io.ReadAll(audioData)
	if err != nil {
		return "", fmt.Errorf("failed to read audio data: %w", err)
	}

	if len(audioBytes) == 0 {
		return "", fmt.Errorf("audio data is empty")
	}

	p.Logger.Info("🎤 Transcribing audio with OpenAI Whisper",
		zap.String("content_type", contentType),
		zap.Int("audio_size_bytes", len(audioBytes)))

	// Create multipart form request for Whisper API
	// OpenAI Whisper expects: file (audio file) and model ("whisper-1")
	var requestBody bytes.Buffer
	writer := &requestBody

	// Generate a proper multipart boundary
	boundaryBytes := make([]byte, 16)
	rand.Read(boundaryBytes)
	boundary := "----BrancrFormBoundary" + base64.URLEncoding.EncodeToString(boundaryBytes)[:22]

	// Write multipart form data manually (proper format)
	// Part 1: File field
	fmt.Fprintf(writer, "--%s\r\n", boundary)
	fmt.Fprintf(writer, "Content-Disposition: form-data; name=\"file\"; filename=\"audio.ogg\"\r\n")
	if contentType != "" {
		fmt.Fprintf(writer, "Content-Type: %s\r\n", contentType)
	}
	fmt.Fprintf(writer, "\r\n")
	writer.Write(audioBytes)
	fmt.Fprintf(writer, "\r\n")

	// Part 2: Model field
	fmt.Fprintf(writer, "--%s\r\n", boundary)
	fmt.Fprintf(writer, "Content-Disposition: form-data; name=\"model\"\r\n")
	fmt.Fprintf(writer, "\r\n")
	fmt.Fprintf(writer, "whisper-1\r\n")

	// Part 3: Language hint (English - Whisper doesn't support Pidgin directly)
	fmt.Fprintf(writer, "--%s\r\n", boundary)
	fmt.Fprintf(writer, "Content-Disposition: form-data; name=\"language\"\r\n")
	fmt.Fprintf(writer, "\r\n")
	fmt.Fprintf(writer, "en\r\n")

	// Part 4: Prompt for Nigerian English/Pidgin context (improves accuracy)
	fmt.Fprintf(writer, "--%s\r\n", boundary)
	fmt.Fprintf(writer, "Content-Disposition: form-data; name=\"prompt\"\r\n")
	fmt.Fprintf(writer, "\r\n")
	fmt.Fprintf(writer, "Nigerian English and Pidgin. Common terms: abeg, wetin, una, dey, na, wahala, oga, madam, how far, no wahala, e dey, sha, o, naira.\r\n")

	// Close boundary
	fmt.Fprintf(writer, "--%s--\r\n", boundary)

	// Call OpenAI Whisper API
	url := fmt.Sprintf("%s/audio/transcriptions", p.AIClient.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, &requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.AIClient.APIKey)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	client := &http.Client{Timeout: 120 * time.Second} // Longer timeout for transcription (audio can be long)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcription request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		p.Logger.Error("❌ Whisper API error",
			zap.Int("status_code", resp.StatusCode),
			zap.String("error_body", string(body)))
		return "", fmt.Errorf("transcription failed (status %d): %s", resp.StatusCode, string(body))
	}

	var transcriptionResp struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&transcriptionResp); err != nil {
		return "", fmt.Errorf("failed to decode transcription response: %w", err)
	}

	p.Logger.Info("✅ Audio transcribed successfully",
		zap.Int("transcription_length", len(transcriptionResp.Text)),
		zap.String("transcription_preview", func() string {
			if len(transcriptionResp.Text) > 100 {
				return transcriptionResp.Text[:100] + "..."
			}
			return transcriptionResp.Text
		}()))

	return transcriptionResp.Text, nil
}

// analyzeImage analyzes an image using OpenAI Vision API
func (p *MediaProcessor) analyzeImage(ctx context.Context, imageURL string) (string, error) {
	if p.AIClient == nil || p.AIClient.APIKey == "" {
		return "", fmt.Errorf("OpenAI API key not configured")
	}

	// Use Vision API to analyze image
	messages := []ai.Message{
		{
			Role: "user",
			Content: []ai.ContentPart{
				{
					Type: "text",
					Text: "Describe what you see in this image in detail. Include any text, objects, people, or important details that would help understand the customer's message.",
				},
				{
					Type: "image_url",
					ImageURL: &struct {
						URL string `json:"url"`
					}{URL: imageURL},
				},
			},
		},
	}

	// Use vision-capable model (gpt-4o or gpt-4o-mini)
	originalModel := p.AIClient.Model
	p.AIClient.Model = "gpt-4o-mini" // Use vision-capable model

	analysis, err := p.AIClient.Generate(ctx, messages)

	// Restore original model
	p.AIClient.Model = originalModel

	if err != nil {
		return "", fmt.Errorf("image analysis failed: %w", err)
	}

	return analysis, nil
}

// extractDocumentText extracts text from documents (PDF, Word, etc.)
func (p *MediaProcessor) extractDocumentText(ctx context.Context, documentData io.Reader, contentType string) (string, error) {
	// For now, we'll use OpenAI's document processing capabilities
	// This is a simplified version - you might want to use specialized libraries for PDF/Word extraction

	// Read document into memory
	docBytes, err := io.ReadAll(documentData)
	if err != nil {
		return "", fmt.Errorf("failed to read document: %w", err)
	}

	// For PDFs and images, we can use OpenAI Vision API
	if strings.HasPrefix(contentType, "application/pdf") || strings.HasPrefix(contentType, "image/") {
		// Upload document to a temporary location or use base64
		// For now, if it's an image-based PDF, we'd need to convert pages to images
		// This is a placeholder - you'd need a PDF library for full extraction
		return "", fmt.Errorf("PDF text extraction not yet implemented - requires PDF library")
	}

	// For text-based documents, try to extract directly
	if strings.HasPrefix(contentType, "text/") {
		// Try to decode as text
		text := string(docBytes)
		if len(text) > 0 {
			return text, nil
		}
	}

	// For other document types, return error (would need specialized libraries)
	return "", fmt.Errorf("document type %s not supported for text extraction", contentType)
}
