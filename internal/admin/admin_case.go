package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/storage"
)

// --- Request/Response Types ---
type CreateCaseRequest struct {
	TenantID uint   `json:"tenant_id"`
	Source   string `json:"source"`
	Reason   string `json:"reason"`
}

type AssignCaseRequest struct {
	AssignedTo string `json:"assigned_to"`
}

type ChangeCaseStatusRequest struct {
	Status string `json:"status"`
}

// --- Handlers ---

// POST /admin/cases
func CreateAdminCaseHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateCaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		// Validate tenant exists
		var tenant storage.Tenant
		if err := ctx.DB.First(&tenant, req.TenantID).Error; err != nil {
			http.Error(w, "tenant not found", http.StatusBadRequest)
			return
		}
		// Validate source and reason
		if req.Source == "" || req.Reason == "" {
			http.Error(w, "source and reason required", http.StatusBadRequest)
			return
		}
		adminEmail := AdminEmailFromRequest(r)
		if adminEmail == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Only allowed roles
		role := AdminRoleFromRequest(r)
		if !isAllowedRole(role, []string{RoleSupportAdmin, RolePlatformAdmin, RoleComplianceAdmin, RoleSuperAdmin}) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		caseObj := storage.AdminCase{
			TenantID:  req.TenantID,
			Source:    req.Source,
			Status:    "open",
			Reason:    req.Reason,
			CreatedBy: adminEmail,
			CreatedAt: time.Now(),
		}
		if err := ctx.DB.Create(&caseObj).Error; err != nil {
			http.Error(w, "failed to create case", http.StatusInternalServerError)
			return
		}
		if err := writeCaseAuditLog(ctx, adminEmail, role, caseObj.ID, caseObj.TenantID, "case.create", req); err != nil {
			ctx.DB.Delete(&caseObj)
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}
		respondJSON(w, caseObj)
	}
}

// POST /admin/cases/{id}/assign
func AssignAdminCaseHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
		var req AssignCaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		var caseObj storage.AdminCase
		if err := ctx.DB.First(&caseObj, id).Error; err != nil {
			http.Error(w, "case not found", http.StatusNotFound)
			return
		}
		role := AdminRoleFromRequest(r)
		if !isAllowedRole(role, []string{RolePlatformAdmin, RoleComplianceAdmin, RoleSuperAdmin}) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Assigned admin must exist and be active
		var adminUser storage.AdminUser
		if err := ctx.DB.Where("email = ? AND active = ?", req.AssignedTo, true).First(&adminUser).Error; err != nil {
			http.Error(w, "assigned admin not found or inactive", http.StatusBadRequest)
			return
		}
		caseObj.AssignedTo = &req.AssignedTo
		if err := ctx.DB.Model(&caseObj).Update("assigned_to", req.AssignedTo).Error; err != nil {
			http.Error(w, "failed to assign case", http.StatusInternalServerError)
			return
		}
		adminEmail := AdminEmailFromRequest(r)
		if err := writeCaseAuditLog(ctx, adminEmail, role, caseObj.ID, caseObj.TenantID, "case.assign", req); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}
		respondJSON(w, caseObj)
	}
}

// POST /admin/cases/{id}/status
func ChangeAdminCaseStatusHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
		var req ChangeCaseStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		var caseObj storage.AdminCase
		if err := ctx.DB.First(&caseObj, id).Error; err != nil {
			http.Error(w, "case not found", http.StatusNotFound)
			return
		}
		role := AdminRoleFromRequest(r)
		adminEmail := AdminEmailFromRequest(r)
		if caseObj.AssignedTo == nil || *caseObj.AssignedTo != adminEmail {
			http.Error(w, "only assigned admin can change status", http.StatusForbidden)
			return
		}
		// Enforce lifecycle
		if err := enforceCaseLifecycle(caseObj.Status, req.Status); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		updates := map[string]interface{}{"status": req.Status}
		if req.Status == "closed" {
			updates["closed_at"] = time.Now()
		}
		if err := ctx.DB.Model(&caseObj).Updates(updates).Error; err != nil {
			http.Error(w, "failed to update status", http.StatusInternalServerError)
			return
		}
		if err := writeCaseAuditLog(ctx, adminEmail, role, caseObj.ID, caseObj.TenantID, "case.status_change", req); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}
		respondJSON(w, caseObj)
	}
}

// --- Helper Functions ---

func enforceCaseLifecycle(from, to string) error {
	allowed := map[string][]string{
		"open":          {"investigating", "waiting"},
		"investigating": {"waiting", "resolved"},
		"waiting":       {"investigating"},
		"resolved":      {"closed"},
	}
	if from == "closed" {
		return errors.New("cannot reopen closed case")
	}
	for _, t := range allowed[from] {
		if to == t {
			return nil
		}
	}
	return errors.New("invalid status transition")
}

func writeCaseAuditLog(ctx *AdminContext, adminEmail, adminRole string, caseID, tenantID uint, action string, meta interface{}) error {
	metaJSON, _ := json.Marshal(meta)
	log := storage.AdminAuditLog{
		AdminEmail: adminEmail,
		AdminRole:  adminRole,
		Action:     action,
		TenantID:   &tenantID,
		Resource:   "admin_case",
		ResourceID: strconv.FormatUint(uint64(caseID), 10),
		Success:    true,
		StatusCode: 200,
		Metadata:   string(metaJSON),
	}
	if err := ctx.DB.Create(&log).Error; err != nil {
		return err
	}
	return nil
}
