package utils

import (
	"fmt"

	phonenumbers "github.com/nyaruka/phonenumbers"
)

// NormalizePhone takes a raw phone string and returns canonical E.164 format or an error
func NormalizePhone(raw string, defaultRegion string) (string, error) {
	num, err := phonenumbers.Parse(raw, defaultRegion)
	if err != nil {
		return "", err
	}
	if !phonenumbers.IsValidNumber(num) {
		return "", fmt.Errorf("invalid phone number")
	}
	return phonenumbers.Format(num, phonenumbers.E164), nil
}

// IsValidPhone checks validity without formatting
func IsValidPhone(raw string, defaultRegion string) bool {
	num, err := phonenumbers.Parse(raw, defaultRegion)
	if err != nil {
		return false
	}
	return phonenumbers.IsValidNumber(num)
}
