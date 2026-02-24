package admin

import (
	"net/http"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/similadayo/brancr/internal/logger"
	"go.uber.org/zap"
)

// LogStreamHandler upgrades the connection to WebSocket and streams logs
func LogStreamHandler(broadcaster *logger.LogBroadcaster, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Debug logging
		log.Info("ws: log stream request received",
			zap.String("path", r.URL.Path),
			zap.String("remote", r.RemoteAddr),
			zap.String("upgrade", r.Header.Get("Upgrade")),
			zap.String("connection", r.Header.Get("Connection")))

		// Check if broadcaster is available
		if broadcaster == nil {
			log.Error("ws: LogBroadcaster is nil - log streaming not available")
			http.Error(w, "Log streaming not available", http.StatusServiceUnavailable)
			return
		}

		// Check if this is actually a WebSocket upgrade request
		if r.Header.Get("Upgrade") != "websocket" {
			// Not a WebSocket request - return info
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","message":"WebSocket endpoint ready. Connect with WebSocket client."}`))
			return
		}

		// Set CORS headers for WebSocket upgrade (some proxies need this)
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Upgrade connection
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			log.Warn("ws: upgrade failed", zap.Error(err), zap.String("origin", r.Header.Get("Origin")))
			return
		}
		defer conn.Close()

		log.Info("ws: admin log stream connected", zap.String("remote", r.RemoteAddr))

		// Subscribe to broadcaster
		// The broadcaster returns a channel for new logs and a slice of historical logs
		logChan, history := broadcaster.Subscribe()
		defer broadcaster.Unsubscribe(logChan)

		// Send history first
		for _, logEntry := range history {
			if err := wsutil.WriteServerText(conn, logEntry); err != nil {
				log.Debug("ws: write error (history)", zap.Error(err))
				return
			}
		}

		// Keep connection alive and listen for close
		// We need a separate goroutine to read from client (to detect close)
		// or we can just rely on write errors to detect disconnection.
		// However, wsutil.WriteServerText might block if we don't read control frames.
		// For simplicity in this admin tool, we'll optimistically write.
		// A more robust implementation would have a reader pump.

		closeChan := make(chan struct{})
		go func() {
			defer close(closeChan)
			// Discard incoming messages, just read to detect close
			for {
				_, _, err := wsutil.ReadClientData(conn)
				if err != nil {
					return
				}
			}
		}()

		// Stream new logs
		for {
			select {
			case <-closeChan:
				return
			case logEntry := <-logChan:
				if err := wsutil.WriteServerText(conn, logEntry); err != nil {
					log.Debug("ws: write error (stream)", zap.Error(err))
					return
				}
			case <-r.Context().Done():
				return
			}
		}
	}
}

// LogEntry is a helper struct for unmarshalling if needed, though we just stream bytes
type LogEntry struct {
	Level     string    `json:"level"`
	Time      time.Time `json:"ts"`
	Caller    string    `json:"caller"`
	Message   string    `json:"msg"`
	Stack     string    `json:"stack,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
}

// LogStreamSSEHandler streams logs using Server-Sent Events (SSE)
// This is a fallback for environments where WebSocket upgrades don't work (e.g., some proxies)
func LogStreamSSEHandler(broadcaster *logger.LogBroadcaster, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Info("sse: log stream request received",
			zap.String("path", r.URL.Path),
			zap.String("remote", r.RemoteAddr))

		// Check if broadcaster is available FIRST
		if broadcaster == nil {
			log.Error("sse: LogBroadcaster is nil - log streaming not available")
			http.Error(w, "Log streaming not available", http.StatusServiceUnavailable)
			return
		}

		// Check flusher support BEFORE setting headers
		flusher, ok := w.(http.Flusher)
		if !ok {
			log.Error("sse: streaming not supported by ResponseWriter")
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// NOW set SSE headers (after we know we can stream)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

		// Set CORS if origin present
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		// Write status code explicitly
		w.WriteHeader(http.StatusOK)

		log.Info("sse: admin log stream connected", zap.String("remote", r.RemoteAddr))

		// Subscribe to broadcaster
		logChan, history := broadcaster.Subscribe()
		defer broadcaster.Unsubscribe(logChan)

		// Send history first (skip nil entries from uninitialized buffer)
		for _, logEntry := range history {
			if logEntry == nil {
				continue // Skip uninitialized buffer slots
			}
			if _, err := w.Write([]byte("data: " + string(logEntry) + "\n\n")); err != nil {
				log.Debug("sse: write error (history)", zap.Error(err))
				return
			}
		}
		flusher.Flush()

		// Send initial heartbeat/ping to confirm connection
		if _, err := w.Write([]byte(": ping\n\n")); err != nil {
			log.Debug("sse: write error (initial ping)", zap.Error(err))
			return
		}
		flusher.Flush()

		// Create heartbeat ticker to keep connection alive (every 15 seconds)
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		defer func() {
			log.Info("sse: stream closed - handler returned", zap.Error(r.Context().Err()))
		}()

		// Stream new logs with periodic heartbeat
		for {
			select {
			case logEntry := <-logChan:
				if logEntry == nil {
					continue
				}
				if _, err := w.Write([]byte("data: " + string(logEntry) + "\n\n")); err != nil {
					log.Debug("sse: write error (stream)", zap.Error(err))
					return
				}
				flusher.Flush()
			case <-heartbeat.C:
				// Send SSE comment as heartbeat (doesn't trigger onmessage in browser)
				if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
					log.Debug("sse: heartbeat write error", zap.Error(err))
					return
				}
				flusher.Flush()
			case <-r.Context().Done():
				log.Debug("sse: client disconnected")
				return
			}
		}
	}
}
