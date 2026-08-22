package grdb_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/backend/grdb"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/gentest"
	"github.com/heojeongbo/calque/schema"
)

// TestContract is what every backend has to hold, and what this one shipped
// without: grdb was written, registered and generating Swift with no test in its
// package at all.
func TestContract(t *testing.T) {
	gentest.Run(t, gentest.Case{
		Backend: grdb.New(),

		Paths: map[string]schema.StorePath{
			"apptest.User.id": {"id"},
			// An edge is one flat column, because SQL has no path to reach
			// through. entsql spells this same edge `user_tenant`; the two are
			// written down here and in entsql_test so a claim that they agree
			// cannot be made without one of these two tests contradicting it.
			"apptest.User.tenant": {"tenant_id"},
		},

		Codecs: map[string]gen.CodecName{
			// Text rather than sixteen bytes, even though SQLite would hold the
			// bytes: the server stores the same text, so an id copied across is
			// the same string and a person reading the local database sees the
			// one they can search the server logs for.
			"apptest.User.id": gen.CodecUUIDString,
			// SQLite has no time type, and GRDB's default is a string in an
			// unstated zone -- which is how two devices disagree about which of
			// two writes is newer.
			"apptest.User.date_updated": gen.CodecTimeEpochMillis,
			"apptest.User.labels":       gen.CodecJSON,
			"apptest.User.profile":      gen.CodecJSON,
			"apptest.User.alias":        gen.CodecIdentity,
			// An edge takes its target's key's codec.
			"apptest.User.tenant": gen.CodecUUIDString,
		},

		Extra: map[string]map[string]any{
			"apptest.User": {"table": "user"},
		},
	})
}

// TestHoldsWhatDexieCannot is the other half of docs/conformance.md item 3: this
// is a local store on a device, the same place Dexie sits, and it is relational
// -- so the constraints a schema states are constraints it actually keeps.
func TestHoldsWhatDexieCannot(t *testing.T) {
	s := gentest.Schema(t, "apptest.proto")
	require.Empty(t, gen.CheckCapabilities(s, grdb.New()),
		"SQLite holds a unique compound index and a partial one")

	caps := grdb.New().Capabilities()
	require.True(t, caps.UniqueCompoundIndex)
	require.True(t, caps.PartialIndex, "CREATE UNIQUE INDEX ... WHERE date_erased IS NULL")
	require.False(t, caps.NestedKeyPath, "a column is flat")
}

// TestColumnJoinsThePath: StorePath stays a sequence and the backend renders it,
// which is what lets a second backend render the same sequence differently.
func TestColumnJoinsThePath(t *testing.T) {
	s := gentest.Schema(t, "apptest.proto")
	user := gentest.Entity(t, s, "apptest.User")

	tenant, ok := user.Prop("tenant")
	require.True(t, ok)

	col, err := grdb.New().Column(tenant)
	require.NoError(t, err)
	require.Equal(t, "tenant_id", col)
}

// TestTableNameIsUnpluralised: pluralising is a language question with no answer
// a generator should be guessing at.
func TestTableNameIsUnpluralised(t *testing.T) {
	s := gentest.Schema(t, "apptest.proto")
	require.Equal(t, "user", grdb.TableName(gentest.Entity(t, s, "apptest.User")))
	require.Equal(t, "tenant", grdb.TableName(gentest.Entity(t, s, "apptest.Tenant")))
}

// TestAcceptNamesAKindOrFails: a misspelled kind would accept nothing while
// looking like it accepted something.
func TestAcceptNamesAKindOrFails(t *testing.T) {
	b := grdb.New()
	err := b.Configure(gentest.Config(t, b, "grdb:\n  accept: [no_such_kind]\n"), "grdb")
	require.ErrorContains(t, err, "is not a shortfall kind")
	require.ErrorContains(t, err, "calque knows:")

	b = grdb.New()
	require.NoError(t, b.Configure(gentest.Config(t, b, "grdb:\n  accept: [partial_index]\n"), "grdb"))
	require.True(t, b.Accepts(gen.ShortfallPartialIndex))
	require.False(t, b.Accepts(gen.ShortfallBinaryKey), "naming one kind should not accept the next")
}
