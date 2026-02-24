package middleware

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTSecret returns the JWT secret key
type JWTSecret func() []byte

// CreateToken creates a JWT token with the given email and expiration
func CreateToken(secretFunc JWTSecret, adminEmail string, exp time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"sub": adminEmail,
		"exp": time.Now().Add(exp).Unix(),
		"iat": time.Now().Unix(),
		"iss": "brancr",
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(secretFunc())
}

// ValidateToken validates a JWT token
func ValidateToken(secretFunc JWTSecret, tokenStr string) (*jwt.Token, error) {
	return jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenInvalidClaims
		}
		return secretFunc(), nil
	})
}
