package schema

import "fmt"

// ElemKind discriminates the Elem variants.
type ElemKind int

const (
	KindField ElemKind = iota + 1
	KindEdge
	KindIndex
)

func (k ElemKind) String() string {
	switch k {
	case KindField:
		return "field"
	case KindEdge:
		return "edge"
	case KindIndex:
		return "index"
	default:
		return fmt.Sprintf("ElemKind(%d)", int(k))
	}
}

// AllElemKinds is every variant. Tests walk it to check that everything which
// must handle all three actually does.
func AllElemKinds() []ElemKind { return []ElemKind{KindField, KindEdge, KindIndex} }

// Elem is a field, an edge, or an index: the three things that can be unique,
// and therefore the three things a lookup can be keyed by.
//
// The interface is sealed by elem(), so a variant can only be added in this
// package. That is what makes AllElemKinds trustworthy and ElemVisitor's method
// set the complete one.
type Elem interface {
	Kind() ElemKind

	// Entity is the entity this belongs to.
	Entity() *Entity

	// Name is a prop's proto field name, or an index's declared name.
	Name() ProtoName

	// Number is a prop's field number. An index reports its first member's,
	// which is only meaningful as a stable sort key.
	Number() int32

	IsUnique() bool
	IsImmutable() bool

	elem()
}

// ElemVisitor is how something that must handle every variant says so.
//
// Adding a variant adds a method here, and every implementation stops
// compiling. That is the difference between this and a type switch whose
// default panics: protoc-gen-orm-ts had two such switches, both
// `default: panic("unimplemented: key type not Field")`, and both were wrong in
// the same way -- a unique edge used as a lookup key built a schema string and
// then panicked on the way to generating get(). One was fixed. The other, at
// apps/db/app/app.go:330, still is not.
//
// A struct of func fields would not do this. A keyed struct literal still
// compiles when a field is added, leaving the new case nil at run time, which
// is the same failure wearing a different hat.
type ElemVisitor[R any] interface {
	VisitField(*Field) (R, error)
	VisitEdge(*Edge) (R, error)
	VisitIndex(*Index) (R, error)
}

// VisitElem dispatches e to v.
//
// The default arm is unreachable for any Elem, because Elem is sealed. It
// returns an error rather than panicking anyway, since the only way to reach it
// is for this function and ElemVisitor to have been changed apart from each
// other, and a generator crashing is worse than a generator complaining.
func VisitElem[R any](e Elem, v ElemVisitor[R]) (R, error) {
	switch t := e.(type) {
	case *Field:
		return v.VisitField(t)
	case *Edge:
		return v.VisitEdge(t)
	case *Index:
		return v.VisitIndex(t)
	default:
		var zero R
		return zero, fmt.Errorf("schema: %T is not an Elem variant; VisitElem and ElemVisitor are out of sync", e)
	}
}

// Prop is a member of an entity that holds a value: a Field or an Edge.
//
// An Index is an Elem but not a Prop, which is the distinction that matters
// when something is about to read or write a value as opposed to look one up.
type Prop interface {
	Elem

	Names() Names
	Type() Type

	IsList() bool

	// IsNullable reports whether the prop can hold null, which fields and edges
	// answer differently on purpose. See field.go and edge.go.
	IsNullable() bool

	// IsOptional reports whether the prop may be left out when a row is
	// created: it is nullable, or it has a default.
	IsOptional() bool

	HasDefault() bool
	Default() string

	prop()
}

// PropVisitor is ElemVisitor without the index arm, for the places that are
// only ever handed a value-bearing member.
type PropVisitor[R any] interface {
	VisitField(*Field) (R, error)
	VisitEdge(*Edge) (R, error)
}

// VisitProp dispatches p to v.
func VisitProp[R any](p Prop, v PropVisitor[R]) (R, error) {
	switch t := p.(type) {
	case *Field:
		return v.VisitField(t)
	case *Edge:
		return v.VisitEdge(t)
	default:
		var zero R
		return zero, fmt.Errorf("schema: %T is not a Prop variant; VisitProp and PropVisitor are out of sync", p)
	}
}
