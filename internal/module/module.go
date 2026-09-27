// Package module defines the extension boundary for read-only data sources.
package module

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/brendlij/labbeacon/internal/control"
	"github.com/brendlij/labbeacon/internal/metric"
)

type Module interface {
	Name() string
	Enabled() bool
	Collect(context.Context) ([]metric.Sample, error)
}

// Registration adapts any collector without making it depend on configuration.
type Registration struct {
	ModuleName string
	Active     bool
	metric.Collector
}

func (r Registration) Name() string  { return r.ModuleName }
func (r Registration) Enabled() bool { return r.Active }
func (r Registration) Actions(ctx context.Context) ([]control.Entry, error) {
	if !r.Active {
		return nil, nil
	}
	if p, ok := r.Collector.(control.Provider); ok {
		return p.Actions(ctx)
	}
	return nil, nil
}

type Status struct {
	Name        string
	Enabled     bool
	LastChecked time.Time
	LastError   string
}
type Registry struct {
	modules []Module
	mu      sync.Mutex
	status  map[string]Status
}

func (r *Registry) Status() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Status, 0, len(r.modules))
	for _, m := range r.modules {
		out = append(out, r.status[m.Name()])
	}
	return out
}

type Unavailable struct{ Reason error }

func (u Unavailable) Collect(context.Context) ([]metric.Sample, error) { return nil, u.Reason }

func (r *Registry) Actions(ctx context.Context, report func(string, error)) []control.Entry {
	var out []control.Entry
	for _, m := range r.modules {
		if !m.Enabled() {
			continue
		}
		if p, ok := m.(control.Provider); ok {
			entries, err := p.Actions(ctx)
			if err != nil {
				report(m.Name(), err)
			}
			out = append(out, entries...)
		}
	}
	return out
}
func (r *Registry) Register(m Module) error {
	if m.Name() == "" {
		return fmt.Errorf("empty module name")
	}
	for _, existing := range r.modules {
		if existing.Name() == m.Name() {
			return fmt.Errorf("duplicate module %s", m.Name())
		}
	}
	r.modules = append(r.modules, m)
	if r.status == nil {
		r.status = map[string]Status{}
	}
	r.status[m.Name()] = Status{Name: m.Name(), Enabled: m.Enabled()}
	return nil
}
func (r *Registry) Collect(ctx context.Context, report func(string, error)) []metric.Sample {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var samples []metric.Sample
	for _, m := range r.modules {
		if !m.Enabled() {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			values, err := m.Collect(ctx)
			r.mu.Lock()
			status := r.status[m.Name()]
			status.LastChecked = time.Now()
			status.LastError = ""
			if err != nil {
				status.LastError = err.Error()
			}
			r.status[m.Name()] = status
			r.mu.Unlock()
			if err != nil {
				report(m.Name(), err)
			}
			mu.Lock()
			samples = append(samples, values...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return samples
}
