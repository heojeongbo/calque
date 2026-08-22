package gentest

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/schema"
)

// Case is one backend's own answers, checked against the corpus.
//
// Contract says what every backend has to do. This says what *this* one decided:
// which column a prop lives in, what transform its value goes through, what the
// runtime adapter is handed. Those are the store-specific answers, and writing
// them down here rather than in prose is what makes two backends' disagreements
// visible -- grdb spells User's tenant edge `tenant_id` and entsql spells it
// `user_tenant`, and a comment claiming they agreed went unnoticed for exactly
// as long as the two spellings lived in two files nobody read together.
type Case struct {
	// Backend is the thing under test. Run configures it.
	Backend gen.Backend

	// Section is this backend's own YAML, indented under its name, or empty.
	Section string

	// Protos overrides the corpus. Empty means Protos.
	Protos []string

	// Paths is the column or key path a prop lives in, keyed "pkg.Entity.prop"
	// with the prop's proto name.
	Paths map[string]schema.StorePath

	// Codecs is the transform a prop's value goes through, keyed the same way.
	// A prop not named here is not checked; naming every prop of every entity is
	// a golden file, and this is a statement about the interesting ones.
	Codecs map[string]gen.CodecName

	// Extra is what the runtime adapter is handed, keyed by entity full name.
	Extra map[string]map[string]any
}

// Run checks the contract and then this backend's own answers.
func Run(t *testing.T, c Case) {
	t.Helper()
	require.NotNil(t, c.Backend, "a Case needs a backend")

	Contract(t, c.Backend, c.Section)

	Configure(t, c.Backend, c.Section)
	s := Schema(t, c.Protos...)

	if len(c.Paths) > 0 || len(c.Codecs) > 0 {
		byKey := map[string]struct {
			entity *schema.Entity
			prop   schema.Prop
		}{}
		for _, e := range s.Entities() {
			for _, p := range e.Props() {
				byKey[propKey(e, p)] = struct {
					entity *schema.Entity
					prop   schema.Prop
				}{e, p}
			}
		}

		t.Run("the columns it decided", func(t *testing.T) {
			for _, key := range sorted(c.Paths) {
				found, ok := byKey[key]
				require.True(t, ok, "%s is not a prop in the corpus", key)
				got, err := c.Backend.StorePath(found.prop)
				require.NoError(t, err, key)
				require.Equal(t, c.Paths[key], got, "%s", key)
			}
		})

		t.Run("the transforms it chose", func(t *testing.T) {
			for _, key := range sorted(c.Codecs) {
				found, ok := byKey[key]
				require.True(t, ok, "%s is not a prop in the corpus", key)
				got, err := c.Backend.Codec(found.prop)
				require.NoError(t, err, key)
				require.Equal(t, c.Codecs[key], got, "%s", key)
			}
		})
	}

	if len(c.Extra) > 0 {
		t.Run("what it hands the runtime adapter", func(t *testing.T) {
			lowered, err := c.Backend.Lower(s)
			require.NoError(t, err)
			for _, name := range sorted(c.Extra) {
				table, err := lowered.Table(Entity(t, s, name))
				require.NoError(t, err)
				require.Equal(t, c.Extra[name], table.Extra, "%s", name)
			}
		})
	}
}

// sorted keeps a failure list in a stable order, so a diff between two runs is
// about the answers and not about map iteration.
func sorted[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
