package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	adminJTIKey = "admin_jti"
)

func adminJWTSecretStrings(ctx *AdminContext) []string {
	var secrets []string
	if ctx != nil && ctx.Config != nil {
		if s := strings.TrimSpace(ctx.Config.AdminJWTSecret); s != "" {
			secrets = append(secrets, s)
		}
		// Rotation support: accept previous secrets for verification.
		for _, s := range strings.Split(ctx.Config.AdminJWTPreviousSecrets, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			// Avoid duplicates.
			dup := false
			for _, existing := range secrets {
				if existing == s {
					dup = true
					break
				}
			}
			if !dup {
				secrets = append(secrets, s)
			}
		}
	}

	// Backwards compatibility: fall back to ADMIN_PASSWORD if ADMIN_JWT_SECRET is not set.
	if len(secrets) == 0 && ctx != nil {
		if s := strings.TrimSpace(ctx.AdminPassword); s != "" {
			secrets = append(secrets, s)
		} else if ctx.Config != nil {
			if s := strings.TrimSpace(ctx.Config.AdminPassword); s != "" {
				secrets = append(secrets, s)
			}
		}
	}
	if len(secrets) == 0 {
		panic("ADMIN_JWT_SECRET (recommended) or ADMIN_PASSWORD must be set for admin authentication")
	}
	return secrets
}

func kidForSecret(secret []byte) string {
	sum := sha256.Sum256(secret)
	// Short, stable identifier; not secret.
	return hex.EncodeToString(sum[:4])
}

func adminSigningSecret(ctx *AdminContext) []byte {
	return []byte(adminJWTSecretStrings(ctx)[0])
}

func createToken(ctx *AdminContext, adminEmail string, role string, exp time.Duration) (tokenStr string, jti string, err error) {
	jti = randomToken(16)
	claims := jwt.MapClaims{
		"sub":  adminEmail,
		"role": role,
		"jti":  jti,
		"exp":  time.Now().Add(exp).Unix(),
		"iat":  time.Now().Unix(),
		"iss":  "brancr",
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	secret := adminSigningSecret(ctx)
	t.Header["kid"] = kidForSecret(secret)
	tokenStr, err = t.SignedString(secret)
	return tokenStr, jti, err
}

func validateToken(ctx *AdminContext, reqCtx context.Context, tokenStr string) (*jwt.Token, jwt.MapClaims, error) {
	secrets := adminJWTSecretStrings(ctx)

	// Fast path: if token has a kid, try matching secret first.
	kid := ""
	if unverified, _, _ := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{}); unverified != nil {
		if v, ok := unverified.Header["kid"].(string); ok {
			kid = v
		}
	}

	ordered := make([]string, 0, len(secrets))
	if kid != "" {
		for _, s := range secrets {
			if kidForSecret([]byte(s)) == kid {
				ordered = append(ordered, s)
			}
		}
	}
	for _, s := range secrets {
		found := false
		for _, o := range ordered {
			if o == s {
				found = true
				break
			}
		}
		if !found {
			ordered = append(ordered, s)
		}
	}

	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
	var lastErr error
	for _, s := range ordered {
		tok, err := parser.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrTokenInvalidClaims
			}
			return []byte(s), nil
		})
		if err != nil || tok == nil || !tok.Valid {
			if err != nil {
				lastErr = err
			}
			continue
		}
		claims, ok := tok.Claims.(jwt.MapClaims)
		if !ok {
			return nil, nil, jwt.ErrTokenInvalidClaims
		}

		// If Redis is configured, require an active session entry for this token's jti.
		if ctx != nil && ctx.Redis != nil {
			jti, _ := claims["jti"].(string)
			if jti != "" {
				exists, rerr := ctx.Redis.Exists(reqCtx, adminSessionKey(jti)).Result()
				if rerr != nil && rerr != redis.Nil {
					// Fail open on Redis errors to avoid lockouts; still log the condition.
					if logger := GetAdminLoggerFromContext(reqCtx); logger != nil {
						logger.Warn("admin session store unavailable; skipping session enforcement", zap.Error(rerr))
					}
				} else if exists == 0 {
					return nil, nil, jwt.ErrTokenInvalidClaims
				}
			}
		}

		return tok, claims, nil
	}
	if lastErr == nil {
		lastErr = jwt.ErrTokenInvalidClaims
	}
	return nil, nil, lastErr
}

