package utils

import (
	"golang.org/x/crypto/bcrypt"
)

// HashToken securely hashes sensitive tokens before saving
func HashToken(token string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	return string(bytes), err
}

// CompareToken verifies a raw token with its hashed value
func CompareToken(raw, hashed string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(raw))
	return err == nil
}
