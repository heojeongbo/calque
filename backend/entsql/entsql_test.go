package entsql_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/backend/entsql"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/gentest"
	"github.com/heojeongbo/calque/schema"
)

// TestContract is what every backend has to hold, plus what this one decided.
func TestContract(t *testing.T) {
	gentest.Run(t, gentest.Case{
		Backend: entsql.New(),
		Paths: map[string]schema.StorePath{
			"apptest.User.id": {"id"},
			// ent's own convention: the owner and the edge, not the edge and
			// `_id`. grdb spells the same edge `tenant_id`, and grdb_test says
			// so -- the two are written down in two tests rather than asserted
			// to agree in a comment, which is how they came to disagree while a
			// comment said they did not.
			"apptest.User.tenant": {"user_tenant"},
		},
		Codecs: map[string]gen.CodecName{
			"apptest.User.id": gen.CodecUUIDString,
			// A native time, where grdb stores epoch millis: SQL has a time
			// type and SQLite does not.
			"apptest.User.date_updated": gen.CodecTimeNative,
			"apptest.User.labels":       gen.CodecJSON,
			"apptest.User.profile":      gen.CodecJSON,
			"apptest.User.tenant":       gen.CodecUUIDString,
		},
		Extra: map[string]map[string]any{
			"apptest.User": {"dialect": "sqlite"},
		},
	})
}

// TestHoldsWhatDexieCannot is the comparison docs/conformance.md item 3 is
// about, from the other side.
func TestHoldsWhatDexieCannot(t *testing.T) {
	s := gentest.Schema(t, "apptest.proto")
	require.Empty(t, gen.CheckCapabilities(s, entsql.New()),
		"ent writes index.Fields(...).Edges(...).Unique() and the database holds it")

	caps := entsql.New().Capabilities()
	require.True(t, caps.UniqueCompoundIndex)
	require.True(t, caps.BinaryKey)
	require.False(t, caps.NestedKeyPath, "SQL has no path into a nested object")
}

// TestEdgeIsAColumnNotAPath: an edge becomes one foreign-key column, which is
// where this backend and Dexie's nested key path genuinely differ.
func TestEdgeIsAColumnNotAPath(t *testing.T) {
	s := gentest.Schema(t, "apptest.proto")
	user := gentest.Entity(t, s, "apptest.User")
	tenant, ok := user.Prop("tenant")
	require.True(t, ok)

	path, err := entsql.New().StorePath(tenant)
	require.NoError(t, err)
	require.Equal(t, schema.StorePath{"user_tenant"}, path)
}

// TestTableNameIsPinned: ent's own default is a snake-cased plural, so leaving
// it alone would rename every table.
func TestTableNameIsPinned(t *testing.T) {
	s := gentest.Schema(t, "apptest.proto")
	b := entsql.New()
	require.Equal(t, "user", b.TableName(gentest.Entity(t, s, "apptest.User")))
	require.Equal(t, "tenant", b.TableName(gentest.Entity(t, s, "apptest.Tenant")))
}

// TestUUIDAgreesWithDexie is conformance item 7: both stores hold the same
// canonical text, and they agree by naming one codec rather than by accident.
func TestUUIDAgreesWithDexie(t *testing.T) {
	s := gentest.Schema(t, "apptest.proto")
	codec, err := entsql.New().Codec(gentest.Entity(t, s, "apptest.User").Key())
	require.NoError(t, err)
	require.Equal(t, gen.CodecUUIDString, codec)
}

// TestMySQLLosesPartialIndexes: a dialect changes what the store can hold, so
// it is a capability and not a formatting option.
func TestMySQLLosesPartialIndexes(t *testing.T) {
	b := entsql.New()
	cfg, err := gen.ParseConfig([]byte("version: 1\ntargets:\n  - {target: go, backend: entsql}\nentsql:\n  dialect: mysql\n"), "calque.yaml")
	require.NoError(t, err)
	require.NoError(t, b.Configure(cfg, "entsql"))

	require.False(t, b.Capabilities().PartialIndex,
		"an entity that erases softly cannot have a unique index over only the live rows")
	require.True(t, b.Capabilities().UniqueCompoundIndex)
}

func TestDialectIsChecked(t *testing.T) {
	b := entsql.New()
	cfg, err := gen.ParseConfig([]byte("version: 1\ntargets:\n  - {target: go, backend: entsql}\nentsql:\n  dialect: oracle\n"), "calque.yaml")
	require.NoError(t, err)
	require.ErrorContains(t, b.Configure(cfg, "entsql"), `dialect "oracle"`)
}

// TestEntIdentIsNotProtocsIdent: the two disagree exactly on initialisms, and
// the Go target has to spell a field both ways.
func TestEntIdentIsNotProtocsIdent(t *testing.T) {
	s := gentest.Schema(t, "naming.proto")
	e := gentest.Entity(t, s, "namingtest.Device")
	p, ok := e.Prop("device_id")
	require.True(t, ok)

	require.Equal(t, "DeviceID", entsql.New().EntIdent(p))
	require.Equal(t, schema.ValueName("deviceId"), p.Names().Value)
}
