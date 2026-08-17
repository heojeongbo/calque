package schema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/schema"
)

// tenant and user are the shape everything downstream is exercised against: a
// uuid key, a unique field, an edge, and a unique index that spans a field and
// an edge. It is deliberately the shape of the production schema this was
// measured against -- hday.oasys.Robot has exactly this "slug" index -- because
// that index is the one a document store cannot enforce and a relational one
// can.
func fixture(t *testing.T) *schema.Schema {
	t.Helper()

	tenantKey := schema.NewField(schema.FieldSpec{
		Names:      schema.Names{Proto: "id", Value: "id"},
		Number:     1,
		Type:       schema.TypeUUID,
		Unique:     true,
		Immutable:  true,
		HasDefault: true,
	})
	tenant := schema.NewEntity(schema.EntitySpec{
		FullName: "apptest.Tenant",
		Name:     "Tenant",
		Package:  "apptest",
		File:     "apptest/tenant.proto",
		Generate: true,
		Fields:   []*schema.Field{tenantKey},
		Rpc:      schema.RpcSet{schema.OpGet: true},
	})
	tenant.SetKey(tenantKey)

	userKey := schema.NewField(schema.FieldSpec{
		Names:      schema.Names{Proto: "id", Value: "id"},
		Number:     1,
		Type:       schema.TypeUUID,
		Unique:     true,
		Immutable:  true,
		HasDefault: true,
	})
	alias := schema.NewField(schema.FieldSpec{
		Names:      schema.Names{Proto: "alias", Value: "alias"},
		Number:     4,
		Type:       schema.TypeString,
		HasDefault: true,
	})
	dateUpdated := schema.NewField(schema.FieldSpec{
		Names:   schema.Names{Proto: "date_updated", Value: "dateUpdated"},
		Number:  14,
		Type:    schema.TypeTime,
		Version: true,
	})
	tenantEdge := schema.NewEdge(schema.EdgeSpec{
		Names:      schema.Names{Proto: "tenant", Value: "tenant"},
		Number:     2,
		TargetName: "apptest.Tenant",
	})
	slug := schema.NewIndex(schema.IndexSpec{
		Name: "slug",
		Refs: []schema.Ref{
			{Name: "alias", Number: 4},
			{Name: "tenant", Number: 2},
		},
		Unique: true,
	})

	user := schema.NewEntity(schema.EntitySpec{
		FullName: "apptest.User",
		Name:     "User",
		Package:  "apptest",
		File:     "apptest/user.proto",
		Generate: true,
		Fields:   []*schema.Field{userKey, alias, dateUpdated},
		Edges:    []*schema.Edge{tenantEdge},
		Indexes:  []*schema.Index{slug},
		Rpc:      schema.RpcSet{schema.OpAdd: true, schema.OpGet: true, schema.OpPatch: true, schema.OpErase: true},
	})
	user.SetKey(userKey)

	s, err := schema.Build([]*schema.Entity{tenant, user})
	require.NoError(t, err)
	return s
}

func buildOneEntity(t *testing.T) *schema.Entity {
	t.Helper()
	s := fixture(t)
	e, ok := s.Get("apptest.User")
	require.True(t, ok)
	return e
}

func TestBuildResolvesEverything(t *testing.T) {
	s := fixture(t)

	user, ok := s.Get("apptest.User")
	require.True(t, ok)
	tenant, ok := s.Get("apptest.Tenant")
	require.True(t, ok)

	t.Run("members know their entity", func(t *testing.T) {
		for _, el := range user.Elems() {
			require.Same(t, user, el.Entity(), "%s does not point back at its entity", el.Name())
		}
	})

	t.Run("props are in field-number order", func(t *testing.T) {
		var numbers []int32
		for _, p := range user.Props() {
			numbers = append(numbers, p.Number())
		}
		require.Equal(t, []int32{1, 2, 4, 14}, numbers,
			"props must be ordered by number regardless of which slice they came from")
	})

	t.Run("edge target is resolved", func(t *testing.T) {
		e, ok := user.Prop("tenant")
		require.True(t, ok)
		require.Same(t, tenant, e.(*schema.Edge).Target())
	})

	t.Run("index refs are resolved in declared order", func(t *testing.T) {
		idx, ok := user.Index("slug")
		require.True(t, ok)
		require.Len(t, idx.Props(), 2)
		require.Equal(t, schema.ProtoName("alias"), idx.Props()[0].Name())
		require.Equal(t, schema.ProtoName("tenant"), idx.Props()[1].Name())
		require.True(t, idx.IsComposite())
	})

	t.Run("version field is found", func(t *testing.T) {
		require.NotNil(t, user.Version())
		require.Equal(t, schema.ProtoName("date_updated"), user.Version().Name())
	})

	t.Run("no erased field means no soft delete", func(t *testing.T) {
		require.Nil(t, user.Erased())
		require.False(t, user.ErasesSoftly())
		idx, _ := user.Index("slug")
		require.False(t, idx.ExcludesErased(),
			"an entity with nothing to erase has nothing to exclude")
	})

	t.Run("keys are the unique elems", func(t *testing.T) {
		var names []schema.ProtoName
		for _, k := range user.Keys() {
			names = append(names, k.Name())
		}
		require.Equal(t, []schema.ProtoName{"id", "slug"}, names)
	})
}

