package shadow

import "context"

type Runner struct {
	pipeline Pipeline
	recorder Recorder
}

func NewRunner(p Pipeline, r Recorder) *Runner {
	return &Runner{
		pipeline: p,
		recorder: r,
	}
}

func (r *Runner) Run(ctx context.Context, message string, request interface{}) {
	defer func() {
		_ = recover()
	}()

	result := r.pipeline.Process(ctx, request)

	r.recorder.Record(ctx, message, result)
}
