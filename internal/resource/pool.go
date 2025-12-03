package resource

import (
	"context"
	"sync"
)

// Pool manages a pool of resources with concurrency limits
type Pool struct {
	semaphore chan struct{}
	wg        sync.WaitGroup
}

// NewPool creates a new resource pool with max concurrency
func NewPool(maxConcurrency int) *Pool {
	return &Pool{
		semaphore: make(chan struct{}, maxConcurrency),
	}
}

// Execute executes a function with resource pool limits
func (p *Pool) Execute(ctx context.Context, fn func() error) error {
	// Acquire semaphore
	select {
	case p.semaphore <- struct{}{}:
		defer func() { <-p.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}

	// Execute function
	p.wg.Add(1)
	defer p.wg.Done()

	return fn()
}

// Wait waits for all pending operations to complete
func (p *Pool) Wait() {
	p.wg.Wait()
}

// Size returns current pool size
func (p *Pool) Size() int {
	return len(p.semaphore)
}

// Capacity returns pool capacity
func (p *Pool) Capacity() int {
	return cap(p.semaphore)
}

