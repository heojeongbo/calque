// Package config is the calque.yaml document: what it says, what a section is,
// and every way a setting can be given.
//
// It is its own package rather than part of gen for two reasons. gen is the
// generator core -- what a target is, what a backend is -- and reading YAML is
// not that. And this is the only package in calque that imports a YAML library,
// which is a claim the README makes about the dependency and one worth being
// able to check with a grep.
//
// gen re-exports Config and TargetConfig as aliases, because a target's
// Configure takes one and a target should not have to import two packages to
// implement one interface.
//
// Four files, one per way a setting arrives:
//
//	config.go    the document, and what the core owns of it
//	validate.go  what a name and an `out` may be
//	section.go   a target or backend claiming its own options
//	opt.go       `<section>.<key>=` from the plugin's option string
package config

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// FormatVersion is the only document version this calque understands.
//
// It versions the shape of the file, which is a different number from the
// plugin's and from the runtime's; see docs/versioning.md. Config.Version is
// what a given document claims to be.
//
// It is required rather than optional so that a future format change reports
// "this calque understands config version 1" instead of a cascade of unknown-key
// errors about keys that are perfectly valid in the version the user wrote.
const FormatVersion = 1

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

// Parse decodes a config that is already in memory.
//
// name is used in error messages. Core keys are decoded strictly; an unknown
// top-level section is kept for [Config.Section], and whatever is left
// unclaimed is an error before any file is produced — so a section no target or
// backend honours cannot pass unnoticed.
func Parse(b []byte, name string) (*Config, error) {
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

// Path is the file this config came from.
func (c *Config) Path() string { return c.path }
