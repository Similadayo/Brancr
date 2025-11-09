package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type tenantContextKey struct{}

// TenantClaims represents JWT claims for tenant sessions.
type TenantClaims struct {
	TenantID uint   `json:"tenant_id"`
	Email    string `json:"email"`
	jwt.RegisteredClaims
}

// CreateTenantToken issues a JWT for the given tenant ID and email.
func CreateTenantToken(secret []byte, tenantID uint, email string, ttl time.Duration) (string, error) {
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
			cookie, err := r.Cookie(cookieName)
			if err != nil || cookie.Value == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			token := strings.TrimSpace(cookie.Value)
			claims, err := ParseTenantToken(secret, token)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), tenantContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantFromContext retrieves tenant JWT claims from context.
func TenantFromContext(ctx context.Context) (*TenantClaims, bool) {
	claims, ok := ctx.Value(tenantContextKey{}).(*TenantClaims)
	return claims, ok
}
