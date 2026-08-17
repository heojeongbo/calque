package schema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/schema"
)

// kindVisitor answers with the kind it was handed, which is enough to tell
// whether dispatch went to the right arm.
type kindVisitor struct{}

func (kindVisitor) VisitField(f *schema.Field) (schema.ElemKind, error) {
	return f.Kind(), nil
}
func (kindVisitor) VisitEdge(e *schema.Edge) (schema.ElemKind, error) {
	return e.Kind(), nil
}
func (kindVisitor) VisitIndex(i *schema.Index) (schema.ElemKind, error) {
	return i.Kind(), nil
}

// TestVisitElemDispatchesEveryKind walks AllElemKinds rather than naming the
// three variants, so that adding a fourth without teaching this test about it
// fails here as well as at every ElemVisitor implementation.
func TestVisitElemDispatchesEveryKind(t *testing.T) {
	e := buildOneEntity(t)

	byKind := map[schema.ElemKind]schema.Elem{}
	for _, el := range e.Elems() {
		byKind[el.Kind()] = el
	}

	for _, kind := range schema.AllElemKinds() {
		el, ok := byKind[kind]
		require.True(t, ok, "the fixture has no %s, so dispatch to it is untested", kind)

		got, err := schema.VisitElem[schema.ElemKind](el, kindVisitor{})
		require.NoError(t, err)
		require.Equal(t, kind, got, "VisitElem sent a %s to the wrong arm", kind)
	}
}

// TestElemKindsAreExhaustive is the guard that makes AllElemKinds trustworthy:
// it must list every kind that a built entity can actually produce.
func TestElemKindsAreExhaustive(t *testing.T) {
	e := buildOneEntity(t)

	known := map[schema.ElemKind]bool{}
	for _, k := range schema.AllElemKinds() {
		known[k] = true
	}
	for _, el := range e.Elems() {
		require.True(t, known[el.Kind()],
			"%T reports kind %s, which AllElemKinds does not list", el, el.Kind())
		require.NotEqual(t, "ElemKind(0)", el.Kind().String(),
			"%T has no kind", el)
	}
}

// TestKindsStringify keeps a kind from printing as a bare number in a
// diagnostic, which is where these names are actually read.
func TestKindsStringify(t *testing.T) {
	require.Equal(t, "field", schema.KindField.String())
	require.Equal(t, "edge", schema.KindEdge.String())
	require.Equal(t, "index", schema.KindIndex.String())
	require.Equal(t, "ElemKind(0)", schema.ElemKind(0).String())
}

// TestAllTypesStringify does the same for types, and doubles as the list a
// backend's codec test walks.
func TestAllTypesStringify(t *testing.T) {
	seen := map[string]bool{}
	for _, ty := range schema.AllTypes() {
		s := ty.String()
		require.NotContains(t, s, "Type(", "%v has no name", int(ty))
		require.False(t, seen[s], "two types both print as %q", s)
		seen[s] = true
	}
}

// TestOrderableExcludesDocuments pins the rule an index member depends on: a
// store has no agreed order for two documents, so neither json nor a message
// can be indexed by range.
func TestOrderableExcludesDocuments(t *testing.T) {
	require.False(t, schema.TypeJSON.IsOrderable())
	require.False(t, schema.TypeMessage.IsOrderable())
	require.False(t, schema.TypeUnspecified.IsOrderable())

	require.True(t, schema.TypeString.IsOrderable())
	require.True(t, schema.TypeTime.IsOrderable())
	require.True(t, schema.TypeUUID.IsOrderable())
	require.True(t, schema.TypeInt64.IsOrderable())
}
