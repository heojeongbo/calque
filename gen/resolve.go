package gen

import "fmt"

// Entry is one config entry with its target and backend resolved.
type Entry struct {
	// Config is the entry as the file wrote it.
	Config TargetConfig
	// Target is always set.
	Target Target
	// Backend is nil for a storeless target, which was paired with none.
	Backend Backend
}

// Resolve reads the config against a registry and configures everything it
// names, stopping at the first thing that is wrong.
//
// It is the half of [Run] that needs no schema and produces no files, which is
// what makes `calque config` possible: every error a build can hit before it
// touches a proto is reachable from here.
//
// The order is the point rather than an implementation detail:
//
//  1. what main got wrong, since every message after this assumes the registry
//     describes one coherent build;
//  2. every name, so a typo in the last entry is reported before the first one
//     has been configured;
//  3. every option, so a bad one fails before anything is lowered;
//  4. whatever is left unclaimed, which can only be asked once everything has
//     had its chance to claim.
func Resolve(cfg *Config, r *Registry) ([]Entry, error) {
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("calque: %w", err)
	}
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("%s: nothing to generate", cfg.Path())
	}

	plan := make([]Entry, 0, len(cfg.Targets))
	for _, entry := range cfg.Targets {
		t, err := r.LookupTarget(entry.Target)
		if err != nil {
			return nil, fmt.Errorf("%s: targets(%s): %w", cfg.Path(), entry.Label(), err)
		}
		// A storeless target has no backend to resolve or pair with, and naming
		// one for it is a mistake worth reporting: it would say the output depends
		// on a store when it does not.
		if isStoreless(t) {
			if entry.Backend != "" {
				return nil, fmt.Errorf("%s: targets(%s): target %q emits nothing store-specific, so it takes no `backend` (got %q)",
					cfg.Path(), entry.Label(), t.Name(), entry.Backend)
			}
			plan = append(plan, Entry{Config: entry, Target: t})
			continue
		}
		if entry.Backend == "" {
			return nil, fmt.Errorf("%s: targets(%s): `backend` is required (a registered backend name); only a storeless target may omit it",
				cfg.Path(), entry.Label())
		}
		b, err := r.LookupBackend(entry.Backend)
		if err != nil {
			return nil, fmt.Errorf("%s: targets(%s): %w", cfg.Path(), entry.Label(), err)
		}
		if err := CheckPairing(t, entry.Backend); err != nil {
			return nil, fmt.Errorf("%s: targets(%s): %w", cfg.Path(), entry.Label(), err)
		}
		plan = append(plan, Entry{Config: entry, Target: t, Backend: b})
	}

	// Configure. A target or backend named twice claims its section twice,
	// which is harmless: Section is idempotent.
	for _, p := range plan {
		if err := p.Target.Configure(cfg, p.Target.Name()); err != nil {
			return nil, fmt.Errorf("%s: %w", p.Config.Label(), err)
		}
		if p.Backend == nil {
			continue
		}
		if err := p.Backend.Configure(cfg, p.Backend.Name()); err != nil {
			return nil, fmt.Errorf("%s: %w", p.Config.Label(), err)
		}
	}

	// Now that everything has had its chance to claim, anything left is a
	// section nobody honours.
	if err := cfg.CheckUnclaimed(r.Sections()); err != nil {
		return nil, err
	}
	return plan, nil
}
