package docker

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
)

func TestStatsCalculation(t *testing.T) {
	var s statsResponse
	s.CPU.Usage.Total = 200
	s.Previous.Usage.Total = 100
	s.CPU.System = 2000
	s.Previous.System = 1000
	s.CPU.Online = 4
	s.Memory.Usage = 4096
	s.Memory.Limit = 8192
	s.Memory.Stats = map[string]uint64{"inactive_file": 1024}
	v := decodeStats(s)
	if v.CPU == nil || *v.CPU != 40 || v.Memory != 3072 {
		t.Fatalf("%+v", v)
	}
	s.CPU.Usage.Total = 1
	if decodeStats(s).CPU != nil {
		t.Fatal("counter reset must not report huge CPU")
	}
}
func TestDockerControlEndpoint(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/containers/abc/restart" || r.URL.Query().Get("t") != "10" {
			t.Errorf("wrong request %s %s", r.Method, r.URL)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{http: &http.Client{Transport: rewriteTransport{srv.Listener.Addr().String()}}}
	if err := c.Control(context.Background(), "abc", "restart"); err != nil {
		t.Fatal(err)
	}
	if err := c.Control(context.Background(), "abc", "delete"); err == nil || calls.Load() != 1 {
		t.Fatal("invalid action reached API")
	}
}

type fakeControl struct {
	fakeAPI
	targets int
}

func (*fakeControl) ControlReady(context.Context) error                  { return nil }
func (f *fakeControl) CheckTarget(context.Context, string, string) error { f.targets++; return nil }
func (*fakeControl) Control(context.Context, string, string) error       { return nil }
func TestActionFilteringAndRemoval(t *testing.T) {
	api := &fakeControl{}
	c := &Collector{API: api, Config: config.Docker{ControlContainers: config.ContainerPolicy{Enabled: true, Deny: []string{"db"}}}, Snapshot: []Container{{ID: "1", Name: "web"}, {ID: "2", Name: "db"}}}
	a, e := c.Actions(context.Background())
	if e != nil || len(a) != 3 {
		t.Fatalf("%v %v", a, e)
	}
	if e = a[0].Check(context.Background()); e != nil || api.targets != 1 {
		t.Fatal("missing target preflight")
	}
	c.Snapshot = nil
	a, e = c.Actions(context.Background())
	if e != nil || len(a) != 0 {
		t.Fatal("stale actions")
	}
}

type digestSource struct{ digests []string }

func (d digestSource) ImageDigests(context.Context, string) ([]string, error) { return d.digests, nil }
func TestAnonymousUpdates(t *testing.T) {
	manifest := `{"schemaVersion":2,"config":{"digest":"sha256:test"}}`
	hash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifest)))
	var calls, status atomic.Int32
	status.Store(200)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(manifest))
	}))
	defer srv.Close()
	ref := srv.Listener.Addr().String() + "/project/app:latest"
	c := NewUpdateChecker(digestSource{[]string{"project/app@" + hash}}, config.ImageUpdates{Interval: time.Hour, Timeout: time.Second})
	c.Client = srv.Client()
	v, e := c.Check(context.Background(), ref, "id")
	if e != nil || v == nil || *v {
		t.Fatalf("matching digest: %v %v", v, e)
	}
	if _, e = c.Check(context.Background(), ref, "id"); e != nil || calls.Load() != 1 {
		t.Fatal("cache")
	}
	c.Local = digestSource{[]string{"project/app@sha256:old"}}
	v, e = c.Check(context.Background(), ref, "changed-id")
	if e != nil || v == nil || !*v {
		t.Fatal("changed digest")
	}
	status.Store(401)
	v, e = c.Check(context.Background(), ref, "auth-required")
	if e != nil || v != nil {
		t.Fatal("auth registry should be skipped")
	}
}
