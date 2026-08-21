package gen_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	"github.com/heojeongbo/calque/schema"
)

// fixtureSchema is the real apptest fixture, parsed the way production parses
// it. It has a uuid key, an edge, and a unique index spanning a field and an
// edge -- which is the shape the capability rules are about.
func fixtureSchema(t *testing.T) *schema.Schema {
	t.Helper()

	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../testdata/proto/valid", "../testdata/proto/_upstream"},
		"", "apptest.proto")
	require.NoError(t, err)

	s, err := ormcompat.Parse(req)
	require.NoError(t, err)
	return s
}

// --- test doubles ------------------------------------------------------------

type fakeBackend struct {
	name string
	caps gen.Capabilities
	// lenient makes the backend report a capability shortfall rather than
	// refuse, which is what a bug-for-bug compatibility mode does.
	lenient bool
	// codec, when set, overrides what Codec returns, so a test can make a
	// backend name a transform it does not implement.
	codec gen.CodecName
}

func (b *fakeBackend) Name() string                        { return b.name }
func (b *fakeBackend) Capabilities() gen.Capabilities      { return b.caps }
func (b *fakeBackend) Configure(*gen.Config, string) error { return nil }
func (b *fakeBackend) Strict() bool                        { return !b.lenient }

func (b *fakeBackend) StorePath(p schema.Prop) (schema.StorePath, error) {
	return schema.StorePath{schema.StoreName(p.Name())}, nil
}

func (b *fakeBackend) Codec(p schema.Prop) (gen.CodecName, error) {
	if b.codec != "" {
		return b.codec, nil
	}
	if p.Type() == schema.TypeUUID {
		return gen.CodecUUIDString, nil
	}
	return gen.CodecIdentity, nil
}

func (b *fakeBackend) Lower(s *schema.Schema) (*gen.Lowered, error) {
	l := &gen.Lowered{Schema: s, Backend: b.name, Tables: map[*schema.Entity]*gen.Table{}}
	for _, e := range s.Entities() {
		table := &gen.Table{Entity: e, Codec: map[schema.Prop]gen.CodecName{}}
		for _, p := range e.Props() {
			codec, err := b.Codec(p)
			if err != nil {
				return nil, err
			}
			table.Codec[p] = codec
		}
		l.Tables[e] = table
	}
	return l, nil
}

// documentStore is Dexie-shaped: no unique compound index, no partial index,
// no binary key.
func documentStore() *fakeBackend {
	return &fakeBackend{
		name: "doc",
		caps: gen.Capabilities{
			UniqueCompoundIndex: false,
			PartialIndex:        false,
			NestedKeyPath:       true,
			BinaryKey:           false,
			Transactions:        true,
			Codecs:              []gen.CodecName{gen.CodecIdentity, gen.CodecUUIDString, gen.CodecJSON},
		},
	}
}

// relationalStore is ent-shaped: it holds a unique compound index.
func relationalStore() *fakeBackend {
	return &fakeBackend{
		name: "rel",
		caps: gen.Capabilities{
			UniqueCompoundIndex: true,
			PartialIndex:        true,
			NestedKeyPath:       false,
			BinaryKey:           true,
			Transactions:        true,
			Codecs:              []gen.CodecName{gen.CodecIdentity, gen.CodecUUIDString, gen.CodecJSON},
		},
	}
}

type fakeTarget struct {
	name     string
	backends []string
	emit     func(g *gen.Generator) ([]gen.File, error)
}

func (t *fakeTarget) Name() string                        { return t.name }
func (t *fakeTarget) Backends() []string                  { return t.backends }
func (t *fakeTarget) Configure(*gen.Config, string) error { return nil }
func (t *fakeTarget) Emit(g *gen.Generator) ([]gen.File, error) {
	if t.emit != nil {
		return t.emit(g)
	}
	var out []gen.File
	for _, e := range g.Sources() {
		out = append(out, gen.File{Name: e.Name() + ".txt", Body: []byte(e.FullName())})
	}
	return out, nil
}

