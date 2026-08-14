package schema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/HeoJeongBo/calque/schema"
)

// TestFieldPresence pins the rules for a field. They are read by every target
// that decides whether a value may be left out, so getting one wrong is wrong
// everywhere at once.
func TestFieldPresence(t *testing.T) {
	for _, tc := range []struct {
		name               string
		spec               schema.FieldSpec
		nullable, optional bool
	}{
		{
			name:     "plain",
			spec:     schema.FieldSpec{Type: schema.TypeString},
			nullable: false, optional: false,
		},
		{
			name:     "nullable",
			spec:     schema.FieldSpec{Type: schema.TypeString, Nullable: true},
			nullable: true, optional: true,
		},
		{
			name:     "has a default",
			spec:     schema.FieldSpec{Type: schema.TypeString, HasDefault: true},
			nullable: false, optional: true,
		},
		{
			// proto cannot tell an empty list from an absent one, so calling a
			// repeated field nullable would invent a distinction that no
			// encoding carries.
			name:     "repeated is never nullable and always optional",
			spec:     schema.FieldSpec{Type: schema.TypeString, List: true},
			nullable: false, optional: true,
		},
		{
			name:     "repeated ignores nullable even when it is set",
			spec:     schema.FieldSpec{Type: schema.TypeString, List: true, Nullable: true},
			nullable: false, optional: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := schema.NewField(tc.spec)
			require.Equal(t, tc.nullable, f.IsNullable(), "IsNullable")
			require.Equal(t, tc.optional, f.IsOptional(), "IsOptional")
		})
	}
}

// TestEdgePresence pins where edges differ from fields.
//
// Every message field in proto has presence, so if an edge treated presence as
// nullability then every relation would be optional and none could ever be
// required. An edge is nullable only when it says so.
func TestEdgePresence(t *testing.T) {
	for _, tc := range []struct {
		name               string
		spec               schema.EdgeSpec
		nullable, optional bool
	}{
		{
			name:     "plain edge is required",
			spec:     schema.EdgeSpec{TargetName: "x.Y"},
			nullable: false, optional: false,
		},
		{
			name:     "nullable edge",
			spec:     schema.EdgeSpec{TargetName: "x.Y", Nullable: true},
			nullable: true, optional: true,
		},
		{
			name:     "repeated edge is never nullable and always optional",
			spec:     schema.EdgeSpec{TargetName: "x.Y", List: true},
			nullable: false, optional: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := schema.NewEdge(tc.spec)
			require.Equal(t, tc.nullable, e.IsNullable(), "IsNullable")
			require.Equal(t, tc.optional, e.IsOptional(), "IsOptional")
		})
	}
}

// TestEdgeTypeIsNotItsStorageType guards a mistake that would be easy to make:
// an edge is stored as its target's key, so a backend choosing a column type
// from Edge.Type() would get "message" and be wrong for every relation.
func TestEdgeTypeIsMessage(t *testing.T) {
	e := schema.NewEdge(schema.EdgeSpec{TargetName: "x.Y"})
	require.Equal(t, schema.TypeMessage, e.Type())
	require.False(t, e.Type().IsOrderable(),
		"an edge is not orderable as itself; it is indexed by its target's key")
}

// TestBackReferencesLinkBothWays checks that declaring one half of a relation
// records both, so neither side has to go looking.
func TestBackReferencesLinkBothWays(t *testing.T) {
	key := func() *schema.Field {
		return schema.NewField(schema.FieldSpec{
			Names: schema.Names{Proto: "id", Value: "id"}, Number: 1,
			Type: schema.TypeUUID, Unique: true, Immutable: true,
		})
	}

	parent := schema.NewEdge(schema.EdgeSpec{
		Names: schema.Names{Proto: "parent", Value: "parent"}, Number: 10,
		TargetName: "t.Node", Nullable: true,
	})
	children := schema.NewEdge(schema.EdgeSpec{
		Names: schema.Names{Proto: "children", Value: "children"}, Number: 11,
		TargetName: "t.Node", List: true,
		FromName: "parent", FromNumber: 10,
	})

	k := key()
	node := schema.NewEntity(schema.EntitySpec{
		FullName: "t.Node", Name: "Node", Package: "t", File: "t.proto", Generate: true,
		Fields: []*schema.Field{k},
		Edges:  []*schema.Edge{parent, children},
	})
	node.SetKey(k)

	_, err := schema.Build([]*schema.Entity{node})
	require.NoError(t, err, "a self-referencing entity must resolve")

	require.Same(t, parent, children.Inverse(), "children declared `from: parent`")
	require.Same(t, children, parent.Reverse(), "so parent's reverse is children")
	require.Nil(t, parent.Inverse(), "parent declared no `from:` of its own")
}

func TestBackReferenceMismatchIsRefused(t *testing.T) {
	k := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "id", Value: "id"}, Number: 1,
		Type: schema.TypeUUID, Unique: true, Immutable: true,
	})
	alias := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "alias", Value: "alias"}, Number: 5, Type: schema.TypeString,
	})
	// `from:` naming a field rather than an edge.
	bad := schema.NewEdge(schema.EdgeSpec{
		Names: schema.Names{Proto: "children", Value: "children"}, Number: 11,
		TargetName: "t.Node", List: true,
		FromName: "alias", FromNumber: 5,
	})
	node := schema.NewEntity(schema.EntitySpec{
		FullName: "t.Node", Name: "Node", Package: "t", File: "t.proto", Generate: true,
		Fields: []*schema.Field{k, alias}, Edges: []*schema.Edge{bad},
	})
	node.SetKey(k)

	_, err := schema.Build([]*schema.Entity{node})
	require.ErrorContains(t, err, "a field rather than an edge")
}

// TestHiddenIndexIsNotAKey pins the distinction protoc-gen-orm-ts never read: a
// hidden index is a constraint the store holds, not a way to ask for a row, so
// generating a lookup for it would produce a call nobody can make.
func TestHiddenIndexIsNotAKey(t *testing.T) {
	k := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "id", Value: "id"}, Number: 1,
		Type: schema.TypeUUID, Unique: true, Immutable: true,
	})
	alias := schema.NewField(schema.FieldSpec{
		Names: schema.Names{Proto: "alias", Value: "alias"}, Number: 4, Type: schema.TypeString,
	})
	hidden := schema.NewIndex(schema.IndexSpec{
		Name: "secret", Refs: []schema.Ref{{Name: "alias", Number: 4}},
		Unique: true, Hidden: true,
	})
	e := schema.NewEntity(schema.EntitySpec{
		FullName: "t.T", Name: "T", Package: "t", File: "t.proto", Generate: true,
		Fields: []*schema.Field{k, alias}, Indexes: []*schema.Index{hidden},
	})
	e.SetKey(k)

	_, err := schema.Build([]*schema.Entity{e})
	require.NoError(t, err)

	var names []schema.ProtoName
	for _, key := range e.Keys() {
		names = append(names, key.Name())
	}
	require.Equal(t, []schema.ProtoName{"id"}, names,
		"a hidden index is unique but is not a lookup")
	require.True(t, hidden.IsUnique(), "it is still a constraint the store must hold")
}
