package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

var adminRoleOptions = []string{
	RoleSuperAdmin,
	RoleSupportAdmin,
	RolePlatformAdmin,
	RoleBillingAdmin,
	RoleComplianceAdmin,
	RoleOperationsSupport,
}

func AdminUsersPageHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var users []storage.AdminUser
		if err := ctx.DB.Order("created_at DESC").Find(&users).Error; err != nil {
			ctx.Logger.Error("failed to fetch admin users", zap.Error(err))
			http.Error(w, "failed to fetch admin users", http.StatusInternalServerError)
			return
		}

		data := map[string]any{
			"Users": users,
			"Roles": adminRoleOptions,
			"Page":  "admin_users",
		}
		renderTemplate(w, r, "admin_users.html", data, ctx.Logger)
	}
}

func AdminUsersCreateHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
		role := strings.TrimSpace(r.FormValue("role"))
		password := r.FormValue("password")

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

		if err := ctx.DB.Create(&user).Error; err != nil {
			ctx.Logger.Error("failed to create admin user", zap.Error(err))
			http.Error(w, "failed to create user", http.StatusInternalServerError)
			return
		}

		persistAdminAuditEvent(ctx, r, storage.AdminAuditLog{
			Action:     "admin.user.create",
			Resource:   "admin_user",
			ResourceID: user.ID.String(),
			Metadata:   mustJSON(map[string]any{"email": email, "role": role}),
		})

		http.Redirect(w, r, "/admin/security/admin-users", http.StatusSeeOther)
	}
}

func AdminUsersUpdateHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		idStr := strings.TrimSpace(r.FormValue("id"))
		id, err := uuid.Parse(idStr)
		if err != nil || id == uuid.Nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		var user storage.AdminUser
		if err := ctx.DB.First(&user, id).Error; err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		updates := map[string]any{}

		if role := strings.TrimSpace(r.FormValue("role")); role != "" {
			if !isAllowedRole(role, adminRoleOptions) {
				http.Error(w, "invalid role", http.StatusBadRequest)
				return
			}
			updates["role"] = role
		}

		active := r.FormValue("active") == "on"
		updates["active"] = active

		if newPassword := r.FormValue("password"); strings.TrimSpace(newPassword) != "" {
			if len(newPassword) < 8 {
				http.Error(w, "password too short", http.StatusBadRequest)
				return
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
			if err != nil {
				ctx.Logger.Error("failed to hash admin password", zap.Error(err))
				http.Error(w, "failed to update user", http.StatusInternalServerError)
				return
			}
			updates["password_hash"] = string(hash)
		}

		if err := ctx.DB.Model(&user).Updates(updates).Error; err != nil {
			ctx.Logger.Error("failed to update admin user", zap.Error(err))
			http.Error(w, "failed to update user", http.StatusInternalServerError)
			return
		}

		meta := map[string]any{"id": user.ID}
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

		http.Redirect(w, r, "/admin/security/admin-users", http.StatusSeeOther)
	}
}

func isAllowedRole(role string, allowed []string) bool {
	for _, r := range allowed {
		if role == r {
			return true
		}
	}
	return false
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