// storelessTarget is a fakeTarget that declares gen.Storeless.
//
// Embedding rather than reimplementing keeps the two identical apart from the one
// method that is the point.
type storelessTarget struct{ fakeTarget }

func (t *storelessTarget) Storeless() {}

func config(t *testing.T, doc string) *gen.Config {
	t.Helper()
	c, err := gen.ParseConfig([]byte(doc), "calque.yaml")
	require.NoError(t, err)
	return c
}

// --- the tests ---------------------------------------------------------------

func TestRunEmitsPerTarget(t *testing.T) {
	s := fixtureSchema(t)
	r := gen.NewRegistry().
		Target(&fakeTarget{name: "ts"}).
		Backend(relationalStore())
	r.Backend(documentStore())

	out, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: rel, out: ts}\n"), r)
	require.NoError(t, err)

	require.Equal(t, []string{"ts/Tenant.txt", "ts/User.txt"}, out.Names(),
		"files land under the target's `out` subdirectory")
	body, ok := out.Body("ts/User.txt")
	require.True(t, ok)
	require.Equal(t, "apptest.User", string(body))
	require.Equal(t, "ts", out.Producer("ts/User.txt"))
}

// TestDocumentStoreRefusesUniqueCompoundIndex is conformance item 3, as a test.
//
// The apptest fixture's `slug` index is unique and spans a field and an edge.
// A relational store holds it. A document store does not, and must say so
// rather than emit a plain index and let the constraint quietly not exist.
func TestDocumentStoreRefusesUniqueCompoundIndex(t *testing.T) {
	s := fixtureSchema(t)

	require.Empty(t, gen.CheckCapabilities(s, relationalStore()),
		"a store that holds a unique compound index has nothing to complain about")

	found := gen.CheckCapabilities(s, documentStore())
	err := found.Err()
	require.Error(t, err, "silently dropping the constraint is the bug this exists to prevent")
	require.ErrorContains(t, err, "cannot enforce a unique index over 2 properties")
	require.ErrorContains(t, err, "would be created and would not be unique")
	require.ErrorContains(t, err, "apptest.User.{indexes}(slug)")

	// The kind is what a config names to accept it, so it has to be the one the
	// check actually reports.
	require.Equal(t, gen.ShortfallUniqueCompoundIndex, found[0].Kind)
}

func TestRunRefusesBeforeEmitting(t *testing.T) {
	s := fixtureSchema(t)
	emitted := false
	r := gen.NewRegistry().
		Target(&fakeTarget{name: "ts", emit: func(*gen.Generator) ([]gen.File, error) {
			emitted = true
			return nil, nil
		}}).
		Backend(documentStore())

	_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: doc}\n"), r)
	require.Error(t, err)
	require.False(t, emitted, "capability refusal must happen before a single file is rendered")
}

func TestUnknownNamesListWhatExists(t *testing.T) {
	s := fixtureSchema(t)
	r := gen.NewRegistry().Target(&fakeTarget{name: "ts"}).Backend(relationalStore())

	_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: nope, backend: rel}\n"), r)
	require.ErrorContains(t, err, `no target named "nope"`)
	require.ErrorContains(t, err, "this build has ts")

	_, err = gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: nope}\n"), r)
	require.ErrorContains(t, err, `no backend named "nope"`)
	require.ErrorContains(t, err, "this build has rel")
}

// TestPairingIsChecked: a target lists the backends its runtime ships an
// adapter for. Pairing it with another one would generate code importing an
// adapter nobody wrote.
func TestPairingIsChecked(t *testing.T) {
	s := fixtureSchema(t)
	r := gen.NewRegistry().
		Target(&fakeTarget{name: "ts", backends: []string{"doc"}}).
		Backend(relationalStore())
	r.Backend(documentStore())

	_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: rel}\n"), r)
	require.ErrorContains(t, err, `target "ts" does not support backend "rel"`)
	require.ErrorContains(t, err, "it supports doc")
}

