// internal/utils/public_url.go
package utils

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
)

func GetPublicURL() (string, error) {
	// Try local Cloudflared API
	resp, err := http.Get("http://127.0.0.1:4040/api/tunnels")
	if err == nil {
		defer resp.Body.Close()
		var result struct {
			Tunnels []struct {
				PublicURL string `json:"public_url"`
			} `json:"tunnels"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
			if len(result.Tunnels) > 0 && result.Tunnels[0].PublicURL != "" {
				return result.Tunnels[0].PublicURL, nil
			}
		}
	}

	// Fallback to environment variable
	if val := os.Getenv("BRANCR_PUBLIC_URL"); val != "" {
		return val, nil
	}

	return "", errors.New("no public Cloudflared URL found (is tunnel running?)")
}
