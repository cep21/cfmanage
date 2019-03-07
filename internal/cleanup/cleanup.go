package cleanup

import (
	"context"
	"sync"
	"time"
)

type Job func(ctx context.Context) error

type Cleanup struct {
	CleanupTimeout time.Duration
	OnErr func(error)
	mu sync.Mutex
	cleaners []Job
}

func (c *Cleanup) Add(f Job) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleaners = append(c.cleaners, f)
}

func (c *Cleanup) Clean() {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, onDone := context.WithTimeout(context.Background(), c.CleanupTimeout)
	defer onDone()
	wg := sync.WaitGroup{}
	for _, cleanJob := range c.cleaners {
		wg.Add(1)
		cleanJob := cleanJob
		go func() {
			defer wg.Done()
			if err := cleanJob(ctx); err != nil {
				if c.OnErr != nil {
					c.OnErr(err)
				}
			}
		}()
	}
	wg.Wait()
}