package admin

import (
	"encoding/json"
	"net/http"
)

// AdminMeHandler returns the current admin's email and role from context
func AdminMeHandler(ctx *AdminContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := AdminEmailFromRequest(r)
		role := AdminRoleFromRequest(r)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"email": email,
			"role":  role,
		})
	}
}
