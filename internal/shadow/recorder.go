package shadow

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	v3 "github.com/similadayo/brancr/internal/ai/v3"
	"go.uber.org/zap"
)

type Recorder struct {
	Logger *zap.Logger
}

func NewRecorder(logger *zap.Logger) *Recorder {
	return &Recorder{Logger: logger}
}

// Record logs the shadow result for offline comparison
func (r *Recorder) Record(ctx context.Context, message string, result ShadowResult) {
	record := ShadowRecord{
		UserMessage:  message,
		ShadowReply:  result.Reply,
		ShadowAction: result.SuggestedAction,
		ShadowGoal:   result.ConversationGoal,
		ShadowMode:   result.ResponseMode,
		LatencyMs:    result.DurationMs,
		Timestamp:    result.Timestamp,
	}

	data, _ := json.Marshal(record)

	// Structured logging for easy ingestion by ELK/Splunk
	r.Logger.Info("shadow_execution",
		zap.String("record_json", string(data)),
	)
}

// AttachActive saves the active pipeline result for comparison
func (r *Recorder) AttachActive(ctx context.Context, message string, activeOut *v3.Response) {
	go func() {
		record := ShadowRecord{
			UserMessage:  message,
			ActiveReply:  "", // Default
			ActiveAction: string(activeOut.Metadata.ActionType),
			Timestamp:    time.Now(),
		}

		if len(activeOut.Messages) > 0 {
			record.ActiveReply = strings.Join(activeOut.Messages, " ")
		}

		data, _ := json.Marshal(record)
		r.Logger.Info("active_execution_capture",
			zap.String("record_json", string(data)),
		)
	}()
}
