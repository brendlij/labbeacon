package docker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeAPI struct{ err error }

func (f fakeAPI) Containers(context.Context) ([]Container, error) {
	return []Container{{Status: "running"}, {Status: "paused"}, {Status: "exited"}}, f.err
}
func TestSummary(t *testing.T) {
	c := Collector{API: fakeAPI{}, Timeout: time.Second}
	s, e := c.Collect(context.Background())
	if e != nil || s[0].Value != 1 || s[0].Attributes["total"] != 3 {
		t.Fatalf("bad summary: %v %v", s, e)
	}
	c.API = fakeAPI{errors.New("unavailable")}
	if _, e = c.Collect(context.Background()); e == nil {
		t.Fatal("expected error")
	}
}

type rewriteTransport struct{ base string }

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.URL.Scheme = "http"
	copy.URL.Host = r.base
	return http.DefaultTransport.RoundTrip(copy)
}
func TestEngineAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/containers/json":
			if r.URL.Query().Get("all") != "1" {
				t.Error("missing all=1")
			}
			_, _ = w.Write([]byte(`[{"Id":"abc"}]`))
		case "/containers/abc/json":
			_, _ = w.Write([]byte(`{"Id":"abc","Name":"/web","Config":{"Image":"nginx"},"State":{"Status":"running","StartedAt":"2026-01-01T00:00:00Z","Health":{"Status":"healthy"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{http: &http.Client{Transport: rewriteTransport{srv.Listener.Addr().String()}}}
	v, err := c.Containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 1 || v[0].Name != "web" || v[0].Health != "healthy" || v[0].Uptime <= 0 {
		t.Fatalf("unexpected containers: %+v", v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.Containers(ctx); err == nil {
		t.Fatal("expected cancellation")
	}
}
