// Package gen is the generator core: what a target is, what a backend is, what
// they are allowed to assume about each other, and what comes out.
//
// It sits between ormcompat, which turns annotations into a schema, and the
// targets, which turn a schema into files. Nothing here knows any language or
// any storage engine.
package gen

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// ConfigVersion is the only document version this calque understands.
//
// It is required rather than optional so that a future format change reports
// "this calque understands config version 1" instead of a cascade of unknown-key
// errors about keys that are perfectly valid in the version the user wrote.
const ConfigVersion = 1

// Config is the calque.yaml document.
type Config struct {
	// Version is the config format version. Required, and must be 1.
	Version int `yaml:"version"`

	// Targets is what to generate. Each entry pairs a language target with a
	// storage backend and says where its files go.
	Targets []TargetConfig `yaml:"targets"`

	// path is the file this came from, for diagnostics.
	path string
	// extra holds top-level sections the core schema does not know, for a
	// target or backend to claim via Section.
	extra map[string]ast.Node
	// claimed marks extra sections something has decoded.
	claimed map[string]bool
	// overrides are settings from the command line, keyed by section. They are
	// applied on top of the file, per section, when it is claimed.
	overrides map[string][]override
}

// override is one `<section>.<key>=<value>` from the plugin's option string.
type override struct {
	// path is the key inside the section, split on dots: `table.apptest.User`
	// is {"table", "apptest", "User"}.
	path []string
	// value is the text as written. It is read as a yaml scalar, so `2` is a
	// number and `true` is a boolean, and anything yaml cannot read is a
	// string -- which is what makes `ts.runtime=@scope/pkg` work.
	value string
	// opt is the whole thing as the user typed it, for diagnostics.
	opt string
}

// TargetConfig is one thing to generate.
type TargetConfig struct {
	// Target is a registered target name, and the key of that target's own
	// config section.
	Target string `yaml:"target"`

	// Backend is a registered backend name. The target must list it in
	// Backends(), so that pairing a language with a store its runtime has no
	// adapter for is refused here rather than discovered at import time.
	Backend string `yaml:"backend"`

	// Out is a subdirectory of the plugin's output root. It is not an output
	// root of its own: buf owns that, and a plugin that thinks otherwise is the
	// `output string` protoc-gen-orm-ts threaded through three layers and then
	// discarded.
	Out string `yaml:"out"`

	// Name disambiguates two entries with the same target. It defaults to
	// Target.
	//
	// It is a label, not a section key: options are claimed under the target's
	// own name, so two entries running the same target read the same section.
	// Naming them apart is for diagnostics and for saying which one emitted a
	// file, not for configuring them differently.
	Name string `yaml:"name"`
}

// Label is how this entry is named in diagnostics and in per-instance sections.
func (t TargetConfig) Label() string {
	if t.Name != "" {
		return t.Name
	}
	return t.Target
}

// KnownSections is the top-level keys the core owns. Everything else is for a
// target or a backend to claim.
func KnownSections() []string { return []string{"version", "targets"} }

// ParseConfig decodes a config that is already in memory.
//
// name is used in error messages. Core keys are decoded strictly; an unknown
// top-level section is kept for [Config.Section], and whatever is left
// unclaimed is an error before any file is produced — so a section no target or
// backend honours cannot pass unnoticed.
func ParseConfig(b []byte, name string) (*Config, error) {
	f, err := parser.ParseBytes(b, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(f.Docs) == 0 || (len(f.Docs) == 1 && f.Docs[0].Body == nil) {
		return nil, fmt.Errorf("%s: config is empty", name)
	}
	if len(f.Docs) != 1 {
		return nil, fmt.Errorf("%s: expected a single yaml document, got %d", name, len(f.Docs))
	}
	mapping, ok := f.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		return nil, fmt.Errorf("%s: config must be a mapping of keys, e.g. `version: 1`", name)
	}

	c := Config{
		path:      name,
		extra:     map[string]ast.Node{},
		claimed:   map[string]bool{},
		overrides: map[string][]override{},
	}

	// Duplicate keys, core or extension, are a parse error: goccy rejects them
	// before this loop runs.
	for _, kv := range mapping.Values {
		key := kv.Key.String()
		var dst any
		switch key {
		case "version":
			dst = &c.Version
		case "targets":
			dst = &c.Targets
		default:
			c.extra[key] = kv.Value
			continue
		}
		if err := yaml.NodeToValue(kv.Value, dst, yaml.Strict()); err != nil {
			return nil, fmt.Errorf("%s: %s: %w", name, key, err)
		}
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	switch {
	case c.Version == 0:
		return fmt.Errorf("%s: `version` is required; this calque understands version %d", c.path, ConfigVersion)
	case c.Version != ConfigVersion:
		return fmt.Errorf("%s: config version %d, but this calque understands version %d", c.path, c.Version, ConfigVersion)
	}

	if len(c.Targets) == 0 {
		return fmt.Errorf("%s: `targets` is empty; list at least one, e.g. `- {target: ts, backend: dexie}`", c.path)
	}

	seen := map[string]int{}
	for i, t := range c.Targets {
		where := fmt.Sprintf("%s: targets[%d]", c.path, i)

		if t.Target == "" {
			return fmt.Errorf("%s: `target` is required (a registered target name)", where)
		}
		if err := checkName(t.Target); err != nil {
			return fmt.Errorf("%s: target: %w", where, err)
		}
		// `backend` is checked for spelling here and for presence in Run, which
		// is the only place that knows whether the target is storeless -- a
		// service contract describes no store and takes no backend at all.
		if t.Backend != "" {
			if err := checkName(t.Backend); err != nil {
				return fmt.Errorf("%s(%s): backend: %w", where, t.Target, err)
			}
		}
		if t.Name != "" {
			if err := checkName(t.Name); err != nil {
				return fmt.Errorf("%s(%s): name: %w", where, t.Target, err)
			}
		}
		if err := checkOut(t.Out); err != nil {
			return fmt.Errorf("%s(%s): out: %w", where, t.Label(), err)
		}

		label := t.Label()
		if prev, dup := seen[label]; dup {
			return fmt.Errorf("%s: targets[%d] and targets[%d] are both named %q; give one a distinct `name`",
				c.path, prev, i, label)
		}
		seen[label] = i
	}
	return nil
}

// nameRE is the spelling a target, backend or label may have.
//
// It is here so that the json schema and this validator agree. The schema
// declared the pattern from the start and the code did not check it, which meant
// `target: TS` was a schema violation an editor flagged and a build did not --
// it failed later with "no target named" and never said the name was the
// problem.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func checkName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("%q is not a name; it must be lowercase, start with a letter, and hold only letters, digits and dashes", name)
	}
	return nil
}

