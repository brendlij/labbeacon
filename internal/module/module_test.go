package module

import (
	"context"
	"errors"
	"testing"

	"github.com/brendlij/labbeacon/internal/metric"
)

type fake struct {
	calls int
	err   error
}

func (f *fake) Collect(context.Context) ([]metric.Sample, error) {
	f.calls++
	return []metric.Sample{metric.Sensor("partial", "Partial", "", 1)}, f.err
}
func TestRegistry(t *testing.T) {
	active := &fake{err: errors.New("partial")}
	disabled := &fake{}
	r := &Registry{}
	if err := r.Register(Registration{ModuleName: "active", Active: true, Collector: active}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Registration{ModuleName: "disabled", Collector: disabled}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Registration{ModuleName: "active"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	reported := false
	values := r.Collect(context.Background(), func(name string, err error) { reported = name == "active" && err != nil })
	if active.calls != 1 || disabled.calls != 0 || len(values) != 1 || !reported {
		t.Fatal("module isolation failed")
	}
}
