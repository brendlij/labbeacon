package docker

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
)

type slowStatsAPI struct {
	mu                  sync.Mutex
	active, peak, calls int
}

func (*slowStatsAPI) Containers(context.Context) ([]Container, error) {
	var out []Container
	for i := range 24 {
		out = append(out, Container{ID: strconv.Itoa(i), Name: strconv.Itoa(i), Status: "running"})
	}
	return append(out, Container{ID: "stopped", Name: "stopped", Status: "exited"}), nil
}

func (a *slowStatsAPI) Stats(ctx context.Context, id string) (Stats, error) {
	a.mu.Lock()
	a.active++
	a.calls++
	a.peak = max(a.peak, a.active)
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.active--; a.mu.Unlock() }()
	i, _ := strconv.Atoi(id)
	if i < statsWorkers {
		<-ctx.Done()
		return Stats{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return Stats{}, err
	}
	return Stats{Memory: 42, Limit: 100}, nil
}

func TestSlowStatsDoNotExhaustLaterContainers(t *testing.T) {
	api := &slowStatsAPI{}
	c := Collector{API: api, Config: config.Docker{Stats: true}, Timeout: 100 * time.Millisecond}
	samples, err := c.Collect(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing slow-container error: %v", err)
	}
	if len(c.Snapshot) != 25 || samples[0].Value != 24 {
		t.Fatal("inventory lost on partial stats failure")
	}
	for i, v := range c.Snapshot[:24] {
		if i < statsWorkers && v.MemoryBytes != nil {
			t.Fatal("timed-out stats published")
		}
		if i >= statsWorkers && (v.MemoryBytes == nil || *v.MemoryBytes != 42) {
			t.Fatalf("container %d inherited expired deadline", i)
		}
	}
	for _, sample := range samples[2:] {
		if sample.Device.Kind != "container" || sample.Device.Name == "" {
			t.Fatalf("ungrouped metric: %+v", sample)
		}
	}
	if samples[0].Device.Kind != "" {
		t.Fatal("summary moved off server")
	}
	if api.calls != 24 || api.peak > statsWorkers || api.peak < 2 || api.active != 0 {
		t.Fatalf("bad worker bounds: %+v", api)
	}
}

func TestStatsParentCancellationAndDisabled(t *testing.T) {
	api := &slowStatsAPI{}
	c := Collector{API: api, Config: config.Docker{Stats: true}, Timeout: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Collect(ctx)
	if !errors.Is(err, context.Canceled) || api.calls != 0 {
		t.Fatalf("parent cancellation ignored: %v", err)
	}
	c.Config.Stats = false
	if _, err := c.Collect(context.Background()); err != nil || api.calls != 0 {
		t.Fatalf("disabled stats queried: %v", err)
	}
}
