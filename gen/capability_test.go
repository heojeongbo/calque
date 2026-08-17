package gen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/gen"
)

// nesting is a store that indexes a path into a nested object, like IndexedDB.
func nesting() *fakeBackend {
	return &fakeBackend{name: "nesting", caps: gen.Capabilities{
		NestedKeyPath:       true,
		UniqueCompoundIndex: true,
		Transactions:        true,
		Codecs:              []gen.CodecName{gen.CodecIdentity, gen.CodecUUIDString},
	}}
}

// flattening is a store that gives an edge a column of its own, like SQL.
func flattening() *fakeBackend {
	return &fakeBackend{name: "flattening", caps: gen.Capabilities{
		NestedKeyPath:       false,
		UniqueCompoundIndex: true,
		Transactions:        true,
		Codecs:              []gen.CodecName{gen.CodecIdentity, gen.CodecUUIDString},
	}}
}

// TestEdgeMembersAreIndexable is the bug this file was added for.
//
// An edge's own type is message, which is never orderable, so asking whether it
// has an order reports every edge in every index as an error. What is indexed is
// the *target's key* -- as `tenant.id` in a store that nests and as a flattened
// column in one that does not -- and either way the type that matters is the
// key's.
//
// The guard used to skip the check only when the backend could NOT nest, which
// is backwards: the store that reaches the key as a nested path is the one that
// most obviously can index it. apptest's `slug` index spans a field and an edge,
// so both backends here have to accept it.
func TestEdgeMembersAreIndexable(t *testing.T) {
	s := fixtureSchema(t)

	for _, b := range []*fakeBackend{nesting(), flattening()} {
		t.Run(b.Name(), func(t *testing.T) {
			found := gen.CheckCapabilities(s, b)
			for _, sf := range found {
				require.NotEqual(t, gen.ShortfallUnorderableMember, sf.Kind,
					"an edge is indexed by its target's key: %s", sf)
			}
		})
	}
}

// accepter is a backend that decides per kind.
type accepter struct {
	*fakeBackend
	accept map[gen.ShortfallKind]bool
}

func (a *accepter) Accepts(k gen.ShortfallKind) bool { return a.accept[k] }

// TestAcceptedShortfallsDoNotStopTheRun: a store that cannot mirror a
// constraint another store holds is a fact about the store, and a build that
// refuses it forever cannot be used as a cache. Naming the kind lets it
// through.
func TestAcceptedShortfallsDoNotStopTheRun(t *testing.T) {
	s := fixtureSchema(t)
	base := documentStore() // no unique compound index, strict
	found := gen.CheckCapabilities(s, base)
	require.NotEmpty(t, found)

	accepted, refused := found.Partition(&accepter{
		fakeBackend: base,
		accept:      map[gen.ShortfallKind]bool{gen.ShortfallUniqueCompoundIndex: true},
	})
	require.NotEmpty(t, accepted, "the named kind is let through")
	require.Empty(t, refused)

	for _, sf := range accepted {
		require.Equal(t, gen.ShortfallUniqueCompoundIndex, sf.Kind)
	}
}

// TestUnacceptedKindsStillStopTheRun is the whole reason this is a list and not
// a boolean: accepting one kind must not accept the next one nobody has looked
// at.
func TestUnacceptedKindsStillStopTheRun(t *testing.T) {
	s := fixtureSchema(t)
	base := documentStore()
	found := gen.CheckCapabilities(s, base)

	_, refused := found.Partition(&accepter{
		fakeBackend: base,
		accept:      map[gen.ShortfallKind]bool{gen.ShortfallBinaryKey: true},
	})
	require.NotEmpty(t, refused, "a kind that was not named still refuses")
}

// TestBackendWithoutAccepterIsJudgedByStrict: the interface is optional, so a
// backend that does not implement it behaves exactly as it did before it
// existed.
func TestBackendWithoutAccepterIsJudgedByStrict(t *testing.T) {
	s := fixtureSchema(t)
	found := gen.CheckCapabilities(s, documentStore())
	require.NotEmpty(t, found)

	_, refused := found.Partition(documentStore())
	require.NotEmpty(t, refused, "strict refuses everything")

	lenient := documentStore()
	lenient.lenient = true
	accepted, refused := found.Partition(lenient)
	require.NotEmpty(t, accepted, "lenient accepts everything")
	require.Empty(t, refused)
}

// TestEveryKindIsListed: AllShortfallKinds is what a config validates against,
// so a kind the check can report and the list does not name is a kind nobody
// can accept.
func TestEveryKindIsListed(t *testing.T) {
	listed := map[gen.ShortfallKind]bool{}
	for _, k := range gen.AllShortfallKinds() {
		require.NotEmpty(t, string(k))
		require.False(t, listed[k], "%s listed twice", k)
		listed[k] = true
	}
	require.Len(t, gen.ShortfallKindNames(), len(gen.AllShortfallKinds()))

	// Every kind the checks can actually produce, gathered from a schema and a
	// backend that can do nothing at all.
	s := fixtureSchema(t)
	nothing := &fakeBackend{name: "nothing", caps: gen.Capabilities{
		Codecs: []gen.CodecName{gen.CodecIdentity, gen.CodecUUIDString},
	}}
	for _, sf := range gen.CheckCapabilities(s, nothing) {
		require.True(t, listed[sf.Kind], "%s is reported and not listed", sf.Kind)
	}
}

// TestShortfallRendersLikeADiagnostic: the path and the hint are what a user
// reads, and they follow the same shape as a schema diagnostic.
func TestShortfallRendersLikeADiagnostic(t *testing.T) {
	sf := gen.Shortfall{
		Kind: gen.ShortfallBinaryKey,
		Path: "apptest.Session.token_hash",
		Msg:  "backend \"dexie\" cannot use a byte string as a key",
		Hint: "store it as text instead",
	}
	require.Equal(t,
		"apptest.Session.token_hash: backend \"dexie\" cannot use a byte string as a key\n\tstore it as text instead",
		sf.String())

	require.NoError(t, gen.Shortfalls(nil).Err(), "nothing found is not an error")
}
