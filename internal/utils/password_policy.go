package utils

import (
	"errors"
	"strings"
	"unicode"
)

// PasswordPolicy defines password requirements
type PasswordPolicy struct {
	MinLength        int
	RequireUppercase bool
	RequireLowercase bool
	RequireNumber    bool
	RequireSpecial   bool
}

// DefaultPasswordPolicy returns recommended password policy
func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{
		MinLength:        8,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
		RequireSpecial:   false, // Optional for better UX
	}
}

// ValidatePassword checks if password meets policy requirements
func ValidatePassword(password string, policy PasswordPolicy) error {
	if len(password) < policy.MinLength {
		return errors.New("password must be at least 8 characters")
	}

	var hasUpper, hasLower, hasNumber, hasSpecial bool

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	var missing []string
	if policy.RequireUppercase && !hasUpper {
		missing = append(missing, "uppercase letter")
	}
	if policy.RequireLowercase && !hasLower {
		missing = append(missing, "lowercase letter")
	}
	if policy.RequireNumber && !hasNumber {
		missing = append(missing, "number")
	}
	if policy.RequireSpecial && !hasSpecial {
		missing = append(missing, "special character")
	}

	if len(missing) > 0 {
		return errors.New("password must include: " + strings.Join(missing, ", "))
	}

	// Check for common weak patterns
	commonPatterns := []string{
		"password", "123456", "qwerty", "admin", "letmein",
		"welcome", "monkey", "dragon", "master", "login",
	}
	lowerPass := strings.ToLower(password)
	for _, pattern := range commonPatterns {
		if strings.Contains(lowerPass, pattern) {
			return errors.New("password contains a common weak pattern")
		}
	}

	return nil
}

// ValidatePasswordDefault validates password against default policy
func ValidatePasswordDefault(password string) error {
	return ValidatePassword(password, DefaultPasswordPolicy())
}