// checkOut refuses an `out` that is not a relative subdirectory.
//
// buf owns the output root and hands it to the plugin; a path that escapes it
// is not a path to normalize, it is a generator writing somewhere nobody asked
// for.
func checkOut(out string) error {
	if out == "" {
		return nil // means the output root itself
	}
	if path.IsAbs(out) || strings.HasPrefix(out, "/") {
		return fmt.Errorf("%q is absolute; it must be a subdirectory of the output root buf gives the plugin", out)
	}
	for _, seg := range strings.Split(path.Clean(out), "/") {
		if seg == ".." {
			return fmt.Errorf("%q escapes the output root", out)
		}
	}
	return nil
}

// Path is the file this config came from.
func (c *Config) Path() string { return c.path }

// Override records a `<section>.<key>=<value>` setting from the command line,
// to be applied on top of the file when that section is claimed.
//
// It is applied at claim time rather than now because the file's own section is
// decoded first: an override is meant to win over what the file said, and there
// is nothing to win over until a target or backend asks for it. A section
// nobody claims is still reported, so `dexei.compat=none` names the mistake
// instead of doing nothing.
func (c *Config) Override(path, value string) error {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return fmt.Errorf("opt %s=%s: an override is <section>.<key>=<value>", path, value)
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("opt %s=%s: an override has an empty key", path, value)
		}
	}
	section := parts[0]
	c.overrides[section] = append(c.overrides[section], override{
		path:  parts[1:],
		value: value,
		opt:   path + "=" + value,
	})
	return nil
}

// Section strictly decodes the top-level extension section key into v and marks
// it claimed; ok is false when neither the config nor the command line said
// anything about it.
//
// A target or backend calls this for its own options. Decoding strictly is what
// makes a typo inside the section loud; UnclaimedSections is what makes a typo
// in the section *name* loud.
func (c *Config) Section(key string, v any) (ok bool, err error) {
	node, found := c.extra[key]
	overrides := c.overrides[key]
	if !found && len(overrides) == 0 {
		return false, nil
	}
	c.claimed[key] = true

	if found {
		if err := yaml.NodeToValue(node, v, yaml.Strict()); err != nil {
			return true, fmt.Errorf("%s: %s: %w", c.path, key, err)
		}
	}

	// One at a time, so a bad key is reported as the option the user typed
	// rather than as a line in a document they never wrote.
	for _, o := range overrides {
		doc, err := o.document()
		if err != nil {
			return true, fmt.Errorf("opt %s: %w", o.opt, err)
		}
		if err := yaml.UnmarshalWithOptions(doc, v, yaml.Strict()); err != nil {
			return true, fmt.Errorf("opt %s: %w", o.opt, err)
		}
	}
	return true, nil
}

// document renders an override as the fragment of the section it stands for, so
// that applying it is the same decode the file gets rather than a second way to
// set a field.
func (o override) document() ([]byte, error) {
	var v any = scalar(o.value)
	for i := len(o.path) - 1; i >= 0; i-- {
		v = map[string]any{o.path[i]: v}
	}
	return yaml.Marshal(v)
}

// scalar reads an option's value the way the same text in the config file would
// be read, so `quiet: true` and `x.quiet=true` mean one thing.
//
// Text yaml cannot read is the text itself. That is not a fallback so much as
// the common case: `ts.runtime=@scope/pkg` starts with a character yaml
// reserves, and the user is plainly naming a package.
func scalar(raw string) any {
	var v any
	if err := yaml.Unmarshal([]byte(raw), &v); err == nil && v != nil {
		return v
	}
	return raw
}

// UnclaimedSections is the sections nothing claimed, from the file or from the
// command line, sorted.
func (c *Config) UnclaimedSections() []string {
	seen := map[string]bool{}
	var out []string
	for key := range c.extra {
		if !c.claimed[key] && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	for key := range c.overrides {
		if !c.claimed[key] && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// CheckUnclaimed turns leftover sections into an error naming what was
// understood, so a misspelled `dexei:` says what it should have been.
func (c *Config) CheckUnclaimed(registered []string) error {
	left := c.UnclaimedSections()
	if len(left) == 0 {
		return nil
	}
	known := append(KnownSections(), registered...)
	sort.Strings(known)
	return fmt.Errorf("%s: nothing understands %s\n\tthis build knows: %s",
		c.path, strings.Join(quoteAll(left), ", "), strings.Join(known, ", "))
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}
