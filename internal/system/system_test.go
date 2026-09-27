package system

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/brendlij/labbeacon/internal/metric"
)

func TestCollectPartialDiskFailure(t *testing.T) {
	path := t.TempDir()
	collector := New([]string{path, filepath.Join(path, "does-not-exist")})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	samples, err := collector.Collect(ctx)
	if err == nil {
		t.Fatal("expected missing disk path error")
	}
	found := map[string]bool{}
	for _, s := range samples {
		found[s.Key] = true
	}
	for _, key := range []string{"cpu_cores", "ram_total", "hostname", "uptime", "disk_" + metric.Key(path) + "_total"} {
		if !found[key] {
			t.Errorf("partial failure lost %s; collection error: %v", key, err)
		}
	}
}

func TestRate(t *testing.T) {
	for _, v := range []struct {
		a, b    uint64
		s, want float64
	}{{120, 100, 2, 10}, {1, 100, 2, 0}, {120, 100, 0, 0}} {
		if got := Rate(v.a, v.b, v.s); got != v.want {
			t.Fatalf("got %v, want %v", got, v.want)
		}
	}
}
