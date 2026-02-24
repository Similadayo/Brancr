package tenantapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/queue"
	"go.uber.org/zap"
)

// parseFile handles POST /tenant/products/parse/file
func (h *Handler) parseFile(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB
		h.Logger.Warn("parse-file-invalid-multipart", zap.Error(err))
		http.Error(w, "invalid multipart request", http.StatusBadRequest)
		return
	}
	file, fh, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Simple size check: if > 2MB, enqueue async job
	if fh.Size > (2 << 20) { // >2MB
		// enqueue job for background processing
		if h.JobQueue == nil {
			h.Logger.Warn("parse-file-no-jobqueue", zap.Stringer("tenant_id", claims.TenantID))
			http.Error(w, "file too large and job queue not configured", http.StatusServiceUnavailable)
			return
		}
		if h.BlobStore == nil {
			h.Logger.Warn("parse-file-no-blobstore", zap.Stringer("tenant_id", claims.TenantID))
			http.Error(w, "file too large and blob storage not configured", http.StatusServiceUnavailable)
			return
		}

		// Upload file to blobstore
		objKey := fmt.Sprintf("imports/tenant_%d/%d_%s", claims.TenantID, time.Now().UnixNano(), fh.Filename)
		if err := h.BlobStore.EnsureBucket(r.Context(), h.Config.StorageBucket); err != nil {
			h.Logger.Error("ensure-bucket-failed", zap.Error(err))
			http.Error(w, "failed to prepare storage", http.StatusInternalServerError)
			return
		}
		// Seek file to beginning if necessary (multipart may be at start)
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			// if seek fails, copy to buffer
			b, err2 := io.ReadAll(file)
			if err2 != nil {
				h.Logger.Error("file-read-failed", zap.Error(err2))
				http.Error(w, "failed to read file", http.StatusInternalServerError)
				return
			}
			if err := h.BlobStore.Upload(r.Context(), h.Config.StorageBucket, objKey, bytes.NewReader(b), int64(len(b)), fh.Header.Get("Content-Type")); err != nil {
				h.Logger.Error("blobstore-upload-failed", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
				http.Error(w, "failed to upload file", http.StatusInternalServerError)
				return
			}
		} else {
			if err := h.BlobStore.Upload(r.Context(), h.Config.StorageBucket, objKey, file, fh.Size, fh.Header.Get("Content-Type")); err != nil {
				h.Logger.Error("blobstore-upload-failed", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
				http.Error(w, "failed to upload file", http.StatusInternalServerError)
				return
			}
		}

		// generate a job id and include it in the payload so the handler can update status reliably
		jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
		payload := queue.ParseImportPayload{TenantID: claims.TenantID, Filename: fh.Filename, JobID: jobID, Bucket: h.Config.StorageBucket, Key: objKey}
		_, err := h.JobQueue.Enqueue(queue.JobTypeParseImport, payload)
		if err != nil {
			h.Logger.Error("enqueue-parse-import-failed", zap.Error(err), zap.Stringer("tenant_id", claims.TenantID))
			http.Error(w, "failed to enqueue parsing job", http.StatusInternalServerError)
			return
		}
		// set job status in redis as pending (use our generated jobID)
		if h.JobQueue != nil && h.JobQueue.GetRedisClient() != nil {
			key := fmt.Sprintf("parse:job:%s", jobID)
			h.JobQueue.GetRedisClient().Set(r.Context(), key, `{"status":"pending"}`, 24*time.Hour)
		}
		writeJSON(w, http.StatusAccepted, map[string]interface{}{"status": "accepted", "job_id": jobID, "message": "file accepted for background processing"})
		return
	}

	// attempt to detect CSV by content-type or filename
	var lines []string
	if fh.Filename != "" && (strings.HasSuffix(strings.ToLower(fh.Filename), ".csv") || strings.HasSuffix(strings.ToLower(fh.Filename), ".tsv")) {
		// parse CSV
		rows, err := parseCSVRows(file)
		if err != nil {
			h.Logger.Warn("csv-parse-failed", zap.Error(err))
			http.Error(w, "failed to parse CSV", http.StatusBadRequest)
			return
		}
		lines = rows
	} else if fh.Filename != "" && strings.HasSuffix(strings.ToLower(fh.Filename), ".docx") {
		text, err := extractTextFromDocx(file)
		if err != nil {
			h.Logger.Warn("docx-extract-failed", zap.Error(err))
			http.Error(w, "failed to extract DOCX", http.StatusBadRequest)
			return
		}
		lines = strings.Split(text, "\n")
	} else if fh.Filename != "" && (strings.HasSuffix(strings.ToLower(fh.Filename), ".jpg") || strings.HasSuffix(strings.ToLower(fh.Filename), ".jpeg") || strings.HasSuffix(strings.ToLower(fh.Filename), ".png") || strings.HasSuffix(strings.ToLower(fh.Filename), ".webp")) {
		// Image parsing via Vision AI
		if h.AIClient == nil {
			h.Logger.Warn("ai-client-not-configured", zap.Stringer("tenant_id", claims.TenantID))
			http.Error(w, "AI service not configured", http.StatusServiceUnavailable)
			return
		}
		if h.BlobStore == nil {
			h.Logger.Warn("blobstore-not-configured", zap.Stringer("tenant_id", claims.TenantID))
			http.Error(w, "blob storage not configured", http.StatusServiceUnavailable)
			return
		}

		// Upload image to blob storage to get URL
		b, err := io.ReadAll(file)
		if err != nil {
			h.Logger.Error("file-read-failed", zap.Error(err))
			http.Error(w, "failed to read file", http.StatusInternalServerError)
			return
		}

		objKey := fmt.Sprintf("catalog-images/tenant_%d/%d_%s", claims.TenantID, time.Now().UnixNano(), fh.Filename)
		if err := h.BlobStore.EnsureBucket(r.Context(), h.Config.StorageBucket); err != nil {
			h.Logger.Error("ensure-bucket-failed", zap.Error(err))
			http.Error(w, "failed to prepare storage", http.StatusInternalServerError)
			return
		}
		if err := h.BlobStore.Upload(r.Context(), h.Config.StorageBucket, objKey, bytes.NewReader(b), int64(len(b)), fh.Header.Get("Content-Type")); err != nil {
			h.Logger.Error("blobstore-upload-failed", zap.Error(err))
			http.Error(w, "failed to upload image", http.StatusInternalServerError)
			return
		}

		// Generate presigned URL for the image (valid for 24 hours)
		imageURL, err := h.BlobStore.GetPresignedURL(r.Context(), h.Config.StorageBucket, objKey, 24*time.Hour)
		if err != nil {
			h.Logger.Error("get-presigned-url-failed", zap.Error(err))
			http.Error(w, "failed to get image URL", http.StatusInternalServerError)
			return
		}

		// Use Vision AI parser
		industry := r.FormValue("industry")
		if industry == "" {
			industry = "products"
		}

		aiParser := NewAIParser(h.AIClient)
		items, err := aiParser.ParseWithImages("", []string{imageURL}, industry)
		if err != nil {
			h.Logger.Warn("vision-parse-failed", zap.Error(err))
			http.Error(w, "failed to parse image", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, items)
		return
	} else {
		// fallback: read file into memory and split by lines
		b, err := io.ReadAll(file)
		if err != nil {
			h.Logger.Warn("file-read-failed", zap.Error(err))
			http.Error(w, "failed to read file", http.StatusInternalServerError)
			return
		}
		lines = strings.Split(string(b), "\n")
	}

	// join lines and send to AI parser for better extraction
	rawInput := strings.Join(lines, "\n")
	industry := r.FormValue("industry")
	if industry == "" {
		industry = "products"
	}

	if h.AIClient != nil {
		aiParser := NewAIParser(h.AIClient)
		items, err := aiParser.Parse(rawInput, industry)
		if err != nil {
			h.Logger.Warn("ai-parse-failed", zap.Error(err), zap.String("fallback", "default"))
			// Fallback to default parser
			parser := NewDefaultParser()
			items, err = parser.Parse(rawInput)
			if err != nil {
				http.Error(w, "failed to parse file", http.StatusInternalServerError)
				return
			}
		}
		writeJSON(w, http.StatusOK, items)
	} else {
		// No AI client, use default parser
		parser := NewDefaultParser()
		items, err := parser.Parse(rawInput)
		if err != nil {
			h.Logger.Warn("parse-file-failed", zap.Error(err))
			http.Error(w, "failed to parse file", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}
