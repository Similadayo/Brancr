package interfaces

import (
	"context"
)

// Service represents a service that can be initialized and started
type Service interface {
	Initialize(ctx context.Context) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
