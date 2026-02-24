package tenantapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
)

// ========================================
// EMAIL VERIFICATION HANDLERS
// ========================================

// verifyEmailRequest request body for email verification
type verifyEmailRequest struct {
	Token string `json:"token"`
}

// resendVerificationRequest request body for resending verification email
type resendVerificationRequest struct {
	Email string `json:"email"`
}

// sendVerificationEmail is an internal helper to generate token and send verification email
// Returns the generated token for testing purposes
func (h *Handler) sendVerificationEmail(email string, tenantID string) error {
	// Defensive nil checks to prevent panics
	if h == nil {
		return fmt.Errorf("handler is nil")
	}

	// Generate verification token (32 bytes = 64 hex chars)
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	// Store in Redis with 24-hour TTL
	key := fmt.Sprintf("email_verify:%s", token)
	if h.Cache != nil {
		ctx := context.Background() // Use proper context, not nil
		if err := h.Cache.SetWithTTL(ctx, key, email, 24*time.Hour); err != nil {
			if h.Logger != nil {
				h.Logger.Error("sendVerificationEmail: failed to store token", zap.Error(err))
			}
			return err
		}
	} else {
		if h.Logger != nil {
			h.Logger.Error("sendVerificationEmail: cache service not available")
		}
		return fmt.Errorf("cache service unavailable")
	}

	// Build verification link
	frontendURL := "https://brancr.com" // Default
	if h.Config != nil && h.Config.FrontendURL != "" {
		frontendURL = h.Config.FrontendURL
	}
	frontendURL = strings.TrimRight(frontendURL, "/")
	verifyLink := fmt.Sprintf("%s/auth/verify-email?token=%s", frontendURL, token)

	// Send email (async)
	if h.EmailService != nil {
		go func() {
			if err := h.EmailService.SendEmailVerification(email, verifyLink); err != nil {
				if h.Logger != nil {
					h.Logger.Error("failed to send verification email", zap.Error(err), zap.String("email", email))
				}
			}
		}()
	} else {
		if h.Logger != nil {
			h.Logger.Warn("email service not configured, printing verification link", zap.String("link", verifyLink))
		}
	}

	return nil
}

// verifyEmail verifies the email using the token
// @Summary      Verify email address
// @Description  Verify email using the token from verification link
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      verifyEmailRequest  true  "Verify Email request"
// @Success      200     {object}  map[string]interface{}  "Email verified"
// @Failure      400     {object}  map[string]interface{}  "Invalid or expired token"
// @Router       /auth/verify-email [post]
func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeValidationError(w, r, "invalid request body", nil)
		return
	}

	if req.Token == "" {
		h.writeValidationError(w, r, "token is required", nil)
		return
	}

	// Lookup token in Redis
	key := fmt.Sprintf("email_verify:%s", req.Token)
	var email string
	if h.Cache != nil {
		if err := h.Cache.Get(r.Context(), key, &email); err != nil || email == "" {
			h.writeValidationError(w, r, "Invalid or expired verification link", nil)
			return
		}
	} else {
		h.writeInternalError(w, r, fmt.Errorf("cache service unavailable"))
		return
	}

	// Update tenant's email_verified status
	now := time.Now()
	result := h.DB.Model(&storage.Tenant{}).
		Where("email = ?", email).
		Updates(map[string]interface{}{
			"email_verified":    true,
			"email_verified_at": now,
		})

	if result.Error != nil {
		h.Logger.Error("verifyEmail: failed to update tenant", zap.Error(result.Error))
		h.writeInternalError(w, r, result.Error)
		return
	}

	if result.RowsAffected == 0 {
		h.writeValidationError(w, r, "Account not found", nil)
		return
	}

	// Delete the token (one-time use)
	if h.Cache != nil {
		h.Cache.Delete(r.Context(), key)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Email verified successfully! You can now access all features.",
	})
}

// resendVerificationEmail resends the verification email
// @Summary      Resend verification email
// @Description  Resend email verification link to user
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      resendVerificationRequest  true  "Resend Verification request"
// @Success      200     {object}  map[string]interface{}  "Verification email sent"
// @Router       /auth/resend-verification [post]
func (h *Handler) resendVerificationEmail(w http.ResponseWriter, r *http.Request) {
	var req resendVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeValidationError(w, r, "invalid request body", nil)
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		h.writeValidationError(w, r, "email is required", nil)
		return
	}

	// Check if tenant exists and is not already verified
	var tenant storage.Tenant
	if err := h.DB.Where("email = ?", email).First(&tenant).Error; err != nil {
		// Don't reveal if email exists for security
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "If an account exists with this email, we have sent a verification link.",
		})
		return
	}

	// If already verified, still return success (don't reveal status)
	if tenant.EmailVerified {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "If an account exists with this email, we have sent a verification link.",
		})
		return
	}

	// Send verification email
	if err := h.sendVerificationEmail(email, tenant.ID.String()); err != nil {
		h.Logger.Error("resendVerificationEmail: failed to send", zap.Error(err))
		// Still return success to not reveal internal errors
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "If an account exists with this email, we have sent a verification link.",
	})
}

// checkEmailVerified is a helper to check if tenant's email is verified
// Returns true if verified, false otherwise
func (h *Handler) checkEmailVerified(tenantID string) bool {
	var tenant storage.Tenant
	if err := h.DB.Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		return false
	}
	return tenant.EmailVerified
}
