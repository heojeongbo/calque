package gen

import (
	"io"

	"fmt"

	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/heojeongbo/calque/schema"
)

// Run generates everything the config asks for.
//
// The order matters and is the point: resolve names, let every target and
// backend claim its options, reject whatever is left unclaimed, check the
// schema against each backend's capabilities, and only then render. A user who
// misspelled an option finds out before a file exists, not from output that
// quietly used a default.
func Run(s *schema.Schema, cfg *Config, r *Registry, options ...RunOption) (*Output, error) {
	opts := runOpts{warn: io.Discard}
	for _, o := range options {
		o(&opts)
	}
	if opts.progress == nil {
		opts.progress = NewProgress(nil)
	}

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
		// A storeless target has no backend to resolve or pair with, and naming
		// one for it is a mistake worth reporting: it would say the output depends
		// on a store when it does not.
		if isStoreless(t) {
			if entry.Backend != "" {
				return nil, fmt.Errorf("%s: targets(%s): target %q emits nothing store-specific, so it takes no `backend` (got %q)",
					cfg.Path(), entry.Label(), t.Name(), entry.Backend)
			}
			plan = append(plan, resolved{entry: entry, target: t})
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
		plan = append(plan, resolved{entry: entry, target: t, backend: b})
	}

	// Configure. A target or backend named twice claims its section twice,
	// which is harmless: Section is idempotent.
	for _, p := range plan {
		if err := p.target.Configure(cfg, p.target.Name()); err != nil {
			return nil, fmt.Errorf("%s: %w", p.entry.Label(), err)
		}
		if p.backend == nil {
			continue
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

	opts.progress.Start(len(s.Sources()), len(plan))

	for _, p := range plan {
		opts.progress.TargetStart(p.entry.Label(), backendName(p.backend))

		// A storeless target skips all of this. Capabilities are facts about a
		// store, lowering is a store's decisions, and neither is a question about
		// output that describes no store -- running them anyway would let a
		// shortfall in a backend this target does not use stop a file it does.
		var lowered *Lowered
		if p.backend != nil {
			// Facts first, then policy. Every shortfall is computed and reported
			// whatever happens to it; the backend only decides which ones stop the
			// run. An accepted one still goes to stderr on every build, because a
			// constraint the store cannot hold does not stop being worth saying
			// once someone has agreed to live with it.
			accepted, refused := CheckCapabilities(s, p.backend).Partition(p.backend)
			if len(refused) > 0 {
				return nil, fmt.Errorf("%s: %w", p.entry.Label(), refused.Err())
			}
			if len(accepted) > 0 {
				fmt.Fprintf(opts.warn, "calque: %s: %v\n", p.entry.Label(), accepted.Err())
			}

			var err error
			lowered, err = p.backend.Lower(s)
			if err != nil {
				return nil, fmt.Errorf("%s: lower for %s: %w", p.entry.Label(), p.backend.Name(), err)
			}
			if err := checkCodecs(lowered, p.backend); err != nil {
				return nil, fmt.Errorf("%s: %w", p.entry.Label(), err)
			}
		}

		g := NewGenerator(s, lowered, p.backend, cfg, p.entry, &diags)
		g.req, g.files, g.warn = opts.req, opts.files, opts.warn
		g.progress, g.label = opts.progress, p.entry.Label()
		files, err := p.target.Emit(g)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.entry.Label(), err)
		}
		for _, f := range files {
			if err := out.Add(p.entry.Label(), g.Out(f.Name), f.Body); err != nil {
				return nil, err
			}
		}
		opts.progress.TargetDone(p.entry.Label(), backendName(p.backend), len(files))
	}
	opts.progress.Finish()

	// A target's own diagnostics are about the schema, so they are reported
	// the same way ormcompat's are.
	if err := diags.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// backendName is a backend's name, or "" when a storeless target has none.
func backendName(b Backend) string {
	if b == nil {
		return ""
	}
	return b.Name()
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

// RunOption tunes a run.
type RunOption func(*runOpts)

type runOpts struct {
	req      *pluginpb.CodeGeneratorRequest
	files    *protoregistry.Files
	warn     io.Writer
	progress *Progress
}

// WithDescriptors hands targets the descriptor facts the schema deliberately
// does not carry: services, Ref messages, Go identifiers.
//
// The schema stays the only source of names. This is for everything else.
func WithDescriptors(req *pluginpb.CodeGeneratorRequest, files *protoregistry.Files) RunOption {
	return func(o *runOpts) { o.req, o.files = req, files }
}

// WithWarnings is where a shortfall goes when a backend is not strict. buf
// shows a plugin's stderr, so this reaches the person running the build.
func WithWarnings(w io.Writer) RunOption {
	return func(o *runOpts) {
		if w != nil {
			o.warn = w
		}
	}
}

// WithProgress reports what the run is doing while it does it.
//
// It is separate from WithWarnings on purpose. Progress is a convenience and
// can be turned off; a warning is a fact about the result and cannot. Putting
// them behind one switch would mean `quiet` silently hid a constraint the
// backend could not hold.
func WithProgress(p *Progress) RunOption {
	return func(o *runOpts) { o.progress = p }
}
