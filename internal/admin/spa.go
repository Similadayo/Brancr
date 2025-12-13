package admin

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// AdminSPAServe serves a pre-built SPA from distDir.
//
// It expects to be mounted under /admin and routed via /app and /app/*.
// Non-asset routes fall back to index.html.
func AdminSPAServe(distDir string) http.HandlerFunc {
	indexPath := filepath.Join(distDir, "index.html")
	fs := http.FileServer(http.Dir(distDir))

	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(indexPath); err != nil {
			http.Error(w, "admin app not built (missing adminapp/dist). Run: cd adminapp && npm install && npm run build", http.StatusServiceUnavailable)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/app")
		if path == "" || path == "/" {
			// Avoid serving stale index.html which can reference non-existent hashed assets.
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			http.ServeFile(w, r, indexPath)
			return
		}

		rel := strings.TrimPrefix(path, "/")
		rel = filepath.Clean(filepath.FromSlash(rel))
		if rel == "." || strings.HasPrefix(rel, "..") {
			http.ServeFile(w, r, indexPath)
			return
		}
		candidate := filepath.Join(distDir, rel)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			clone := new(http.Request)
			*clone = *r
			clone.URL = newCopyURL(r)
			clone.URL.Path = "/" + strings.ReplaceAll(rel, string(filepath.Separator), "/")
			fs.ServeHTTP(w, clone)
			return
		}

		// If this looks like a static asset request, do NOT fall back to index.html.
		// Returning HTML for a .css/.js request triggers strict MIME errors in browsers.
		if looksLikeAssetRequest(rel) {
			http.NotFound(w, r)
			return
		}

		// SPA fallback for client-side routes
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		http.ServeFile(w, r, indexPath)
	}
}

// AdminSPAAssetsServe serves only built assets (no SPA fallback).
// Intended to be mounted at /admin/app/assets/* (router path: /app/assets/*).
func AdminSPAAssetsServe(distDir string) http.HandlerFunc {
	indexPath := filepath.Join(distDir, "index.html")
	fs := http.FileServer(http.Dir(distDir))

	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(indexPath); err != nil {
			http.Error(w, "admin app not built (missing adminapp/dist)", http.StatusServiceUnavailable)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/app")
		rel := strings.TrimPrefix(path, "/")
		rel = filepath.Clean(filepath.FromSlash(rel))
		if rel == "." || strings.HasPrefix(rel, "..") {
			http.NotFound(w, r)
			return
		}

		candidate := filepath.Join(distDir, rel)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			clone := new(http.Request)
			*clone = *r
			clone.URL = newCopyURL(r)
			clone.URL.Path = "/" + strings.ReplaceAll(rel, string(filepath.Separator), "/")
			fs.ServeHTTP(w, clone)
			return
		}

		http.NotFound(w, r)
	}
}

func looksLikeAssetRequest(rel string) bool {
	// Vite outputs hashed assets under dist/assets/...
	if strings.HasPrefix(rel, "assets/") {
		return true
	}
	// Any file extension is treated as an asset request.
	base := filepath.Base(rel)
	if strings.Contains(base, ".") {
		return true
	}
	return false
}

func newCopyURL(r *http.Request) *url.URL {
	clone := *r.URL
	return &clone
}
