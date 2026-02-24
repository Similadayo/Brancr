package tenantapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

func TestCreateAndConfirmBooking(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	tenant := &storage.Tenant{}
	db.Create(tenant)

	h := NewHandler(db, testutil.TestLogger(t), testutil.TestConfig(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Create booking
	start := time.Now().Add(48 * time.Hour).Format(time.RFC3339)
	breq := map[string]any{"service_id": 1, "start_at": start}
	b, _ := json.Marshal(breq)
	req := httptest.NewRequest("POST", "/tenant/bookings", bytes.NewReader(b))
	claims := &middleware.TenantClaims{TenantID: tenant.ID}
	req = req.WithContext(middleware.WithTenantContext(context.Background(), claims))
	w := httptest.NewRecorder()

	h.createBooking(w, req)
	if w.Result().StatusCode != 201 {
		t.Fatalf("expected 201 created, got %d", w.Result().StatusCode)
	}

	// parse response
	var resp map[string]any
	json.NewDecoder(w.Result().Body).Decode(&resp)
	bid := int(resp["booking_id"].(float64))

	// Confirm booking
	confirmPath := "/tenant/bookings/" + fmt.Sprintf("%d", bid) + "/confirm"
	req2 := httptest.NewRequest("POST", confirmPath, nil)
	req2 = req2.WithContext(middleware.WithTenantContext(context.Background(), claims))
	w2 := httptest.NewRecorder()
	h.confirmBooking(w2, req2)
	if w2.Result().StatusCode != 200 {
		t.Fatalf("expected 200 OK on confirm, got %d", w2.Result().StatusCode)
	}

	// Check DB
	var bobj storage.Booking
	db.First(&bobj, bid)
	if bobj.Status != "confirmed" {
		t.Fatalf("expected booking status confirmed, got %s", bobj.Status)
	}
}
