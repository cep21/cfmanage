package ctxfinder

import (
	"context"
	"sync"
	"time"
)

type ContextFinder struct {
	Timeout time.Duration
	mu      sync.Mutex
	ctx     context.Context
}

func (c *ContextFinder) Ctx() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx != nil {
		return c.ctx
	}
	c.ctx = context.Background()
	if c.Timeout != 0 {
		c.ctx, _ = context.WithDeadline(c.ctx, time.Now().Add(c.Timeout)) //nolint
	}
	return c.ctx
}