// TestTwoProducersOneFile: the losing half of a silent overwrite is a file
// somebody meant to ship.
func TestTwoProducersOneFile(t *testing.T) {
	s := fixtureSchema(t)
	same := func(*gen.Generator) ([]gen.File, error) {
		return []gen.File{{Name: "shared.txt", Body: []byte("x")}}, nil
	}
	r := gen.NewRegistry().
		Target(&fakeTarget{name: "a", emit: same}).
		Target(&fakeTarget{name: "b", emit: same}).
		Backend(relationalStore())

	_, err := gen.Run(s, config(t, `
version: 1
targets:
  - {target: a, backend: rel}
  - {target: b, backend: rel}
`), r)
	require.ErrorContains(t, err, "both emit shared.txt")
	require.ErrorContains(t, err, "a and b")
}

func TestEmittedPathsStayInsideTheRoot(t *testing.T) {
	s := fixtureSchema(t)
	for _, name := range []string{"/abs.txt", "../up.txt", "ts/../../out.txt"} {
		t.Run(name, func(t *testing.T) {
			r := gen.NewRegistry().
				Target(&fakeTarget{name: "ts", emit: func(*gen.Generator) ([]gen.File, error) {
					return []gen.File{{Name: name, Body: []byte("x")}}, nil
				}}).
				Backend(relationalStore())

			_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: rel}\n"), r)
			require.Error(t, err)
		})
	}
}

// TestUnclaimedSectionStopsTheRun ties the config's claim mechanism to the run:
// a section nobody honours is an error, not a no-op.
func TestUnclaimedSectionStopsTheRun(t *testing.T) {
	s := fixtureSchema(t)
	r := gen.NewRegistry().Target(&fakeTarget{name: "ts"}).Backend(relationalStore())

	_, err := gen.Run(s, config(t, `
version: 1
targets:
  - {target: ts, backend: rel}

dexei:
  database_version: 1
`), r)
	require.ErrorContains(t, err, `nothing understands "dexei"`)
}

// TestCodecMustBeImplemented catches a calque bug rather than a user's, and
// says so.
func TestCodecMustBeImplemented(t *testing.T) {
	s := fixtureSchema(t)
	b := relationalStore()
	b.codec = gen.CodecName("invented")

	r := gen.NewRegistry().Target(&fakeTarget{name: "ts"}).Backend(b)
	_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: rel}\n"), r)
	require.ErrorContains(t, err, `codec "invented"`)
	require.ErrorContains(t, err, "this is a calque bug")
}

func TestManifestListsEverything(t *testing.T) {
	s := fixtureSchema(t)
	r := gen.NewRegistry().Target(&fakeTarget{name: "ts"}).Backend(relationalStore())

	out, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: rel}\n"), r)
	require.NoError(t, err)

	manifest := string(out.Manifest())
	require.Contains(t, manifest, "Tenant.txt")
	require.Contains(t, manifest, "User.txt")
	require.Contains(t, manifest, "stale", "the header has to say what the list is for")
}

// TestRegistryRefusesDuplicateNames: two things under one name can only come
// from a main written wrong, and the message has to say which two.
func TestRegistryRefusesDuplicateNames(t *testing.T) {
	r := gen.NewRegistry().Target(&fakeTarget{name: "ts"}).Target(&fakeTarget{name: "ts"})
	require.ErrorContains(t, r.Err(), `two targets named "ts"`)

	r = gen.NewRegistry().Backend(relationalStore()).Backend(relationalStore())
	require.ErrorContains(t, r.Err(), `two backends named "rel"`)

	// And Run reports it before anything else, so it is not discovered from
	// output that silently came from whichever one won.
	s := fixtureSchema(t)
	bad := gen.NewRegistry().
		Target(&fakeTarget{name: "ts"}).
		Target(&fakeTarget{name: "ts"}).
		Backend(relationalStore())
	_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: rel}\n"), bad)
	require.ErrorContains(t, err, `two targets named "ts"`)
}

var _ = fmt.Sprintf // keep fmt if the file is trimmed

