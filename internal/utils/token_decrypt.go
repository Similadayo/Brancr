package utils

import (
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DecryptTokenWithNotification attempts to decrypt a token and logs/notifies on failure
// Returns the decrypted token or falls back to plain text if decryption fails
func DecryptTokenWithNotification(encoded string, logger *zap.Logger, tenantID uuid.UUID, platform string) (string, error) {
	decrypted, err := DecryptToken(encoded)
	if err != nil {
		// Log the decryption failure
		if logger != nil {
			logger.Warn("token decryption failed, using as plain text",
				zap.Stringer("tenant_id", tenantID),
				zap.String("platform", platform),
				zap.Error(err))
		}
		// Return the token as-is (might be plain text)
		// The caller should handle the error appropriately
		return encoded, fmt.Errorf("decryption failed, token might be plain text or corrupted: %w", err)
	}
	return decrypted, nil
}
