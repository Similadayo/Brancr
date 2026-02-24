package websockets

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SetupWebSocketRouter returns a chi Router with no middleware attached so that
// websocket upgrades have direct access to the underlying http.ResponseWriter
// (and http.Hijacker) without being wrapped by standard HTTP middlewares.
func SetupWebSocketRouter(db *gorm.DB, logger *zap.Logger, tenantJWTSecret string) http.Handler {
	wsRouter := chi.NewRouter()
	// route mounted at "" so when mounted at "/ws" it becomes "/ws/"
	wsRouter.Get("/*", NewWSHandler(db, logger, tenantJWTSecret))
	return wsRouter
}

// NewWSHandler returns an HTTP handler that upgrades connections to WebSocket.
// It validates the presence of tenant_id query param and (optionally) can be
// extended to validate a tenant session token.
func NewWSHandler(db *gorm.DB, logger *zap.Logger, tenantJWTSecret string) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {
		// start := time.Now()
		logger.Debug("ws: incoming handshake", zap.String("path", r.URL.Path), zap.String("remote", r.RemoteAddr))

		// Quick validation before attempting Upgrade - returns proper HTTP error
		tenantID := r.URL.Query().Get("tenant_id")
		if tenantID == "" {
			logger.Warn("ws: missing tenant_id", zap.String("remote", r.RemoteAddr))
			http.Error(w, "missing tenant_id", http.StatusBadRequest)
			return
		}

		// Note: if you want to authenticate by session cookie or Bearer token,
		// prefer returning an HTTP 401/400 before Upgrade so the client gets a
		// meaningful HTTP response rather than a 1006 WebSocket closure.

		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			logger.Warn("ws: upgrade failed", zap.Error(err))
			return
		}
		defer conn.Close()

		// logger.Info("ws: connection established", zap.String("tenant_id", tenantID), zap.Duration("handshake_ms", time.Since(start)))

		// Simple echo / heartbeat loop to keep the connection alive and confirm
		// that the handshake completes correctly in integration tests.
		for {
			msg, err := wsutil.ReadClientText(conn)
			if err != nil {
				logger.Debug("ws: read error, closing", zap.Error(err))
				return
			}
			// Echo back
			if err := wsutil.WriteServerText(conn, msg); err != nil {
				logger.Debug("ws: write error, closing", zap.Error(err))
				return
			}
		}
	}
}
