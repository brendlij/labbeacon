package tailscale

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	s, e := Parse([]byte(`{"BackendState":"Running","TailscaleIPs":["100.1.2.3"],"Self":{"Online":true,"ExitNodeOption":true},"Peer":{"a":{"Online":true},"b":{"Online":false}},"ExitNodeStatus":{"Online":true,"ID":"exit"}}`))
	if e != nil || s[0].Value != "ON" || s[1].Value != 1 || len(s) != 4 {
		t.Fatalf("%v %v", s, e)
	}
	for _, data := range []string{`{`, `{}`} {
		if _, e = Parse([]byte(data)); e == nil {
			t.Fatal("expected error")
		}
	}
}

type missing struct{}

func (missing) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("not installed")
}
func TestCommandFailure(t *testing.T) {
	c := Collector{Runner: missing{}, Command: "missing", Timeout: time.Second}
	if _, e := c.Collect(context.Background()); e == nil {
		t.Fatal("expected error")
	}
}
