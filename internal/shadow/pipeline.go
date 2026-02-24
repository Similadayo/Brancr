package shadow

import "context"

type Pipeline interface {
	Process(ctx context.Context, request interface{}) ShadowResult
}
