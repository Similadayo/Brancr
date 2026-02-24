package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type adminSessionInfo struct {
	JTI        string         `json:"jti"`
	Email      string         `json:"email"`
	Role       string         `json:"role,omitempty"`
	IP         string         `json:"ip,omitempty"`
	UserAgent  string         `json:"user_agent,omitempty"`
	CreatedAt  string         `json:"created_at,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	TTLSeconds int64          `json:"ttl_seconds"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"`
	Meta       map[string]any `json:"meta,omitempty"`
}

// GET /admin/api/sessions[?email=...]
func AdminSessionsListHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ctx == nil || ctx.Redis == nil {
			writeAdminError(w, r, http.StatusNotImplemented, "sessions_unavailable", "session store not configured", nil)
			return
		}

		requester := strings.ToLower(strings.TrimSpace(AdminEmailFromRequest(r)))
		requesterRole := AdminRoleFromRequest(r)
		target := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
		if target == "" {
			target = requester
		}
		if requester == "" {
			writeAdminError(w, r, http.StatusForbidden, "forbidden", "forbidden", nil)
			return
		}
		if target != requester && requesterRole != RoleSuperAdmin {
			writeAdminError(w, r, http.StatusForbidden, "permission_denied", "forbidden", nil)
			return
		}

		jtis, err := ctx.Redis.SMembers(r.Context(), adminUserSessionsKey(target)).Result()
		if err != nil && err != redis.Nil {
			writeAdminError(w, r, http.StatusInternalServerError, "session_store_error", "failed to list sessions", nil)
			return
		}

		pipe := ctx.Redis.Pipeline()
		type pending struct {
			jti      string
			metaCmd  *redis.StringCmd
			ttlCmd   *redis.DurationCmd
			emailCmd *redis.StringCmd
		}
		pendingCmds := make([]pending, 0, len(jtis))
		for _, jti := range jtis {
			jti = strings.TrimSpace(jti)
			if jti == "" {
				continue
			}
			pendingCmds = append(pendingCmds, pending{
				jti:      jti,
				metaCmd:  pipe.Get(r.Context(), adminSessionMetaKey(jti)),
				ttlCmd:   pipe.TTL(r.Context(), adminSessionKey(jti)),
				emailCmd: pipe.Get(r.Context(), adminSessionKey(jti)),
			})
		}
		_, _ = pipe.Exec(r.Context())

		sessions := make([]adminSessionInfo, 0, len(pendingCmds))
		stale := make([]string, 0)

		for _, p := range pendingCmds {
			email, _ := p.emailCmd.Result()
			ttl, _ := p.ttlCmd.Result()
			if email == "" || ttl <= 0 {
				stale = append(stale, p.jti)
				continue
			}

			info := adminSessionInfo{
				JTI:        p.jti,
				Email:      email,
				TTLSeconds: int64(ttl / time.Second),
			}

			if metaStr, err := p.metaCmd.Result(); err == nil && metaStr != "" {
				var meta map[string]any
				if json.Unmarshal([]byte(metaStr), &meta) == nil {
					if v, ok := meta["role"].(string); ok {
						info.Role = v
					}
					if v, ok := meta["ip"].(string); ok {
						info.IP = v
					}
					if v, ok := meta["user_agent"].(string); ok {
						info.UserAgent = v
					}
					if v, ok := meta["created_at"].(string); ok {
						info.CreatedAt = v
					}
					if v, ok := meta["request_id"].(string); ok {
						info.RequestID = v
					}
					info.Meta = meta
				}
			}

			exp := time.Now().UTC().Add(ttl)
			info.ExpiresAt = &exp
			sessions = append(sessions, info)
		}

		// Best-effort cleanup of expired sessions in the user's set.
		if len(stale) > 0 {
			_ = ctx.Redis.SRem(r.Context(), adminUserSessionsKey(target), stale).Err()
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"email":    target,
			"sessions": sessions,
			"total":    len(sessions),
		})
	}
}

// DELETE /admin/api/sessions/{jti}
func AdminSessionsRevokeHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ctx == nil || ctx.Redis == nil {
			writeAdminError(w, r, http.StatusNotImplemented, "sessions_unavailable", "session store not configured", nil)
			return
		}

		jti := strings.TrimSpace(chi.URLParam(r, "jti"))
		if jti == "" {
			writeAdminError(w, r, http.StatusBadRequest, "bad_request", "missing jti", nil)
			return
		}

		requester := strings.ToLower(strings.TrimSpace(AdminEmailFromRequest(r)))
		requesterRole := AdminRoleFromRequest(r)
		if requester == "" {
			writeAdminError(w, r, http.StatusForbidden, "forbidden", "forbidden", nil)
			return
		}

		email, err := ctx.Redis.Get(r.Context(), adminSessionKey(jti)).Result()
		if err != nil || email == "" {
			writeAdminError(w, r, http.StatusNotFound, "not_found", "session not found", nil)
			return
		}
		email = strings.ToLower(strings.TrimSpace(email))
		if email != requester && requesterRole != RoleSuperAdmin {
			writeAdminError(w, r, http.StatusForbidden, "permission_denied", "forbidden", nil)
			return
		}

		deleteAdminSession(ctx, r.Context(), jti, email)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"revoked": true,
			"jti":     jti,
			"email":   email,
		})
	}
}

// POST /admin/api/sessions/revoke-all  (JSON: {"email":"..."}; email optional for self)
func AdminSessionsRevokeAllHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ctx == nil || ctx.Redis == nil {
			writeAdminError(w, r, http.StatusNotImplemented, "sessions_unavailable", "session store not configured", nil)
			return
		}

		requester := strings.ToLower(strings.TrimSpace(AdminEmailFromRequest(r)))
		requesterRole := AdminRoleFromRequest(r)
		if requester == "" {
			writeAdminError(w, r, http.StatusForbidden, "forbidden", "forbidden", nil)
			return
		}

		var payload struct {
			Email string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		target := strings.ToLower(strings.TrimSpace(payload.Email))
		if target == "" {
			target = requester
		}
		if target != requester && requesterRole != RoleSuperAdmin {
			writeAdminError(w, r, http.StatusForbidden, "permission_denied", "forbidden", nil)
			return
		}

		jtis, err := ctx.Redis.SMembers(r.Context(), adminUserSessionsKey(target)).Result()
		if err != nil && err != redis.Nil {
			writeAdminError(w, r, http.StatusInternalServerError, "session_store_error", "failed to list sessions", nil)
			return
		}

		revoked := 0
		for _, jti := range jtis {
			jti = strings.TrimSpace(jti)
			if jti == "" {
				continue
			}
			deleteAdminSession(ctx, r.Context(), jti, target)
			revoked++
		}
		_ = ctx.Redis.Del(r.Context(), adminUserSessionsKey(target)).Err()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"revoked": revoked,
			"email":   target,
		})
	}
}
