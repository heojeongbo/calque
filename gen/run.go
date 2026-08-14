package gen

import (
	"fmt"

	"github.com/HeoJeongBo/calque/schema"
)

// Run generates everything the config asks for.
//
// The order matters and is the point: resolve names, let every target and
// backend claim its options, reject whatever is left unclaimed, check the
// schema against each backend's capabilities, and only then render. A user who
// misspelled an option finds out before a file exists, not from output that
// quietly used a default.
func Run(s *schema.Schema, cfg *Config, r *Registry) (*Output, error) {
	// What main got wrong comes first: every message after this assumes the
	// registry describes one coherent build.
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("calque: %w", err)
	}
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("%s: nothing to generate", cfg.Path())
	}

	type resolved struct {
		entry   TargetConfig
		target  Target
		backend Backend
	}

	// Resolve every name first, so a typo in the last entry is reported before
	// the first one is configured.
	plan := make([]resolved, 0, len(cfg.Targets))
	for _, entry := range cfg.Targets {
		t, err := r.LookupTarget(entry.Target)
		if err != nil {
			return nil, fmt.Errorf("%s: targets(%s): %w", cfg.Path(), entry.Label(), err)
		}
		b, err := r.LookupBackend(entry.Backend)
		if err != nil {
			return nil, fmt.Errorf("%s: targets(%s): %w", cfg.Path(), entry.Label(), err)
		}
		if err := CheckPairing(t, entry.Backend); err != nil {
			return nil, fmt.Errorf("%s: targets(%s): %w", cfg.Path(), entry.Label(), err)
		}
		plan = append(plan, resolved{entry: entry, target: t, backend: b})
	}

	// Configure. A target or backend named twice claims its section twice,
	// which is harmless: Section is idempotent.
	for _, p := range plan {
		if err := p.target.Configure(cfg, p.target.Name()); err != nil {
			return nil, fmt.Errorf("%s: %w", p.entry.Label(), err)
		}
		if err := p.backend.Configure(cfg, p.backend.Name()); err != nil {
			return nil, fmt.Errorf("%s: %w", p.entry.Label(), err)
		}
	}

	// Now that everything has had its chance to claim, anything left is a
	// section nobody honours.
	if err := cfg.CheckUnclaimed(r.Sections()); err != nil {
		return nil, err
	}

	out := NewOutput()
	var diags schema.Diagnostics

	for _, p := range plan {
		if err := CheckCapabilities(s, p.backend); err != nil {
			return nil, fmt.Errorf("%s: %w", p.entry.Label(), err)
		}

		lowered, err := p.backend.Lower(s)
		if err != nil {
			return nil, fmt.Errorf("%s: lower for %s: %w", p.entry.Label(), p.backend.Name(), err)
		}
		if err := checkCodecs(lowered, p.backend); err != nil {
			return nil, fmt.Errorf("%s: %w", p.entry.Label(), err)
		}

		g := NewGenerator(s, lowered, p.backend, cfg, p.entry, &diags)
		files, err := p.target.Emit(g)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.entry.Label(), err)
		}
		for _, f := range files {
			if err := out.Add(p.entry.Label(), g.Out(f.Name), f.Body); err != nil {
				return nil, err
			}
		}
	}

	// A target's own diagnostics are about the schema, so they are reported
	// the same way ormcompat's are.
	if err := diags.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// checkCodecs verifies that every transform a lowering chose is one the backend
// says it implements.
//
// This catches a calque bug rather than a user's, and says so: a backend naming
// a codec its runtime does not have would otherwise surface as a missing export
// in someone else's build.
func checkCodecs(l *Lowered, b Backend) error {
	caps := b.Capabilities()
	for entity, table := range l.Tables {
		for prop, codec := range table.Codec {
			if !caps.Supports(codec) {
				return fmt.Errorf(
					"backend %q lowered %s.%s to codec %q, which it does not list as implemented (this is a calque bug)",
					b.Name(), entity.FullName(), prop.Name(), codec)
			}
		}
	}
	return nil
}
