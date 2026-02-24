package logger

import (
	"sync"
	"time"
)

// LogBroadcaster captures logs and broadcasts them to subscribers
// It implements io.Writer so it can be used with zapcore.AddSync
type LogBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan []byte]struct{}
	buffer      [][]byte
	bufferSize  int
	cursor      int // Points to the next write position (circular buffer)
	filled      bool
}

// NewLogBroadcaster creates a new log broadcaster with a circular buffer
func NewLogBroadcaster(bufferSize int) *LogBroadcaster {
	if bufferSize <= 0 {
		bufferSize = 1000 // Default buffer size
	}
	return &LogBroadcaster{
		subscribers: make(map[chan []byte]struct{}),
		buffer:      make([][]byte, bufferSize),
		bufferSize:  bufferSize,
	}
}

// Write implements io.Writer
func (b *LogBroadcaster) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Make a copy of the slice because p might be reused by the caller
	data := make([]byte, len(p))
	copy(data, p)

	// Write to circular buffer
	b.buffer[b.cursor] = data
	b.cursor = (b.cursor + 1) % b.bufferSize
	if b.cursor == 0 {
		b.filled = true
	}

	// Broadcast to active subscribers
	for ch := range b.subscribers {
		select {
		case ch <- data:
		default:
			// If subscriber is too slow, we drop the message for them
			// to avoid blocking the logger
		}
	}

	return len(p), nil
}

// Subscribe adds a new subscriber and returns a channel specific to them.
// It also returns historical logs from the buffer to catch them up.
func (b *LogBroadcaster) Subscribe() (chan []byte, [][]byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan []byte, 100) // Small buffer for client
	b.subscribers[ch] = struct{}{}

	// Collect history
	var history [][]byte
	if b.filled {
		// Buffer is full/wrapped: read from cursor to end, then from 0 to cursor
		history = make([][]byte, 0, b.bufferSize)
		history = append(history, b.buffer[b.cursor:]...)
		history = append(history, b.buffer[:b.cursor]...)
	} else {
		// Buffer partially filled: read from 0 to cursor
		history = make([][]byte, 0, b.cursor)
		history = append(history, b.buffer[:b.cursor]...)
	}

	return ch, history
}

// Unsubscribe removes a subscriber
func (b *LogBroadcaster) Unsubscribe(ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[ch]; ok {
		delete(b.subscribers, ch)
		close(ch)
	}
}

// LastActivity returns current time - dummy method for interface satisfaction if needed
func (b *LogBroadcaster) LastActivity() time.Time {
	return time.Now()
}
