package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// reauth store (in-memory). For production, consider a persistent or shared store.
type reauthEntry struct {
	Token      string
	AdminEmail string
	ExpiresAt  time.Time
}

var (
	reauthMu    sync.Mutex
	reauthStore = map[string]reauthEntry{}
)

// PostReauthHandler verifies admin password and issues a short-lived reauth token.
func PostReauthHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminEmail := AdminEmailFromRequest(r)
		if adminEmail == "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		// Password can be provided as JSON {"password":"..."} or form value
		var payload struct {
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Password == "" {
			// fallback to form
			if err := r.ParseForm(); err == nil {
				payload.Password = r.FormValue("password")
			}
		}

		if ctx.Config == nil || ctx.Config.AdminPassword == "" {
			http.Error(w, "admin password not configured", http.StatusInternalServerError)
			return
		}
		if payload.Password != ctx.Config.AdminPassword {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		ttl := 15
		if ctx.Config != nil && ctx.Config.AdminReauthTTLMinutes > 0 {
			ttl = ctx.Config.AdminReauthTTLMinutes
		}

		token := randomToken(32)
		entry := reauthEntry{Token: token, AdminEmail: adminEmail, ExpiresAt: time.Now().Add(time.Duration(ttl) * time.Minute)}

		reauthMu.Lock()
		reauthStore[token] = entry
		reauthMu.Unlock()

		// Audit the reauth event
		ae := AdminAuditEntry{AdminEmail: adminEmail, AdminRole: AdminRoleFromRequest(r), Action: "admin.reauth", Metadata: map[string]interface{}{"method": "password"}}
		_ = auditWriter(r.Context(), ctx, ae)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"token": token, "expires_at": entry.ExpiresAt.Format(time.RFC3339)})
	}
}

// RequireRecentAuth ensures an admin has recently reauthenticated using PostReauthHandler.
func RequireRecentAuth(ctx *AdminContext) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			adminEmail := AdminEmailFromRequest(r)
			if adminEmail == "" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			token := r.Header.Get("X-Admin-Reauth-Token")
			if token == "" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			reauthMu.Lock()
			entry, ok := reauthStore[token]
			reauthMu.Unlock()
			if !ok || entry.AdminEmail != adminEmail || time.Now().After(entry.ExpiresAt) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
