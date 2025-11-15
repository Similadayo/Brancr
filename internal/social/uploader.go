package social

import (
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
	"go.uber.org/zap"
)

type TelegramFileResp struct {
	OK     bool `json:"ok"`
	Result struct {
		FilePath string `json:"file_path"`
	} `json:"result"`
}

// StreamTelegramFile downloads a Telegram file as a stream
func StreamTelegramFile(ctx context.Context, botToken, fileID string) (io.ReadCloser, string, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", botToken, fileID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, "", fmt.Errorf("getFile: %w", err)
	}
	defer resp.Body.Close()

	var file TelegramFileResp
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		return nil, "", fmt.Errorf("decode telegram json: %w", err)
	}
	if !file.OK || file.Result.FilePath == "" {
		return nil, "", fmt.Errorf("telegram API error: no file path")
	}

	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", botToken, file.Result.FilePath)
	stream, err := http.Get(fileURL)
	if err != nil {
		return nil, "", fmt.Errorf("getFile stream: %w", err)
	}

	resourceType := "image"
	if strings.HasSuffix(strings.ToLower(file.Result.FilePath), ".mp4") {
		resourceType = "video"
	}

	return stream.Body, resourceType, nil
}

// ✅ UploadTelegramMedia now accepts carouselName for folder structure.
func UploadTelegramMedia(
	ctx context.Context,
	logger *zap.Logger,
	cloudURL string,
	tenantID uint,
	botToken string,
	fileID string,
	title string,
	carouselName string,
) (string, error) {

	// 🔍 1. Download from Telegram
	reader, resourceType, err := StreamTelegramFile(ctx, botToken, fileID)
	if err != nil {
		logger.Error("failed to stream telegram file", zap.Error(err))
		return "", err
	}
	defer reader.Close()

	cld, err := cloudinary.NewFromURL(cloudURL)
	if err != nil {
		logger.Error("cloudinary init failed", zap.Error(err))
		return "", err
	}

	// 🔍 2. Pick correct folder
	folder := BuildCloudinaryFolder(tenantID, resourceType, "")
	if folder == "" {
		folder = fmt.Sprintf("brancr/uploads/tenant_%d/misc", tenantID)
	}

	// Generate unique publicID using timestamp + random component + fileID hash
	// This ensures each file gets a unique ID even when uploaded simultaneously
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomStr := base64.URLEncoding.EncodeToString(randomBytes)[:6] // Use first 6 chars
	timestamp := time.Now().UnixNano() // Use nanoseconds instead of seconds
	
	// Use fileID for uniqueness, but handle if it's too short
	fileIDHash := fileID
	if len(fileID) > 12 {
		fileIDHash = fileID[:12] // Use first 12 chars of fileID
	}
	
	publicID := fmt.Sprintf("%s_%d_%s_%s", resourceType, timestamp, randomStr, fileIDHash)

	logger.Info("📤 Uploading to Cloudinary",
		zap.String("tenant_folder", folder),
		zap.String("file_id", fileID),
		zap.String("resource_type", resourceType))

	// 🔍 3. Upload file
	res, err := cld.Upload.Upload(ctx, reader, uploader.UploadParams{
		PublicID:     publicID,
		Folder:       folder,
		ResourceType: resourceType,
		Timestamp:    time.Now().Unix(),
	})
	if err != nil {
		logger.Error("cloudinary upload failed", zap.Error(err))
		return "", err
	}

	// 🔍 4. Log Cloudinary response
	if res.SecureURL == "" {
		logger.Error("⚠️ Empty Cloudinary URL returned",
			zap.String("public_id", publicID),
			zap.Any("response", res))
		return "", fmt.Errorf("cloudinary returned empty url")
	}

	logger.Info("✅ Uploaded to Cloudinary",
		zap.Uint("tenant_id", tenantID),
		zap.String("url", res.SecureURL),
		zap.String("folder", folder),
		zap.String("resource_type", resourceType))

	return res.SecureURL, nil
}

// ✅ Builds folder structure per tenant
func BuildCloudinaryFolder(tenantID uint, mediaType, carouselName string) string {
	base := fmt.Sprintf("brancr/uploads/tenant_%d", tenantID)
	switch mediaType {
	case "carousel":
		if carouselName != "" {
			return fmt.Sprintf("%s/carousels/%s", base, sanitizeFolderName(carouselName))
		}
		return base + "/carousels/unnamed"
	case "image":
		return base + "/photos"
	case "video":
		return base + "/videos"
	default:
		return base + "/misc"
	}
}

func sanitizeFolderName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "/", "-")
	return name
}
