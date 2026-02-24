package utils

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ScrapeWebsiteContent fetches the title, meta description, and main text from a URL.
// It returns a formatted string suitable for AI analysis.
func ScrapeWebsiteContent(url string) (string, error) {
	if !strings.HasPrefix(url, "http") {
		url = "https://" + url
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Allow skipping SSL verify for broader compatibility
		},
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned non-200 status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read body: %w", err)
	}

	html := string(bodyBytes)

	// Simple extraction using Regex (robust enough for this purpose)
	title := extractTag(html, "title")
	metaDesc := extractMeta(html, "description")
	h1 := extractTag(html, "h1")
	h2 := extractTag(html, "h2")

	// Limit body text to first 2000 chars to avoid token limits, prioritizing useful text
	// This is a very naive stripper, but sufficient for extracting context
	cleanBody := stripTags(html)
	if len(cleanBody) > 2000 {
		cleanBody = cleanBody[:2000] + "..."
	}

	result := fmt.Sprintf("Title: %s\nDescription: %s\nHeaders: %s | %s\nSample Content: %s", title, metaDesc, h1, h2, cleanBody)
	return result, nil
}

func extractTag(html, tag string) string {
	re := regexp.MustCompile(fmt.Sprintf(`<%s[^>]*>(.*?)</%s>`, tag, tag))
	match := re.FindStringSubmatch(html)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func extractMeta(html, name string) string {
	re := regexp.MustCompile(fmt.Sprintf(`<meta[^>]*name=["']%s["'][^>]*content=["']([^"']*)["'][^>]*>`, name))
	match := re.FindStringSubmatch(html)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func stripTags(html string) string {
	// Remove script and style tags first
	reScript := regexp.MustCompile(`<(script|style)[^>]*>[\s\S]*?</(script|style)>`)
	html = reScript.ReplaceAllString(html, " ")

	// Remove all tags
	reTags := regexp.MustCompile(`<[^>]*>`)
	text := reTags.ReplaceAllString(html, " ")

	// Collapse whitespace
	reSpace := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(reSpace.ReplaceAllString(text, " "))
}
