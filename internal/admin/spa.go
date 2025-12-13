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

		// SPA fallback
		http.ServeFile(w, r, indexPath)
	}
}

func newCopyURL(r *http.Request) *url.URL {
	clone := *r.URL
	return &clone
}
