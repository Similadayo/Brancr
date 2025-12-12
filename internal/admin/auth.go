package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

func jwtSecret(ctx *AdminContext) []byte {
	secret := ctx.AdminPassword
	if secret == "" {
		secret = "dev_brancr_secret"
	}
	return []byte(secret)
}

func createToken(ctx *AdminContext, adminEmail string, role string, exp time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"sub":  adminEmail,
		"role": role,
		"exp":  time.Now().Add(exp).Unix(),
		"iat":  time.Now().Unix(),
		"iss":  "brancr",
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(jwtSecret(ctx))
}

func validateToken(ctx *AdminContext, tokenStr string) (*jwt.Token, error) {
	return jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenInvalidClaims
		}
		return jwtSecret(ctx), nil
	})
}

func AdminAuthMiddleware(ctx *AdminContext) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("brancr_admin")
			if err != nil || cookie.Value == "" {
				logger := GetAdminLogger(r)
				if logger != nil {
					logger.Warn("admin auth failed: no session cookie",
						zap.String("path", r.URL.Path),
						zap.String("remote_addr", r.RemoteAddr))
				}
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			token, err := validateToken(ctx, cookie.Value)
			if err != nil || !token.Valid {
				logger := GetAdminLogger(r)
				if logger != nil {
					logger.Warn("admin auth failed: invalid token",
						zap.String("path", r.URL.Path),
						zap.Error(err))
				}
				clearAdminCookie(w)
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			// Extract admin email from token claims
			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				if email, ok := claims["sub"].(string); ok {
					// Store admin email in context for logging
					r = r.WithContext(context.WithValue(r.Context(), "admin_email", email))

					// If AdminUser exists in DB, enforce active + role.
					if ctx.DB != nil {
						var adminUser struct {
							Email  string
							Role   string
							Active bool
						}
						if err := ctx.DB.Table("admin_users").Select("email, role, active").Where("email = ?", email).First(&adminUser).Error; err == nil {
							if !adminUser.Active {
								clearAdminCookie(w)
								http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
								return
							}
							r = r.WithContext(context.WithValue(r.Context(), adminRoleKey, adminUser.Role))
						} else if role, ok := claims["role"].(string); ok {
							r = r.WithContext(context.WithValue(r.Context(), adminRoleKey, role))
						}
					} else if role, ok := claims["role"].(string); ok {
						r = r.WithContext(context.WithValue(r.Context(), adminRoleKey, role))
					}
				}
			}

			r = r.WithContext(context.WithValue(r.Context(), "admin_token", token))
			next.ServeHTTP(w, r)
		})
	}
}

func setAdminCookie(w http.ResponseWriter, token string) {
	c := &http.Cookie{
		Name:     "brancr_admin",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400,
		SameSite: http.SameSiteLaxMode,
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
	}
	http.SetCookie(w, c)
}
