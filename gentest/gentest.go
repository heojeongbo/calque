// Package gentest is the contract a backend has to meet, and the harness for
// saying what one backend's own answers are.
//
// docs/conformance.md is what two targets have to agree about, measured. This is
// the same commitment one layer down: the rules docs/extending.md states in
// prose about what a backend must do -- cover every entity, name only codecs it
// implements, answer a store path for every prop, fail loudly on an option it
// does not know -- are rules nothing checked. A backend could be written that
// broke every one of them and `go test ./...` would be green, which is how the
// newest backend came to ship with no tests at all.
//
// It is not internal, because docs/extending.md promises a backend can live in
// somebody else's module and that promise is worth nothing if the contract it
// has to meet cannot be imported.
package gentest

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	"github.com/heojeongbo/calque/schema"
)

// Protos is the corpus a backend is measured against when a Case names none.
//
// The three that declare entities. apptest is the shape of a real schema -- a
// uuid key, a required edge, a nullable edge, a version stamp, a unique index
// spanning a field and an edge; erased is soft deletion; naming is the prop
// spellings that differ between the proto and a decoded value, which is the bug
// the name types exist for.
var Protos = []string{"apptest.proto", "erased.proto", "naming.proto"}

// root is this package's own directory, so the corpus is found from whatever
// package is running the test rather than from a relative path that is only
// correct in the one it was written in.
func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

// ImportPaths is where the corpus and the vocabulary live.
func ImportPaths() []string {
	r := root()
	return []string{
		filepath.Join(r, "..", "testdata", "proto", "valid"),
		filepath.Join(r, "..", "testdata", "proto", "_upstream"),
	}
}

// Schema compiles corpus protos in process and reads the annotations, which is
// the same path a build takes and deliberately not a hand-built schema.
func Schema(t *testing.T, files ...string) *schema.Schema {
	t.Helper()
	if len(files) == 0 {
		files = Protos
	}
	req, err := protoc.CompileRequest(context.Background(), ImportPaths(), "", files...)
	require.NoError(t, err, "compiling the corpus")

	s, err := ormcompat.Parse(req)
	require.NoError(t, err, "reading the corpus annotations")
	return s
}

// Entity looks one up by full name, failing rather than returning a nil to
// dereference.
func Entity(t *testing.T, s *schema.Schema, fullName string) *schema.Entity {
	t.Helper()
	e, ok := s.Get(fullName)
	require.True(t, ok, "%s is not in the corpus", fullName)
	return e
}

// Config builds the document a backend is configured from.
//
// section is the backend's own YAML, indented, or empty for none. The targets
// entry names no registered target on purpose: this is the half of a config that
// exists before any registry does, and Resolve is what pairs names with things.
func Config(t *testing.T, b gen.Backend, section string) *gen.Config {
	t.Helper()
	doc := "version: 1\ntargets:\n  - {target: t, backend: " + b.Name() + "}\n" + section
	cfg, err := gen.ParseConfig([]byte(doc), "calque.yaml")
	require.NoError(t, err, "the config a backend test builds should parse")
	return cfg
}

// Configure claims the backend's section from a document the caller wrote.
func Configure(t *testing.T, b gen.Backend, section string) {
	t.Helper()
	require.NoError(t, b.Configure(Config(t, b, section), b.Name()))
}