// --- storeless targets -------------------------------------------------------

// TestStorelessTargetNeedsNoBackend: the whole point of the interface.
//
// It also asserts what a storeless target is handed: nil where the store's
// decisions would be. A target that reaches for them is a bug, and one that does
// not cannot be refused for a shortfall in a store it never asked about.
func TestStorelessTargetNeedsNoBackend(t *testing.T) {
	s := fixtureSchema(t)

	var sawLowered, sawBackend bool
	tgt := &storelessTarget{fakeTarget{name: "svc", emit: func(g *gen.Generator) ([]gen.File, error) {
		sawLowered = g.Lowered() != nil
		sawBackend = g.Backend() != nil
		return []gen.File{{Name: "x.proto", Body: []byte("edition = \"2023\";\n")}}, nil
	}}}

	// A strict backend that refuses everything is registered and must not run.
	r := gen.NewRegistry().Target(tgt).Backend(&fakeBackend{name: "strict", caps: gen.Capabilities{}})

	out, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - target: svc\n"), r)
	require.NoError(t, err)
	require.Equal(t, []string{"x.proto"}, out.Names())
	require.False(t, sawLowered, "a storeless target gets no Lowered")
	require.False(t, sawBackend, "a storeless target gets no Backend")
}

// TestStorelessTargetRefusesABackend: naming one says the output depends on a
// store, and it does not.
func TestStorelessTargetRefusesABackend(t *testing.T) {
	s := fixtureSchema(t)
	r := gen.NewRegistry().
		Target(&storelessTarget{fakeTarget{name: "svc"}}).
		Backend(&fakeBackend{name: "store"})

	_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: svc, backend: store}\n"), r)
	require.ErrorContains(t, err, "emits nothing store-specific, so it takes no `backend`")
	require.ErrorContains(t, err, `got "store"`)
}

// TestBackendIsRequiredForAnythingElse: moving the check out of config validation
// must not lose it.
func TestBackendIsRequiredForAnythingElse(t *testing.T) {
	s := fixtureSchema(t)
	r := gen.NewRegistry().Target(&fakeTarget{name: "ts"}).Backend(&fakeBackend{name: "store"})

	_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - target: ts\n"), r)
	require.ErrorContains(t, err, "`backend` is required")
	require.ErrorContains(t, err, "only a storeless target may omit it")
}

// TestAnAcceptedShortfallIsStillReported is why WithProgress and WithWarnings
// are two options and not one.
//
// A backend that accepts a shortfall has agreed to live with it, not to stop
// hearing about it: the constraint the store cannot hold is a fact about the
// output, and it is reported on every build. `quiet=` turns progress off and
// reaches this not at all -- which the plugin spells by passing a discarding
// Progress and the real stderr.
func TestAnAcceptedShortfallIsStillReported(t *testing.T) {
	s := fixtureSchema(t)
	lenient := documentStore()
	lenient.lenient = true

	r := gen.NewRegistry().Target(&fakeTarget{name: "ts"}).Backend(lenient)

	var warnings, progress bytes.Buffer
	_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: doc}\n"), r,
		gen.WithWarnings(&warnings),
		gen.WithProgress(gen.NewProgress(&progress)))
	require.NoError(t, err, "a lenient backend generates rather than refusing")

	require.Contains(t, warnings.String(), "cannot enforce a unique index over 2 properties")
	require.NotContains(t, progress.String(), "unique",
		"a warning belongs on the stream `quiet` cannot switch off")

	// And with progress discarded -- what `quiet=true` does -- the warning is
	// still there.
	warnings.Reset()
	lenient2 := documentStore()
	lenient2.lenient = true
	r2 := gen.NewRegistry().Target(&fakeTarget{name: "ts"}).Backend(lenient2)
	_, err = gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: doc}\n"), r2,
		gen.WithWarnings(&warnings),
		gen.WithProgress(gen.NewProgress(nil)))
	require.NoError(t, err)
	require.Contains(t, warnings.String(), "cannot enforce a unique index")
}
