package ormcompat_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	"github.com/heojeongbo/calque/schema"
)

// TestReferenceSchema parses a real schema from outside this repository.
//
// A fixture is written by whoever writes the parser, so it tests what they
// already thought of. A schema somebody else wrote, for a system in production,
// is the only thing that finds the rest -- and this parser has to be right
// about a schema like that before any emitter is worth writing.
//
// Nothing is copied in. CALQUE_REFERENCE_PROTO points at the proto root of a
// project that already uses these annotations, and the test is skipped when it
// is unset, so this repository never carries anybody else's data model and CI
// does not need one:
//
//	CALQUE_REFERENCE_PROTO=~/path/to/interfaces/oasys/proto go test ./ormcompat/
//
// It asserts shape rather than specifics -- how many entities, that each has a
// key, that every prop has a type and both names -- because the schema it reads
// belongs to someone else and will change without telling this test.
func TestReferenceSchema(t *testing.T) {
	root := os.Getenv("CALQUE_REFERENCE_PROTO")
	if root == "" {
		t.Skip("set CALQUE_REFERENCE_PROTO to a proto root that uses (orm.*) annotations")
	}
	root = expand(t, root)

	files := protoFilesUnder(t, root)
	require.NotEmpty(t, files, "no .proto files under %s", root)

	upstream, err := filepath.Abs("../testdata/proto/_upstream")
	require.NoError(t, err)

	req, err := protoc.CompileRequest(t.Context(), []string{root, upstream}, "", files...)
	require.NoError(t, err, "the reference protos must compile")

	s, err := ormcompat.Parse(req)
	require.NoError(t, err, "a schema that is in production must parse")

	entities := s.Entities()
	require.NotEmpty(t, entities, "no entities found; is this the right proto root?")
	t.Logf("parsed %d entities from %s", len(entities), root)

	for _, e := range entities {
		t.Run(e.FullName(), func(t *testing.T) {
			require.NotNil(t, e.Key(), "every entity has exactly one key")
			require.True(t, e.Key().IsUnique())
			require.True(t, e.Key().IsImmutable())

			for _, p := range e.Props() {
				require.NotEqual(t, schema.TypeUnspecified, p.Type(),
					"%s has no type", p.Name())
				require.NotEmpty(t, p.Names().Proto, "a prop with no proto name")
				require.NotEmpty(t, p.Names().Value,
					"%s has no value name; the JSON name comes from the descriptor", p.Name())

				if edge, ok := p.(*schema.Edge); ok {
					require.NotNil(t, edge.Target(),
						"%s points at nothing after Build", p.Name())
					require.NotNil(t, edge.Target().Key(),
						"%s points at an entity with no key", p.Name())
				}
			}

			for _, idx := range e.Indexes() {
				require.NotEmpty(t, idx.Props(), "index %s resolved to nothing", idx.Name())
			}
		})
	}
}

func expand(t *testing.T, path string) string {
	t.Helper()
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		path = filepath.Join(home, path[2:])
	}
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	return abs
}

// protoFilesUnder lists the hand-written .proto files below root, named the way
// an import names them: slash-separated and relative to root.
//
// Files ending in .g.proto are left out. They are service contracts another
// generator wrote, they carry no (orm.*) annotations of their own, and they
// tend to import things -- opentelemetry, in the schema this was first pointed
// at -- that have nothing to do with whether calque can read a schema. The
// entities are in the hand-written files, which is what this test is about.
func protoFilesUnder(t *testing.T, root string) []string {
	t.Helper()

	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}
		if strings.HasSuffix(path, ".g.proto") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err)
	return out
}
