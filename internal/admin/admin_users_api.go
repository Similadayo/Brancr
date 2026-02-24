package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// AdminUsersAPIHandler returns all admin users as JSON
// GET /admin/api/admin-users
func AdminUsersAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var users []storage.AdminUser
		if err := ctx.DB.WithContext(r.Context()).Order("created_at DESC").Find(&users).Error; err != nil {
			ctx.Logger.Error("failed to fetch admin users", zap.Error(err))
			http.Error(w, "failed to fetch admin users", http.StatusInternalServerError)
			return
		}

		// Convert to response format
		response := make([]map[string]interface{}, len(users))
		for i, user := range users {
			response[i] = map[string]interface{}{
				"id":         user.ID,
				"email":      user.Email,
				"role":       user.Role,
				"active":     user.Active,
				"last_login": user.LastLogin,
				"created_at": user.CreatedAt,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// AdminUsersCreateAPIHandler creates a new admin user
// POST /admin/api/admin-users
func AdminUsersCreateAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Role     string `json:"role"`
			Password string `json:"password"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		email := strings.TrimSpace(strings.ToLower(req.Email))
		role := strings.TrimSpace(req.Role)
		password := req.Password

		if email == "" || !strings.Contains(email, "@") {
			http.Error(w, "invalid email", http.StatusBadRequest)
			return
		}
		if !isAllowedRole(role, adminRoleOptions) {
			http.Error(w, "invalid role", http.StatusBadRequest)
			return
		}
		if len(password) < 8 {
			http.Error(w, "password too short", http.StatusBadRequest)
			return
		}

		// Check if user already exists
		var existing storage.AdminUser
		if err := ctx.DB.WithContext(r.Context()).Where("email = ?", email).First(&existing).Error; err == nil {
			http.Error(w, "user already exists", http.StatusConflict)
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			ctx.Logger.Error("failed to hash admin password", zap.Error(err))
			http.Error(w, "failed to create user", http.StatusInternalServerError)
			return
		}

		user := storage.AdminUser{
			Email:        email,
			PasswordHash: string(hash),
			Role:         role,
			Active:       true,
		}

		if err := ctx.DB.WithContext(r.Context()).Create(&user).Error; err != nil {
			ctx.Logger.Error("failed to create admin user", zap.Error(err))
			http.Error(w, "failed to create user", http.StatusInternalServerError)
			return
		}

		// Audit log
		persistAdminAuditEvent(ctx, r, storage.AdminAuditLog{
			Action:     "admin.user.create",
			Resource:   "admin_user",
			ResourceID: user.ID.String(),
			Metadata:   mustJSON(map[string]any{"email": email, "role": role}),
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     user.ID,
			"email":  user.Email,
			"role":   user.Role,
			"active": user.Active,
		})
	}
}

// AdminUsersUpdateAPIHandler updates an admin user
// PUT /admin/api/admin-users/{id}
func AdminUsersUpdateAPIHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil || id == uuid.Nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		var req struct {
			Role     *string `json:"role,omitempty"`
			Active   *bool   `json:"active,omitempty"`
			Password *string `json:"password,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		var user storage.AdminUser
		if err := ctx.DB.WithContext(r.Context()).First(&user, id).Error; err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		updates := map[string]interface{}{}

		if req.Role != nil {
			role := strings.TrimSpace(*req.Role)
			if !isAllowedRole(role, adminRoleOptions) {
				http.Error(w, "invalid role", http.StatusBadRequest)
				return
			}
			updates["role"] = role
		}

		if req.Active != nil {
			updates["active"] = *req.Active
		}

		if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
			password := strings.TrimSpace(*req.Password)
			if len(password) < 8 {
				http.Error(w, "password too short", http.StatusBadRequest)
				return
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				ctx.Logger.Error("failed to hash admin password", zap.Error(err))
				http.Error(w, "failed to update user", http.StatusInternalServerError)
				return
			}
			updates["password_hash"] = string(hash)
		}

		if len(updates) == 0 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     user.ID,
				"email":  user.Email,
				"role":   user.Role,
				"active": user.Active,
			})
			return
		}

		if err := ctx.DB.WithContext(r.Context()).Model(&user).Updates(updates).Error; err != nil {
			ctx.Logger.Error("failed to update admin user", zap.Error(err))
			http.Error(w, "failed to update user", http.StatusInternalServerError)
			return
		}

		// Refresh user from DB
		ctx.DB.WithContext(r.Context()).First(&user, id)

		// Audit log
		meta := map[string]interface{}{"id": user.ID}
		if v, ok := updates["role"]; ok {
			meta["role"] = v
		}
		if v, ok := updates["active"]; ok {
			meta["active"] = v
		}
		if _, ok := updates["password_hash"]; ok {
			meta["password_reset"] = true
			meta["password_reset_at"] = time.Now().UTC().Format(time.RFC3339)
		}

		persistAdminAuditEvent(ctx, r, storage.AdminAuditLog{
			Action:     "admin.user.update",
			Resource:   "admin_user",
			ResourceID: user.ID.String(),
			Metadata:   mustJSON(meta),
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         user.ID,
			"email":      user.Email,
			"role":       user.Role,
			"active":     user.Active,
			"last_login": user.LastLogin,
		})
	}
}
