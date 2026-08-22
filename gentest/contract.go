package gentest

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/schema"
)

// Contract runs every invariant a backend has to hold, whatever store it is.
//
// Each one is a rule docs/extending.md states in prose. A rule stated in prose
// is a rule the next backend's author may or may not have read; a rule that is a
// failing test is one they cannot ship past.
//
// section is the backend's own YAML, as Config takes it, so a backend whose
// answers depend on an option can be measured in the configuration that matters.
func Contract(t *testing.T, b gen.Backend, section string) {
	t.Helper()

	// A backend's name is the value of `backend:` in a config, so it has to be
	// spellable as one -- which config/validate.go decides and nothing checked
	// from this side. Asking the real validator rather than restating its regex
	// is what keeps the two from drifting.
	t.Run("its name can be written in a config", func(t *testing.T) {
		doc := "version: 1\ntargets:\n  - {target: t, backend: " + b.Name() + "}\n"
		_, err := gen.ParseConfig([]byte(doc), "calque.yaml")
		require.NoError(t, err, "backend %q cannot be named by the config that selects it", b.Name())
	})

	// Not finding a section is not an error: it means nobody configured this
	// backend, and its defaults stand. A backend that requires its section is one
	// that cannot be added to an existing calque.yaml without editing it.
	t.Run("it configures from a document that says nothing about it", func(t *testing.T) {
		require.NoError(t, b.Configure(Config(t, b, ""), b.Name()))
	})

	// Section decodes strictly, so a typo inside a backend's own section is loud.
	// A backend that decodes leniently turns a misspelled option into a default
	// nobody chose.
	t.Run("it refuses an option it does not know", func(t *testing.T) {
		section := b.Name() + ":\n  no_such_option_bd7f: 1\n"
		err := b.Configure(Config(t, b, section), b.Name())
		require.Error(t, err, "a misspelled option should be reported, not defaulted")
	})

	// Resolve configures a backend once per entry that names it, so a config
	// listing two targets over one store configures it twice. A backend that
	// accumulates rather than replaces would answer differently the second time.
	t.Run("configuring twice says the same thing", func(t *testing.T) {
		Configure(t, b, section)
		first := b.Capabilities()
		Configure(t, b, section)
		require.Equal(t, first, b.Capabilities(), "Configure should replace what it decoded, not add to it")
	})

	Configure(t, b, section)
	s := Schema(t)

	lowered, err := b.Lower(s)
	require.NoError(t, err, "lowering the corpus")
	require.NotNil(t, lowered)

	// Cover Entities(), not Sources(). An entity reachable only as an edge target
	// is not emitted and a target still asks what its key looks like, which
	// docs/extending.md had to say in prose because nothing enforced it.
	t.Run("it lowers every entity, not only the ones that get emitted", func(t *testing.T) {
		require.Equal(t, names(s.Entities()), loweredNames(lowered),
			"Lower should cover Schema.Entities()")
	})

	// The corpus cannot tell the two apart -- everything in it is generated -- so
	// the rule is checked against a file that generates no entity of its own and
	// imports two. A backend that lowered Sources() lowers nothing here, and
	// every question a target asks about an edge target becomes "did not lower".
	t.Run("it lowers an entity that is imported rather than generated", func(t *testing.T) {
		imported := Schema(t, importedOnly)
		require.Empty(t, imported.Sources(), "%s should generate no entity of its own", importedOnly)
		require.NotEmpty(t, imported.Entities(), "%s should reach entities through its import", importedOnly)

		l, err := b.Lower(imported)
		require.NoError(t, err)
		require.Equal(t, names(imported.Entities()), loweredNames(l),
			"Lower should cover Schema.Entities() and not Schema.Sources()")
	})

	t.Run("every prop has a codec", func(t *testing.T) {
		for _, e := range s.Entities() {
			table, err := lowered.Table(e)
			require.NoError(t, err)
			for _, p := range e.Props() {
				_, ok := table.Codec[p]
				require.True(t, ok, "%s.%s has no codec", e.FullName(), p.Name())
			}
		}
	})

	// gen.Run checks this too, at run time, against whatever schema that build
	// happened to have. Here it is checked against the corpus before anything
	// ships, which is the difference between a bug someone else's build reports
	// and one this repository's test suite does.
	t.Run("every codec it chose is one it says it implements", func(t *testing.T) {
		caps := b.Capabilities()
		for _, e := range s.Entities() {
			table, err := lowered.Table(e)
			require.NoError(t, err)
			for p, codec := range table.Codec {
				require.True(t, caps.Supports(codec),
					"%s.%s lowered to codec %q, which Capabilities does not list", e.FullName(), p.Name(), codec)
			}
		}
	})

	t.Run("it names only codecs calque knows", func(t *testing.T) {
		seen := map[gen.CodecName]bool{}
		for _, c := range b.Capabilities().Codecs {
			require.False(t, seen[c], "codec %q is listed twice", c)
			seen[c] = true
			require.True(t, knownCodecs[c], "codec %q is not one calque defines", c)
		}
	})

	// A store path is where a value lives. An empty one is not an answer, and a
	// panic is what the predecessor did -- internal/policy can see the literal
	// spelling of that and not a nil dereference, which is what running the whole
	// corpus through is for.
	t.Run("it answers a store path for every prop", func(t *testing.T) {
		for _, e := range s.Entities() {
			for _, p := range e.Props() {
				path, err := b.StorePath(p)
				require.NoError(t, err, "%s.%s", e.FullName(), p.Name())
				require.NotEmpty(t, path, "%s.%s has an empty store path", e.FullName(), p.Name())
				for i, n := range path {
					require.NotEmpty(t, n, "%s.%s has an empty name at position %d", e.FullName(), p.Name(), i)
				}
			}
		}
	})

	// Strict is all-or-nothing and Accepts refines it per kind. A backend that
	// implements the optional interface is answering for every kind whether it
	// meant to or not, so the answer has to be one it would give twice.
	t.Run("what it accepts is total and does not change", func(t *testing.T) {
		acc, ok := b.(gen.ShortfallAccepter)
		if !ok {
			t.Skip("this backend has no per-kind opinion; Strict speaks for it")
		}
		for _, kind := range gen.AllShortfallKinds() {
			require.Equal(t, acc.Accepts(kind), acc.Accepts(kind), "Accepts(%q) is not stable", kind)
		}
	})
}

// importedOnly is a corpus file that declares no entity and imports a file that
// declares two, which is the only shape that separates Schema.Entities() from
// Schema.Sources().
const importedOnly = "apptest_svc.proto"

// knownCodecs is every transform calque defines, so a backend cannot list one
// that exists only in its own head.
var knownCodecs = map[gen.CodecName]bool{
	gen.CodecIdentity:        true,
	gen.CodecUUIDString:      true,
	gen.CodecUUIDBytes:       true,
	gen.CodecTimeEpochMillis: true,
	gen.CodecTimeNative:      true,
	gen.CodecJSON:            true,
}

func names(entities []*schema.Entity) []string {
	out := make([]string, 0, len(entities))
	for _, e := range entities {
		out = append(out, e.FullName())
	}
	sort.Strings(out)
	return out
}

func loweredNames(l *gen.Lowered) []string {
	out := make([]string, 0, len(l.Tables))
	for e := range l.Tables {
		out = append(out, e.FullName())
	}
	sort.Strings(out)
	return out
}

func propKey(e *schema.Entity, p schema.Prop) string {
	return fmt.Sprintf("%s.%s", e.FullName(), p.Name())
}
