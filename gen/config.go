// Package gen is the generator core: what a target is, what a backend is, what
// they are allowed to assume about each other, and what comes out.
//
// It sits between ormcompat, which turns annotations into a schema, and the
// targets, which turn a schema into files. Nothing here knows any language or
// any storage engine.
package gen

import "github.com/heojeongbo/calque/config"

// Config is the calque.yaml document. It lives in [config], which is the
// package that knows how to read one.
//
// These are aliases and not definitions, which matters: *gen.Config and
// *config.Config are the same type, so a Configure written against either one
// satisfies Target and Backend, and every example in docs/extending.md
// compiles word for word. They are re-exported here because Configure takes
// one, and a target should not have to import two packages to implement one
// interface.
type Config = config.Config

// TargetConfig is one entry of `targets:`.
type TargetConfig = config.TargetConfig

// ConfigVersion is the only document version this calque understands.
const ConfigVersion = config.FormatVersion

// KnownSections is the top-level keys the core owns. Everything else is for a
// target or a backend to claim.
func KnownSections() []string { return config.KnownSections() }

// ParseConfig decodes a config that is already in memory.
func ParseConfig(b []byte, name string) (*Config, error) { return config.Parse(b, name) }
