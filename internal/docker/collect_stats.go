package docker

import (
	"context"
	"sync"
)

const statsWorkers = 8

type statsResult struct {
	stats Stats
	err   error
}

func (c *Collector) collectStats(ctx context.Context, containers []Container) []statsResult {
	api, ok := c.API.(interface {
		Stats(context.Context, string) (Stats, error)
	})
	if !c.Config.Stats || !ok {
		return nil
	}
	results := make([]statsResult, len(containers))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(statsWorkers, len(containers)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					results[i].err = err
					continue
				}
				// Start the budget when a worker actually begins this request.
				requestCtx, cancel := context.WithTimeout(ctx, c.Timeout)
				results[i].stats, results[i].err = api.Stats(requestCtx, containers[i].ID)
				cancel()
			}
		}()
	}
	for i := range containers {
		if containers[i].Status == "running" {
			jobs <- i
		}
	}
	close(jobs)
	workers.Wait()
	return results
}
