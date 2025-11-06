package admin

import (
	"encoding/json"
	"net/http"
)

// small helper to count tenants (best-effort)
func countTenants(ctx *AdminContext) int64 {
	if ctx.DB == nil {
		return 0
	}
	var count int64
	// assuming tenants table name is "tenants"
	ctx.DB.Table("tenants").Count(&count)
	return count
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func zapError(err error) interface{} {
	if err == nil {
		return nil
	}
	return map[string]interface{}{"error": err.Error()}
}
