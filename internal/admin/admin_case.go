package admin

import (
	"context"
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
		// Pre-audit intent (fail-closed): do not create the case unless audit succeeds
		aePre := AdminAuditEntry{
			AdminEmail: adminEmail,
			AdminRole:  role,
			Action:     "case.create.intent",
			TenantID:   &req.TenantID,
			Metadata: map[string]interface{}{
				"tenant_id": req.TenantID,
				"source":    req.Source,
				"reason":    req.Reason,
			},
		}
		if err := auditWriter(r.Context(), ctx, aePre); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}
		if err := ctx.DB.Create(&caseObj).Error; err != nil {
			http.Error(w, "failed to create case", http.StatusInternalServerError)
			return
		}
		// Post-audit with case id
		ae := AdminAuditEntry{
			AdminEmail: adminEmail,
			AdminRole:  role,
			Action:     "case.create",
			TenantID:   &caseObj.TenantID,
			CaseID:     &caseObj.ID,
			Metadata: map[string]interface{}{
				"case_id":   caseObj.ID,
				"tenant_id": caseObj.TenantID,
				"source":    req.Source,
				"reason":    req.Reason,
			},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			// Rollback create if audit fails
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
		adminEmail := AdminEmailFromRequest(r)
		// Audit before mutation (fail-closed)
		ae := AdminAuditEntry{
			AdminEmail: adminEmail,
			AdminRole:  role,
			Action:     "case.assign",
			TenantID:   &caseObj.TenantID,
			CaseID:     &caseObj.ID,
			Metadata: map[string]interface{}{
				"case_id":     caseObj.ID,
				"assigned_to": req.AssignedTo,
			},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}
		if err := ctx.DB.Model(&caseObj).Update("assigned_to", req.AssignedTo).Error; err != nil {
			http.Error(w, "failed to assign case", http.StatusInternalServerError)
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
		// Audit before mutation (fail-closed)
		ae := AdminAuditEntry{
			AdminEmail: adminEmail,
			AdminRole:  role,
			Action:     "case.status_change",
			TenantID:   &caseObj.TenantID,
			CaseID:     &caseObj.ID,
			Metadata: map[string]interface{}{
				"case_id": caseObj.ID,
				"status":  req.Status,
			},
		}
		if err := auditWriter(r.Context(), ctx, ae); err != nil {
			http.Error(w, "audit log failed", http.StatusInternalServerError)
			return
		}
		if err := ctx.DB.Model(&caseObj).Updates(updates).Error; err != nil {
			http.Error(w, "failed to update status", http.StatusInternalServerError)
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
	metaMap := map[string]interface{}{"meta": meta}
	ae := AdminAuditEntry{
		AdminEmail: adminEmail,
		AdminRole:  adminRole,
		Action:     action,
		TenantID:   &tenantID,
		CaseID:     &caseID,
		Metadata:   metaMap,
	}
	return auditWriter(context.Background(), ctx, ae)
}
