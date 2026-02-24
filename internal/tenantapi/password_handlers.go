package tenantapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// forgotPasswordRequest request body
type forgotPasswordRequest struct {
	Email string `json:"email" example:"john@example.com"`
}

// forgotPassword initiates the password reset flow
// @Summary      Request password reset
// @Description  Send a password reset link to the user's email
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      forgotPasswordRequest  true  "Forgot Password request"
// @Success      200     {object}  map[string]interface{}  "If email exists, email sent"
// @Router       /auth/forgot-password [post]
func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeValidationError(w, r, "invalid request body", nil)
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		h.writeValidationError(w, r, "email is required", nil)
		return
	}

	// 1. Verify email exists (but don't reveal existence to caller)
	var tenant storage.Tenant
	err := h.DB.Where("email = ?", email).First(&tenant).Error

	if err == nil {
		// Tenant exists
		// 2. Generate Reset Token
		tokenBytes := make([]byte, 32)
		rand.Read(tokenBytes)
		token := hex.EncodeToString(tokenBytes)

		// 3. Store in Redis (15 minutes TTL)
		key := fmt.Sprintf("reset_token:%s", token)
		// We store the email as the value
		if h.Cache != nil {
			if err := h.Cache.SetWithTTL(r.Context(), key, email, 15*time.Minute); err != nil {
				h.Logger.Error("forgotPassword: failed to store token in redis", zap.Error(err))
				h.writeInternalError(w, r, err)
				return
			}
		} else {
			h.Logger.Error("forgotPassword: cache service not available")
			h.writeInternalError(w, r, fmt.Errorf("service unavailable"))
			return
		}

		// 4. Send Email
		frontendURL := h.Config.FrontendURL
		if frontendURL == "" {
			frontendURL = "https://brancr.com"
		}

		// Ensure no double slashes
		frontendURL = strings.TrimRight(frontendURL, "/")
		resetLink := fmt.Sprintf("%s/auth/reset-password?token=%s", frontendURL, token)

		if h.EmailService != nil {
			go func() {
				if err := h.EmailService.SendPasswordResetEmail(email, resetLink); err != nil {
					h.Logger.Error("failed to send reset email", zap.Error(err), zap.String("email", email))
				}
			}()
		} else {
			h.Logger.Warn("email service not configured, printing link locally", zap.String("link", resetLink))
		}
	} else {
		// Log that we couldn't find email, but proceed to return 200 security
		h.Logger.Info("forgotPassword: email not found", zap.String("email", email))
	}

	// Always return success message to prevent email enumeration
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "If an account exists with this email, we have sent a password reset link.",
	})
}

// resetPasswordRequest request body
type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// resetPassword completes the password reset flow
// @Summary      Reset password
// @Description  Reset password using a valid token
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      resetPasswordRequest  true  "Reset Password request"
// @Success      200     {object}  map[string]interface{}  "Password updated"
// @Failure      400     {object}  map[string]interface{}  "Invalid token"
// @Router       /auth/reset-password [post]
func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeValidationError(w, r, "invalid request body", nil)
		return
	}

	if req.Token == "" || req.NewPassword == "" {
		h.writeValidationError(w, r, "token and new_password are required", nil)
		return
	}

	if err := utils.ValidatePasswordDefault(req.NewPassword); err != nil {
		h.writeValidationError(w, r, err.Error(), nil)
		return
	}

	// 1. Verify Token from Redis
	key := fmt.Sprintf("reset_token:%s", req.Token)
	var email string
	if h.Cache != nil {
		if err := h.Cache.Get(r.Context(), key, &email); err != nil {
			// Redis miss or error implies invalid/expired token
			h.writeValidationError(w, r, "Invalid or expired reset link", nil)
			return
		}
	} else {
		h.writeInternalError(w, r, fmt.Errorf("service unavailable"))
		return
	}

	if email == "" {
		h.writeValidationError(w, r, "Invalid or expired reset link", nil)
		return
	}

	// 2. Update Password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		h.writeInternalError(w, r, err)
		return
	}

	if err := h.DB.Model(&storage.Tenant{}).Where("email = ?", email).Update("password_hash", string(hash)).Error; err != nil {
		h.Logger.Error("resetPassword: failed to update password", zap.Error(err))
		h.writeInternalError(w, r, err)
		return
	}

	// 3. Delete Token (Consume it)
	if h.Cache != nil {
		h.Cache.Delete(r.Context(), key)
	}

	// 4. Send password changed alert (fetch tenant to get ID)
	var tenant storage.Tenant
	if err := h.DB.Where("email = ?", email).First(&tenant).Error; err == nil && h.AlertTriggers != nil {
		h.AlertTriggers.OnPasswordChanged(r.Context(), tenant.ID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Password successfully reset. You can now login.",
	})
}

// changePasswordRequest request body
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// changePassword updates the password for logged in user
// @Summary      Change password
// @Description  Update password for authenticated user
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      changePasswordRequest  true  "Change Password request"
// @Success      200     {object}  map[string]interface{}  "Password updated"
// @Router       /auth/change-password [post]
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		h.writeUnauthorizedError(w, r, "unauthorized")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeValidationError(w, r, "invalid request body", nil)
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		h.writeValidationError(w, r, "current_password and new_password are required", nil)
		return
	}

	if err := utils.ValidatePasswordDefault(req.NewPassword); err != nil {
		h.writeValidationError(w, r, err.Error(), nil)
		return
	}

	// 1. Verify Current Password
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, claims.TenantID).Error; err != nil {
		h.writeUnauthorizedError(w, r, "user not found")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(tenant.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		h.writeUnauthorizedError(w, r, "Incorrect current password")
		return
	}

	// 2. Update to New Password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		h.writeInternalError(w, r, err)
		return
	}

	if err := h.DB.Model(&tenant).Update("password_hash", string(hash)).Error; err != nil {
		h.writeInternalError(w, r, err)
		return
	}

	// 3. Optional: Invalidate all sessions except current?
	// For now we don't track session IDs individually (cookie based).
	// Changing password doesn't inherently invalidate the JWT signature unless we change the secret or store version.
	// We can invalidate the specific user session cache if we want.
	if h.Cache != nil {
		sessionKey := fmt.Sprintf("session:tenant:%s:%s", claims.TenantID, claims.Email)
		h.Cache.Delete(r.Context(), sessionKey)
	}

	// 4. Send password changed alert
	if h.AlertTriggers != nil {
		h.AlertTriggers.OnPasswordChanged(r.Context(), claims.TenantID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Password updated successfully",
	})
}