func AdminAuthMiddleware(ctx *AdminContext) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if this is an API or WebSocket request (should return JSON error, not redirect)
			isAPIorWS := strings.HasPrefix(r.URL.Path, "/admin/api/") ||
				strings.HasPrefix(r.URL.Path, "/admin/ws/") ||
				r.Header.Get("Upgrade") == "websocket"

			cookie, err := r.Cookie("brancr_admin")
			if err != nil || cookie.Value == "" {
				logger := GetAdminLogger(r)
				if logger != nil {
					logger.Warn("admin auth failed: no session cookie",
						zap.String("path", r.URL.Path),
						zap.String("remote_addr", r.RemoteAddr))
				}
				if isAPIorWS {
					writeAdminError(w, r, http.StatusUnauthorized, "unauthorized", "unauthorized", nil)
					return
				}
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			token, claims, err := validateToken(ctx, attachLoggerToContext(r.Context(), GetAdminLogger(r)), cookie.Value)
			if err != nil || token == nil || !token.Valid {
				logger := GetAdminLogger(r)
				if logger != nil {
					logger.Warn("admin auth failed: invalid token",
						zap.String("path", r.URL.Path),
						zap.Error(err))
				}
				clearAdminCookie(w)
				if isAPIorWS {
					writeAdminError(w, r, http.StatusUnauthorized, "unauthorized", "unauthorized", nil)
					return
				}
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			// Extract admin email from token claims
			email, _ := claims["sub"].(string)
			role, _ := claims["role"].(string)
			jti, _ := claims["jti"].(string)
			if email != "" {
				// Store admin email in context for logging
				r = r.WithContext(context.WithValue(r.Context(), adminEmailKey, email))
			}
			if jti != "" {
				r = r.WithContext(context.WithValue(r.Context(), adminJTIKey, jti))
			}

			// If AdminUser exists in DB, enforce active + role.
			// Use request context with timeout for DB query
			if email != "" && ctx.DB != nil {
				reqCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()

				var adminUser struct {
					Email  string
					Role   string
					Active bool
				}
				if err := ctx.DB.WithContext(reqCtx).Table("admin_users").Select("email, role, active").Where("email = ?", email).First(&adminUser).Error; err == nil {
					if !adminUser.Active {
						clearAdminCookie(w)
						if isAPIorWS {
							writeAdminError(w, r, http.StatusUnauthorized, "account_disabled", "account disabled", nil)
							return
						}
						http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
						return
					}
					role = adminUser.Role
				}
			}
			if role != "" {
				r = r.WithContext(context.WithValue(r.Context(), adminRoleKey, role))
			}

			r = r.WithContext(context.WithValue(r.Context(), "admin_token", token))
			next.ServeHTTP(w, r)
		})
	}
}

func setAdminCookie(w http.ResponseWriter, token string, maxAgeSeconds int) {
	if maxAgeSeconds <= 0 {
		maxAgeSeconds = 3600
	}
	c := &http.Cookie{
		Name:     "brancr_admin",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   maxAgeSeconds,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	}
	http.SetCookie(w, c)
}

func clearAdminCookie(w http.ResponseWriter) {
	c := &http.Cookie{
		Name:     "brancr_admin",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	}
	http.SetCookie(w, c)
}

func AdminJTIFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v, ok := r.Context().Value(adminJTIKey).(string); ok {
		return v
	}
	return ""
}

func adminSessionKey(jti string) string {
	return "admin:sess:" + jti
}

func adminSessionMetaKey(jti string) string {
	return "admin:sessmeta:" + jti
}

func adminUserSessionsKey(email string) string {
	return "admin:user_sessions:" + strings.ToLower(strings.TrimSpace(email))
}

type adminLoggerFromContextKey struct{}

func GetAdminLoggerFromContext(ctx context.Context) *zap.Logger {
	if ctx == nil {
		return nil
	}
	if logger, ok := ctx.Value(adminLoggerFromContextKey{}).(*zap.Logger); ok {
		return logger
	}
	return nil
}

// attachLoggerToContext allows token validation to log without an *http.Request.
func attachLoggerToContext(ctx context.Context, logger *zap.Logger) context.Context {
	if ctx == nil || logger == nil {
		return ctx
	}
	return context.WithValue(ctx, adminLoggerFromContextKey{}, logger)
}

func storeAdminSession(ctx *AdminContext, reqCtx context.Context, jti, email, role, ip, userAgent, requestID string, exp time.Duration) {
	if ctx == nil || ctx.Redis == nil || jti == "" || email == "" {
		return
	}
	_ = ctx.Redis.Set(reqCtx, adminSessionKey(jti), email, exp).Err()

	meta := map[string]any{
		"jti":        jti,
		"email":      email,
		"role":       role,
		"ip":         ip,
		"user_agent": userAgent,
		"request_id": requestID,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	if metaBytes, err := json.Marshal(meta); err == nil {
		_ = ctx.Redis.Set(reqCtx, adminSessionMetaKey(jti), metaBytes, exp).Err()
	}

	// Best-effort: keep a set of active sessions for the user.
	_ = ctx.Redis.SAdd(reqCtx, adminUserSessionsKey(email), jti).Err()
	_ = ctx.Redis.Expire(reqCtx, adminUserSessionsKey(email), 30*24*time.Hour).Err()
}

func deleteAdminSession(ctx *AdminContext, reqCtx context.Context, jti, email string) {
	if ctx == nil || ctx.Redis == nil || jti == "" {
		return
	}
	_ = ctx.Redis.Del(reqCtx, adminSessionKey(jti), adminSessionMetaKey(jti)).Err()
	if email != "" {
		_ = ctx.Redis.SRem(reqCtx, adminUserSessionsKey(email), jti).Err()
	}
}
