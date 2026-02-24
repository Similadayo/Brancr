package v3

import (
	"go.uber.org/zap"
)

// PipelineLogger provides semantic logging for the AI pipeline
// It wraps zap.Logger to enforce a structured "X-Ray" format
type PipelineLogger struct {
	logger *zap.Logger
}

// NewPipelineLogger creates a new diagnostic logger
func NewPipelineLogger(logger *zap.Logger) *PipelineLogger {
	return &PipelineLogger{logger: logger}
}

// LogThink captures the "Thinking" phase (State Evaluation)
// Shows WHY the AI perceives the conversation this way
func (l *PipelineLogger) LogThink(ctx *ConversationContext) {
	if l.logger == nil {
		return
	}
	l.logger.Info("[THINK] State Evaluation",
		zap.String("phase", string(ctx.Phase)),
		zap.Float64("gradient", ctx.CommitmentGradient),
		zap.String("tone", ctx.EmotionalTone),
		zap.Float64("momentum", ctx.UserSignals.Momentum),
		zap.String("role", ctx.ActiveRole),
	)
}

// LogDecide captures the "Decision" phase (Action Resolution)
// Shows rule triggers: Input Intent -> Output Action
func (l *PipelineLogger) LogDecide(intent Intent, action Action) {
	if l.logger == nil {
		return
	}
	l.logger.Info("[DECIDE] Rule Triggered",
		zap.String("input_intent", string(intent.Type)),
		zap.Float64("intent_conf", intent.Confidence),
		zap.String("output_action", string(action.Type)),
		zap.String("reason", action.Reason),
		zap.Bool("allowed", action.Allowed),
	)
}

// LogSay captures the "Saying" phase (Response Composition)
// Shows the final output generated for the user
func (l *PipelineLogger) LogSay(response *Response) {
	if l.logger == nil {
		return
	}
	l.logger.Info("[SAY] Final Response",
		zap.Strings("messages", response.Messages),
		zap.Int64("processing_ms", response.Metadata.ProcessingMs),
	)
}

// LogContext captures context build stats
func (l *PipelineLogger) LogContext(ctx *ConversationContext) {
	if l.logger == nil {
		return
	}
	l.logger.Info("[CONTEXT] Built",
		zap.Int("products", len(ctx.Products)),
		zap.Int("cart_items", len(ctx.PendingCart)),
		zap.Bool("has_payment_info", ctx.PaymentInfo != nil),
	)
}
