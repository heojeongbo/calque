// Package plugin is the protoc-plugin boundary: a CodeGeneratorRequest in, a
// CodeGeneratorResponse out, and the option string in between.
package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Params is what `opt:` said.
type Params struct {
	// Config is the path to calque.yaml, resolved against the plugin process's
	// working directory — which for buf is where `buf generate` ran.
	Config string

	// Overrides are scalar settings applied after the config file is parsed,
	// keyed by dotted path: `dexie.unique_compound=runtime`.
	Overrides map[string]string

	// Manifest, when set, is where to write the list of files this run
	// produced. A plugin can only add files, so pruning is the build's job and
	// this is what it prunes against.
	Manifest string

	// Quiet turns off progress reporting. It does not turn off warnings: a
	// warning is a fact about the result, and hiding one behind a convenience
	// switch is how a constraint the store cannot hold goes unnoticed.
	Quiet bool
}

// ParseParams reads protoc's comma-separated `k=v` parameter string.
//
// A value containing a comma is unrepresentable. That is protoc's format, not
// calque's choice, so a config path with a comma in it says exactly that rather
// than arriving later as a mysterious missing file.
func ParseParams(param string) (*Params, error) {
	p := &Params{Overrides: map[string]string{}}

	seenConfig := false
	for _, entry := range strings.Split(param, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("opt %q is not key=value", entry)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)

		switch key {
		case "config":
			if seenConfig {
				return nil, fmt.Errorf("opt config given more than once")
			}
			seenConfig = true
			p.Config = value
		case "manifest":
			p.Manifest = value
		case "quiet":
			switch value {
			case "true", "1", "":
				p.Quiet = true
			case "false", "0":
				p.Quiet = false
			default:
				return nil, fmt.Errorf("opt quiet=%q is not a boolean", value)
			}
		default:
			// A Go target hands the whole parameter string to protogen, which
			// parses its own options out of it. Those are passed through rather
			// than rejected -- but by name, so a typo is still caught.
			if forProtogen(key) {
				continue
			}
			if !strings.Contains(key, ".") {
				return nil, fmt.Errorf(
					"unknown opt %q; calque understands config=, manifest=, quiet=, <section>.<key>= overrides, "+
						"and the options protoc-gen-go takes", key)
			}
			p.Overrides[key] = value
		}
	}
	return p, nil
}

// forProtogen reports whether an option belongs to protogen rather than to
// calque.
//
// The Go target builds a protogen.Plugin from the same request, so it needs the
// same flags protoc-gen-go was given -- `module=` decides where files land,
// `default_api_level=` decides whether the emitted code says SetX or assigns a
// field. Passing them through keeps calque's buf entry a copy of the
// protoc-gen-go entry, which is the point: two entries that disagree would
// generate code that does not compile, loudly, rather than code that compiles
// and reads the wrong thing.
func forProtogen(key string) bool {
	switch key {
	case "paths", "module", "annotate_code", "default_api_level":
		return true
	}
	// M<file>=<import> and apilevelM<file>=<level> are per-file overrides.
	return strings.HasPrefix(key, "M") || strings.HasPrefix(key, "apilevelM")
}

// ConfigPath is where the config is, or an error saying what to do about it.
//
// With no `config=`, calque looks beside the working directory. A missing file
// names the absolute path it looked for, because "no such file or directory:
// calque.yaml" is unhelpful when the plugin's working directory is not the one
// the user is thinking about.
func (p *Params) ConfigPath() (string, error) {
	if p.Config != "" {
		abs, err := filepath.Abs(p.Config)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("no config at %s (from opt config=%s)", abs, p.Config)
		}
		return abs, nil
	}

	for _, name := range []string{"calque.yaml", "calque.yml"} {
		abs, err := filepath.Abs(name)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}

	wd, _ := os.Getwd()
	return "", fmt.Errorf(
		"no calque.yaml in %s\n\tpass one with `opt: [config=path/to/calque.yaml]`, resolved from where buf runs", wd)
}

// OverrideKeys is the override paths, sorted, for diagnostics.
func (p *Params) OverrideKeys() []string {
	out := make([]string, 0, len(p.Overrides))
	for k := range p.Overrides {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
