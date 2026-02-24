package social

import (
	"strings"
)

var videoExtensions = []string{".mp4", ".mov", ".m4v", ".webm", ".avi", ".wmv", ".mkv"}

// isVideoURL performs a best-effort check on the URL/filename to determine if it references a video asset.
func isVideoURL(mediaURL string) bool {
	lower := strings.ToLower(mediaURL)

	// Strip query parameters or fragments
	if idx := strings.IndexAny(lower, "?#"); idx != -1 {
		lower = lower[:idx]
	}

	for _, ext := range videoExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// inferMediaType attempts to resolve a normalized media type string based on the stored type and URLs.
// Returns one of: image, video, reel, carousel.
func inferMediaType(rawType string, urls []string) string {
	t := strings.ToLower(strings.TrimSpace(rawType))

	switch t {
	case "photo", "picture", "image":
		return "image"
	case "video":
		return "video"
	case "reel", "reels":
		return "reel"
	case "carousel":
		if len(urls) > 1 {
			return "carousel"
		}
		// Fall through if only a single asset is present.
	}

	if len(urls) > 1 {
		return "carousel"
	}

	if len(urls) > 0 && isVideoURL(urls[0]) {
		return "video"
	}

	return "image"
}
