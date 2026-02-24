package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type tenantContextKey struct{}

// TenantClaims represents JWT claims for tenant sessions.
type TenantClaims struct {
	TenantID uuid.UUID `json:"tenant_id"`
	Email    string    `json:"email"`
	jwt.RegisteredClaims
}

// CreateTenantToken issues a JWT for the given tenant ID and email.
func CreateTenantToken(secret []byte, tenantID uuid.UUID, email string, ttl time.Duration) (string, error) {
	claims := TenantClaims{
		TenantID: tenantID,
		Email:    email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "brancr-tenant",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// ParseTenantToken validates the JWT and returns decoded claims.
func ParseTenantToken(secret []byte, tokenStr string) (*TenantClaims, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
	token, err := parser.ParseWithClaims(tokenStr, &TenantClaims{}, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*TenantClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, jwt.ErrTokenInvalidClaims
}

// TenantAuth middleware ensures the request has a valid tenant session cookie.
func TenantAuth(secret []byte, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenSource := "cookie"
			var token string

			cookie, err := r.Cookie(cookieName)
			if err == nil && strings.TrimSpace(cookie.Value) != "" {
				token = strings.TrimSpace(cookie.Value)
			} else {
				authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
				lower := strings.ToLower(authHeader)
				if strings.HasPrefix(lower, "bearer ") {
					token = strings.TrimSpace(authHeader[len("bearer "):])
					tokenSource = "bearer"
				}
			}

			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			claims, err := ParseTenantToken(secret, token)
			if err != nil {
				if tokenSource == "cookie" {
					// Clear stale/invalid cookie
					ClearSessionCookieForRequest(w, r, cookieName)
				}
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), tenantContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClearSessionCookie removes the session cookie from the client
func ClearSessionCookie(w http.ResponseWriter, cookieName string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // Delete immediately
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})
}

func ClearSessionCookieForRequest(w http.ResponseWriter, r *http.Request, cookieName string) {
	ClearSessionCookie(w, cookieName)

	host := r.Host
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return
	}

	domain := host
	if strings.Count(domain, ".") >= 2 {
		parts := strings.Split(domain, ".")
		domain = strings.Join(parts[len(parts)-2:], ".")
	}
	domain = "." + domain

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})
}

// TenantFromContext retrieves tenant JWT claims from context.
func TenantFromContext(ctx context.Context) (*TenantClaims, bool) {
	claims, ok := ctx.Value(tenantContextKey{}).(*TenantClaims)
	return claims, ok
}

// WithTenantContext adds tenant claims to context (useful for testing)
func WithTenantContext(ctx context.Context, claims *TenantClaims) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, claims)
}

// TenantExistsCheck middleware verifies that the tenant from the JWT still exists in the database.
// This should be used AFTER TenantAuth middleware.
// If tenant doesn't exist, clears session cookie and returns 401.
func TenantExistsCheck(tenantChecker func(tenantID uuid.UUID) bool, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := TenantFromContext(r.Context())
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if tenant still exists
			if !tenantChecker(claims.TenantID) {
				// Tenant was deleted - clear the stale cookie
				ClearSessionCookieForRequest(w, r, cookieName)
				http.Error(w, `{"error":"session_invalid","message":"Your session is no longer valid. Please log in again."}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
