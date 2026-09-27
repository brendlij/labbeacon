package services

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
)

func TestHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	c := New([]config.Check{{Name: "ok", Type: "http", URL: srv.URL, ExpectedStatus: 204, Timeout: time.Second}, {Name: "wrong", Type: "http", URL: srv.URL, ExpectedStatus: 200, Timeout: time.Second}, {Name: "slow", Type: "http", URL: srv.URL + "/slow", ExpectedStatus: 200, Timeout: 20 * time.Millisecond}})
	s, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s[0].Value != "ON" || s[1].Value != "OFF" || s[2].Value != "OFF" {
		t.Fatalf("bad states: %v", s)
	}
	if s[0].Device.Kind != "service" || s[0].Device.Name != "ok" || s[0].Name != "Connectivity" {
		t.Fatal("incorrect service grouping")
	}
	if s[0].Attributes["checked_at"] == nil || s[0].Attributes["response_time_ms"] == nil {
		t.Fatal("missing attributes")
	}
}
func TestTCP(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	host, port, e := net.SplitHostPort(l.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	p, e := strconv.Atoi(port)
	if e != nil {
		t.Fatal(e)
	}
	c := New([]config.Check{{Name: "tcp", Type: "tcp", Host: host, Port: p, Timeout: time.Second}})
	s, e := c.Collect(context.Background())
	if e != nil || s[0].Value != "ON" {
		t.Fatalf("%v %v", s, e)
	}
	if e = l.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = c.Collect(context.Background())
	if e != nil || s[0].Value != "OFF" {
		t.Fatalf("%v %v", s, e)
	}
}
