package tenantapi

import (
	"encoding/json"
)

// InteractionMedia represents media information for an interaction
type InteractionMedia struct {
	URL           string `json:"url,omitempty"`            // Original media URL
	StoredURL     string `json:"stored_url,omitempty"`     // Cloudinary URL (permanent)
	Type          string `json:"type,omitempty"`           // audio, image, video, document, sticker
	Transcription string `json:"transcription,omitempty"`  // For audio: transcribed text
	ImageAnalysis string `json:"image_analysis,omitempty"` // For images: AI description
	DocumentText  string `json:"document_text,omitempty"`  // For documents: extracted text
	Filename      string `json:"filename,omitempty"`       // For documents: original filename
	Caption       string `json:"caption,omitempty"`        // For images/videos: customer caption
}

// parseMediaFromMetadata extracts media information from Interaction Metadata JSON
func parseMediaFromMetadata(metadataJSON string) *InteractionMedia {
	if metadataJSON == "" {
		return nil
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return nil
	}

	// Check if this interaction has media
	mediaURL, _ := metadata["media_url"].(string)
	mediaType, _ := metadata["media_type"].(string)
	storedURL, _ := metadata["media_stored_url"].(string)
	
	// Also check for nested media object
	if mediaURL == "" {
		if mediaObj, ok := metadata["media"].(map[string]interface{}); ok {
			if url, ok := mediaObj["media_url"].(string); ok {
				mediaURL = url
			}
			if mType, ok := mediaObj["media_type"].(string); ok {
				mediaType = mType
			}
		}
	}

	// Check if we have any media indicators (URL, stored URL, type, or transcription/analysis)
	// This ensures we return media info even if only stored_url exists (after processing)
	hasTranscription, _ := metadata["audio_transcription"].(string)
	hasImageAnalysis, _ := metadata["image_analysis"].(string)
	hasDocumentText, _ := metadata["document_text"].(string)
	
	// If no media indicators at all, return nil
	if mediaURL == "" && storedURL == "" && mediaType == "" && hasTranscription == "" && hasImageAnalysis == "" && hasDocumentText == "" {
		return nil
	}

	media := &InteractionMedia{
		URL:  mediaURL,
		Type: mediaType,
	}
	
	// If mediaType is empty but we have stored URL or transcription/analysis, infer type
	if mediaType == "" {
		if storedURL != "" || hasTranscription != "" {
			mediaType = "audio" // Likely audio if we have transcription
		} else if hasImageAnalysis != "" {
			mediaType = "image" // Likely image if we have analysis
		} else if hasDocumentText != "" {
			mediaType = "document" // Likely document if we have text
		}
		media.Type = mediaType
	}

	// Extract stored URL (Cloudinary) - use existing storedURL variable
	if storedURL != "" {
		media.StoredURL = storedURL
	}

	// Extract transcription (for audio)
	if transcription, ok := metadata["audio_transcription"].(string); ok && transcription != "" {
		media.Transcription = transcription
	}

	// Extract image analysis (for images/stickers)
	if analysis, ok := metadata["image_analysis"].(string); ok && analysis != "" {
		media.ImageAnalysis = analysis
	}

	// Extract document text (for documents)
	if docText, ok := metadata["document_text"].(string); ok && docText != "" {
		media.DocumentText = docText
	}

	// Extract filename (for documents)
	if filename, ok := metadata["filename"].(string); ok && filename != "" {
		media.Filename = filename
	} else if mediaObj, ok := metadata["media"].(map[string]interface{}); ok {
		if fn, ok := mediaObj["filename"].(string); ok {
			media.Filename = fn
		}
	}

	// Extract caption (for images/videos)
	if caption, ok := metadata["caption"].(string); ok && caption != "" {
		media.Caption = caption
	} else if mediaObj, ok := metadata["media"].(map[string]interface{}); ok {
		if cap, ok := mediaObj["caption"].(string); ok {
			media.Caption = cap
		}
	}

	return media
}