// TestRefMustMatchNameAndNumber is the rename-safety belt. Getting the name
// right and the number wrong has to fail, or an index silently follows whatever
// took the name.
func TestRefMustMatchNameAndNumber(t *testing.T) {
	key := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "id", Value: "id"}, Number: 1,
		Type: schema.TypeUUID, Unique: true, Immutable: true,
	})
	alias := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "alias", Value: "alias"}, Number: 4,
		Type: schema.TypeString,
	})
	idx := schema.NewIndex(schema.IndexSpec{
		Name:   "slug",
		Refs:   []schema.Ref{{Name: "alias", Number: 7}}, // renumbered
		Unique: true,
	})
	e := schema.NewEntity(schema.EntitySpec{
		FullName: "t.T", Name: "T", Package: "t", File: "t.proto", Generate: true,
		Fields: []*schema.Field{key, alias}, Indexes: []*schema.Index{idx},
	})
	e.SetKey(key)

	_, err := schema.Build([]*schema.Entity{e})
	require.Error(t, err)
	require.Contains(t, err.Error(), "alias is field 4, not 7")
	require.Contains(t, err.Error(), "t.T.{indexes}(slug).refs[0]")
}

func TestUnknownRefIsNamed(t *testing.T) {
	key := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "id", Value: "id"}, Number: 1,
		Type: schema.TypeUUID, Unique: true, Immutable: true,
	})
	idx := schema.NewIndex(schema.IndexSpec{
		Name: "slug", Refs: []schema.Ref{{Name: "nope", Number: 9}},
	})
	e := schema.NewEntity(schema.EntitySpec{
		FullName: "t.T", Name: "T", Package: "t", File: "t.proto", Generate: true,
		Fields: []*schema.Field{key}, Indexes: []*schema.Index{idx},
	})
	e.SetKey(key)

	_, err := schema.Build([]*schema.Entity{e})
	require.ErrorContains(t, err, "no prop named nope")
}

func TestEdgeToNonEntityIsRefused(t *testing.T) {
	key := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "id", Value: "id"}, Number: 1,
		Type: schema.TypeUUID, Unique: true, Immutable: true,
	})
	edge := schema.NewEdge(schema.EdgeSpec{
		Names: schema.Names{Proto: "other", Value: "other"}, Number: 2,
		TargetName: "t.Missing",
	})
	e := schema.NewEntity(schema.EntitySpec{
		FullName: "t.T", Name: "T", Package: "t", File: "t.proto", Generate: true,
		Fields: []*schema.Field{key}, Edges: []*schema.Edge{edge},
	})
	e.SetKey(key)

	_, err := schema.Build([]*schema.Entity{e})
	require.ErrorContains(t, err, "t.Missing is not an entity")
	require.ErrorContains(t, err, "option (orm.message)")
}

// TestDiagnosticsAreCollected is the property that makes a bad schema one run
// to fix rather than one run per mistake.
func TestDiagnosticsAreCollected(t *testing.T) {
	key := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "id", Value: "id"}, Number: 1,
		Type: schema.TypeUUID, Unique: true, Immutable: true,
	})
	e := schema.NewEntity(schema.EntitySpec{
		FullName: "t.T", Name: "T", Package: "t", File: "t.proto", Generate: true,
		Fields: []*schema.Field{key},
		Edges: []*schema.Edge{
			schema.NewEdge(schema.EdgeSpec{Names: schema.Names{Proto: "a"}, Number: 2, TargetName: "t.NoA"}),
			schema.NewEdge(schema.EdgeSpec{Names: schema.Names{Proto: "b"}, Number: 3, TargetName: "t.NoB"}),
		},
		Indexes: []*schema.Index{
			schema.NewIndex(schema.IndexSpec{Name: "empty"}),
		},
	})
	e.SetKey(key)

	_, err := schema.Build([]*schema.Entity{e})
	require.Error(t, err)

	var diags *schema.Diagnostics
	require.ErrorAs(t, err, &diags)
	require.Equal(t, 3, diags.Len(), "every problem should be reported, not just the first")
	require.ErrorContains(t, err, "t.NoA")
	require.ErrorContains(t, err, "t.NoB")
	require.ErrorContains(t, err, "at least one prop")
}

func TestSourcesExcludeEntitiesOnlyReachedByAnEdge(t *testing.T) {
	s := fixture(t)
	require.Len(t, s.Sources(), 2)

	// Rebuild with Tenant not marked for generation: it stays in the schema,
	// because the edge means nothing without it, and leaves Sources.
	tenantKey := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "id", Value: "id"}, Number: 1,
		Type: schema.TypeUUID, Unique: true, Immutable: true,
	})
	tenant := schema.NewEntity(schema.EntitySpec{
		FullName: "apptest.Tenant", Name: "Tenant", Package: "apptest",
		File: "apptest/tenant.proto", Generate: false,
		Fields: []*schema.Field{tenantKey},
	})
	tenant.SetKey(tenantKey)

	userKey := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "id", Value: "id"}, Number: 1,
		Type: schema.TypeUUID, Unique: true, Immutable: true,
	})
	user := schema.NewEntity(schema.EntitySpec{
		FullName: "apptest.User", Name: "User", Package: "apptest",
		File: "apptest/user.proto", Generate: true,
		Fields: []*schema.Field{userKey},
		Edges: []*schema.Edge{schema.NewEdge(schema.EdgeSpec{
			Names: schema.Names{Proto: "tenant"}, Number: 2, TargetName: "apptest.Tenant",
		})},
	})
	user.SetKey(userKey)

	built, err := schema.Build([]*schema.Entity{tenant, user})
	require.NoError(t, err)
	require.Len(t, built.Entities(), 2)
	require.Len(t, built.Sources(), 1)
	require.Equal(t, "apptest.User", built.Sources()[0].FullName())
}
