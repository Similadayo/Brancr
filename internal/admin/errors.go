package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/similadayo/brancr/internal/middleware"
)

type adminErrorBody struct {
	Error     string         `json:"error"`
	Message   string         `json:"message,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

func isAdminAPIRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/admin/api/") || strings.HasPrefix(r.URL.Path, "/admin/ws/") {
		return true
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

func writeAdminError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	if w == nil {
		return
	}
	if status == 0 {
		status = http.StatusInternalServerError
	}

	// Expose a stable, machine-readable code for clients and for audit metadata.
	w.Header().Set("X-Brancr-Error-Code", code)

	if !isAdminAPIRequest(r) {
		http.Error(w, message, status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(adminErrorBody{
		Error:     code,
		Message:   message,
		Details:   details,
		RequestID: middleware.GetRequestID(r.Context()),
	})
}
