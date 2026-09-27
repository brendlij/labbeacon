package tailscale

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type socketInfo struct{ mode os.FileMode }

func (s socketInfo) Name() string       { return "tailscaled.sock" }
func (s socketInfo) Size() int64        { return 0 }
func (s socketInfo) Mode() os.FileMode  { return s.mode }
func (s socketInfo) ModTime() time.Time { return time.Time{} }
func (s socketInfo) IsDir() bool        { return false }
func (s socketInfo) Sys() any           { return nil }

func TestDetectHostSocket(t *testing.T) {
	hostPath := filepath.Join("/hostfs/run", "tailscale", "tailscaled.sock")
	stat := func(path string) (os.FileInfo, error) {
		if path == hostPath {
			return socketInfo{os.ModeSocket}, nil
		}
		return socketInfo{0}, nil // Regular files are not daemon sockets.
	}
	if got := detectSocket("/hostfs/run", stat); got != hostPath {
		t.Fatalf("host socket: %s", got)
	}
	if got := detectSocket("", stat); got != "" {
		t.Fatalf("unexpected socket: %s", got)
	}
	stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	if got := detectSocket("/hostfs/run", stat); got != "" {
		t.Fatal("missing socket selected")
	}
}

type recordingRunner struct{ args []string }

func (r *recordingRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.args = args
	return []byte(`{"BackendState":"Running","Self":{"Online":true}}`), nil
}

func TestExplicitSocketArgument(t *testing.T) {
	r := &recordingRunner{}
	c := Collector{Runner: r, Command: "tailscale", SocketPath: "/shared/tailscaled.sock", Timeout: time.Second}
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.args, []string{"--socket=/shared/tailscaled.sock", "status", "--json"}) {
		t.Fatalf("wrong arguments: %v", r.args)
	}
}
