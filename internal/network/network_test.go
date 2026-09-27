package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"homelab-agent/internal/config"
)

func TestPublicIPCacheAndInvalidResponse(t *testing.T) {
	var calls atomic.Int32
	var body atomic.Value
	body.Store("203.0.113.7")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	defer srv.Close()
	c := New(config.Network{PublicIP: config.PublicIP{Enabled: true, Endpoint: srv.URL, Interval: time.Hour, Timeout: time.Second}})
	c.Client = srv.Client()
	for range 2 {
		s, e := c.Collect(context.Background())
		if e != nil || len(s) != 1 || s[0].Value != "203.0.113.7" {
			t.Fatalf("%v %v", s, e)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("missing cache")
	}
	body.Store("not an IP")
	c.next = time.Time{}
	s, e := c.Collect(context.Background())
	if e == nil || len(s) != 0 {
		t.Fatal("bad endpoint must not publish stale IP")
	}
}
