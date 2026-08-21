package schema

import "google.golang.org/protobuf/reflect/protoreflect"

// EdgeSpec is what an Edge is built from.
type EdgeSpec struct {
	Names  Names
	Number int32

	// TargetName is the full name of the entity this edge points at. Build
	// resolves it to Target, so that an edge can name an entity that has not
	// been parsed yet -- which is how a self-reference and a cycle both work.
	TargetName string

	// FromName and FromNumber are the `from:` back-reference: the edge on the
	// target that this one is the other half of. Empty when there is none.
	FromName   string
	FromNumber int32

	List      bool
	Unique    bool
	Nullable  bool
	Immutable bool

	HasDefault bool
	Default    string

	Source protoreflect.FieldDescriptor
}

// Edge is a member of an entity that points at another entity.
//
// An edge is stored as a reference to its target's key. protoc-gen-orm-ts
// embedded the whole target message in the row and indexed the copy's key path,
// which means the copy goes stale with nothing to refresh it, and the same
// proto has different key semantics depending on the store. The Go side of this
// ecosystem already stores references -- protoc-gen-orm-ent emits
// edge.To("tenant", Tenant.Type) -- so references are also what makes the two
// targets agree.
type Edge struct {
	spec   EdgeSpec
	parent *Entity

	target  *Entity
	inverse *Edge
	reverse *Edge
}

var _ Prop = (*Edge)(nil)

// NewEdge builds an edge. Build resolves its target and back-references.
func NewEdge(spec EdgeSpec) *Edge { return &Edge{spec: spec} }

func (e *Edge) Kind() ElemKind    { return KindEdge }
func (e *Edge) Entity() *Entity   { return e.parent }
func (e *Edge) Name() ProtoName   { return e.spec.Names.Proto }
func (e *Edge) Names() Names      { return e.spec.Names }
func (e *Edge) Number() int32     { return e.spec.Number }
func (e *Edge) IsUnique() bool    { return e.spec.Unique }
func (e *Edge) IsImmutable() bool { return e.spec.Immutable }
func (e *Edge) IsList() bool      { return e.spec.List }
func (e *Edge) HasDefault() bool  { return e.spec.HasDefault }
func (e *Edge) Default() string   { return e.spec.Default }

// Type is always TypeMessage. An edge's storage type is its target's key type,
// which is Target().Key().Type() and deliberately not this.
func (e *Edge) Type() Type { return TypeMessage }

// Target is the entity this edge points at. Never nil after Build.
func (e *Edge) Target() *Entity { return e.target }

// Inverse is the edge this one declared itself the back-reference of, via
// `from:`, or nil.
func (e *Edge) Inverse() *Edge { return e.inverse }

// Reverse is the edge on the target that names this one via `from:`, or nil.
func (e *Edge) Reverse() *Edge { return e.reverse }

func (e *Edge) Source() protoreflect.FieldDescriptor { return e.spec.Source }

// IsNullable reports whether the edge can be absent.
//
// An edge is nullable only when it says so, or when the `optional` keyword was
// used. Message presence alone does not make it nullable, which is where edges
// and fields deliberately disagree: every message field has presence, so
// treating presence as nullability would make every edge optional and no
// relation ever required.
func (e *Edge) IsNullable() bool {
	if e.spec.List {
		return false
	}
	return e.spec.Nullable
}

// IsOptional reports whether the edge may be left out when a row is created.
func (e *Edge) IsOptional() bool {
	if e.spec.List {
		return true
	}
	return e.IsNullable() || e.spec.HasDefault
}

func (e *Edge) elem() {}
func (e *Edge) prop() {}
