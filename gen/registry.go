package gen

import (
	"fmt"
	"sort"
	"strings"
)

// Registry is the set of targets and backends a binary knows.
//
// It is populated at composition time, in main, and never at run time. There is
// no config-driven loading of third-party targets or backends: Go's runtime
// plugin support is Linux-only and requires an identical toolchain and
// identical build flags, which is a worse contract than a twenty-five line
// program that imports what it wants and calls Serve.
type Registry struct {
	targets  map[string]Target
	backends map[string]Backend

	// errs collects what registration got wrong, so the builder stays
	// chainable. Run reports them before doing anything else.
	errs []error
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{targets: map[string]Target{}, backends: map[string]Backend{}}
}

// Target registers a target.
//
// Registering two under one name is recorded rather than panicked on: it can
// only come from a main written wrong, but a generator that crashes tells the
// user less than one that says which two things collided. Err reports it.
func (r *Registry) Target(t Target) *Registry {
	if prev, dup := r.targets[t.Name()]; dup {
		r.errs = append(r.errs, fmt.Errorf("two targets named %q: %T and %T", t.Name(), prev, t))
		return r
	}
	r.targets[t.Name()] = t
	return r
}

// Backend registers a backend.
func (r *Registry) Backend(b Backend) *Registry {
	if prev, dup := r.backends[b.Name()]; dup {
		r.errs = append(r.errs, fmt.Errorf("two backends named %q: %T and %T", b.Name(), prev, b))
		return r
	}
	r.backends[b.Name()] = b
	return r
}

// Err reports what registration got wrong. Run calls it first.
func (r *Registry) Err() error {
	switch len(r.errs) {
	case 0:
		return nil
	case 1:
		return r.errs[0]
	default:
		msgs := make([]string, len(r.errs))
		for i, err := range r.errs {
			msgs[i] = err.Error()
		}
		return fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}
}

// TargetNames is every registered target, sorted.
func (r *Registry) TargetNames() []string { return sortedKeys(r.targets) }

// BackendNames is every registered backend, sorted.
func (r *Registry) BackendNames() []string { return sortedKeys(r.backends) }

// Sections is every name a target or backend may claim in the config, which is
// what CheckUnclaimed needs to say what this build understands.
func (r *Registry) Sections() []string {
	return append(r.TargetNames(), r.BackendNames()...)
}

// LookupTarget resolves a configured target name.
func (r *Registry) LookupTarget(name string) (Target, error) {
	t, ok := r.targets[name]
	if !ok {
		return nil, fmt.Errorf("no target named %q; this build has %s",
			name, strings.Join(r.TargetNames(), ", "))
	}
	return t, nil
}

// LookupBackend resolves a configured backend name.
func (r *Registry) LookupBackend(name string) (Backend, error) {
	b, ok := r.backends[name]
	if !ok {
		return nil, fmt.Errorf("no backend named %q; this build has %s",
			name, strings.Join(r.BackendNames(), ", "))
	}
	return b, nil
}

// CheckPairing reports whether a target supports a backend.
//
// A target that lists its backends is saying which adapters its runtime
// actually ships. Pairing it with another one would generate code importing an
// adapter nobody wrote, and the error for that should name the pair rather than
// arrive as a missing module at install time.
func CheckPairing(t Target, backend string) error {
	supported := t.Backends()
	if supported == nil {
		return nil
	}
	for _, name := range supported {
		if name == backend {
			return nil
		}
	}
	return fmt.Errorf("target %q does not support backend %q; it supports %s",
		t.Name(), backend, strings.Join(supported, ", "))
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
