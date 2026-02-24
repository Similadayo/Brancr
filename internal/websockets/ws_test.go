package websockets

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestWSHandler_MissingTenantID(t *testing.T) {
	handler := NewWSHandler(&gorm.DB{}, zap.NewNop(), "secret")
	s := httptest.NewServer(handler)
	defer s.Close()

	resp, err := http.Get(s.URL + "/ws")
	if err != nil {
		t.Fatalf("http get failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestWSHandler_UpgradeSuccess(t *testing.T) {
	handler := NewWSHandler(&gorm.DB{}, zap.NewNop(), "secret")
	s := httptest.NewServer(handler)
	defer s.Close()

	u := "ws" + s.URL[len("http"):]
	// connect with tenant_id
	u = u + "/ws?tenant_id=tenant_123"

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	conn, resp, err := dialer.Dial(u, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed, status: %d, err: %v", resp.StatusCode, err)
		}
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Send a ping text message and expect an echo back
	msg := "hello"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, b, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if string(b) != msg {
		t.Fatalf("expected echo %q, got %q", msg, string(b))
	}
}
