package ormcompat_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/HeoJeongBo/calque/internal/protoc"
	"github.com/HeoJeongBo/calque/ormcompat"
	"github.com/HeoJeongBo/calque/schema"
)

// importPaths are the fixture root and the real upstream vocabulary, so a
// fixture writes `import "orm.proto"` exactly as a user's proto does.
var importPaths = []string{
	"../testdata/proto/valid",
	"../testdata/proto/invalid",
	"../testdata/proto/_upstream",
}

func parse(t *testing.T, files ...string) (*schema.Schema, error) {
	t.Helper()

	req, err := protoc.CompileRequest(t.Context(), importPaths, "", files...)
	require.NoError(t, err, "the fixture itself must compile")
	return ormcompat.Parse(req)
}

func mustParse(t *testing.T, files ...string) *schema.Schema {
	t.Helper()
	s, err := parse(t, files...)
	require.NoError(t, err)
	return s
}

func TestParseApptest(t *testing.T) {
	s := mustParse(t, "apptest.proto")

	t.Run("only annotated messages are entities", func(t *testing.T) {
		var names []string
		for _, e := range s.Entities() {
			names = append(names, e.FullName())
		}
		require.Equal(t, []string{"apptest.Tenant", "apptest.User"}, names,
			"Profile carries no (orm.message), so it is a message and not an entity")
	})

	user, ok := s.Get("apptest.User")
	require.True(t, ok)

	t.Run("key is found and made unique and immutable", func(t *testing.T) {
		key := user.Key()
		require.NotNil(t, key)
		require.Equal(t, schema.ProtoName("id"), key.Name())
		require.Equal(t, schema.TypeUUID, key.Type())
		require.True(t, key.IsUnique(), "a key is unique whether or not it said so")
		require.True(t, key.IsImmutable())
		require.False(t, key.IsNullable())
	})

	t.Run("a disabled prop is not there at all", func(t *testing.T) {
		_, ok := user.Prop("state")
		require.False(t, ok)
	})

	t.Run("types are deduced when not declared", func(t *testing.T) {
		for _, tc := range []struct {
			prop schema.ProtoName
			want schema.Type
		}{
			{"id", schema.TypeUUID},      // declared; never deduced
			{"alias", schema.TypeString}, // from the kind
			{"labels", schema.TypeJSON},  // a map is a document
			{"profile", schema.TypeJSON}, // a message with no edge is stored
			{"date_created", schema.TypeTime},
			{"date_updated", schema.TypeTime},
		} {
			p, ok := user.Prop(tc.prop)
			require.True(t, ok, "%s is missing", tc.prop)
			require.Equal(t, tc.want, p.Type(), "%s", tc.prop)
		}
	})

	t.Run("value names come from the descriptor", func(t *testing.T) {
		p, ok := user.Prop("date_updated")
		require.True(t, ok)
		require.Equal(t, schema.ProtoName("date_updated"), p.Names().Proto)
		require.Equal(t, schema.ValueName("dateUpdated"), p.Names().Value,
			"the JSON name is protoc's answer, not one calque computes")
	})

	t.Run("presence", func(t *testing.T) {
		// Explicit presence on a scalar: really nullable.
		lock, ok := user.Prop("lock")
		require.True(t, ok)
		require.True(t, lock.IsNullable())

		// A timestamp is a message and has presence, but a time is not nullable
		// for that reason alone -- otherwise no schema could have a required
		// one.
		created, ok := user.Prop("date_created")
		require.True(t, ok)
		require.False(t, created.IsNullable(), "a time is not nullable merely by having presence")
		require.True(t, created.IsOptional(), "it has a default, so it may be left out")

		// A map is repeated: never nullable, always optional.
		labels, ok := user.Prop("labels")
		require.True(t, ok)
		require.True(t, labels.IsList())
		require.False(t, labels.IsNullable())
		require.True(t, labels.IsOptional())

		// An edge is required unless it says otherwise, even though the
		// underlying message field has presence.
		tenant, ok := user.Prop("tenant")
		require.True(t, ok)
		require.False(t, tenant.IsNullable(), "an edge is nullable only when it says so")
		require.False(t, tenant.IsOptional())
	})

	t.Run("edge points at the entity", func(t *testing.T) {
		p, ok := user.Prop("tenant")
		require.True(t, ok)
		e, ok := p.(*schema.Edge)
		require.True(t, ok, "tenant carries (orm.edge), so it is an edge and not a field")
		require.NotNil(t, e.Target())
		require.Equal(t, "apptest.Tenant", e.Target().FullName())
	})

	t.Run("version field", func(t *testing.T) {
		require.NotNil(t, user.Version())
		require.Equal(t, schema.ProtoName("date_updated"), user.Version().Name())
		require.Nil(t, user.Erased(), "this schema does not erase softly")
	})

	t.Run("index resolves across a field and an edge", func(t *testing.T) {
		idx, ok := user.Index("slug")
		require.True(t, ok)
		require.True(t, idx.IsUnique())
		require.True(t, idx.IsComposite())
		require.Len(t, idx.Props(), 2)
		require.Equal(t, schema.ProtoName("alias"), idx.Props()[0].Name())
		require.Equal(t, schema.ProtoName("tenant"), idx.Props()[1].Name())
	})

	t.Run("keys are the primary key and the unique index", func(t *testing.T) {
		var names []schema.ProtoName
		for _, k := range user.Keys() {
			names = append(names, k.Name())
		}
		require.Equal(t, []schema.ProtoName{"id", "slug"}, names)
	})

	t.Run("crud declares all four operations", func(t *testing.T) {
		require.Equal(t, []schema.Op{schema.OpAdd, schema.OpGet, schema.OpPatch, schema.OpErase},
			user.Rpc().Ops())
	})
}

// TestSourcesFollowFileToGenerate checks that an entity reachable only through
// an edge is modelled but not emitted.
func TestSourcesFollowFileToGenerate(t *testing.T) {
	s := mustParse(t, "apptest.proto")
	require.Len(t, s.Sources(), 2, "both entities are in the file asked for")

	for _, e := range s.Entities() {
		require.True(t, e.Generate())
	}
}
