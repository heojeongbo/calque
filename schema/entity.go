package schema

import "google.golang.org/protobuf/reflect/protoreflect"

// EntitySpec is what an Entity is built from.
type EntitySpec struct {
	// FullName is "apptest.User" and is the entity's identity.
	FullName string
	// Name is "User", Package is "apptest", File is "apptest/user.proto".
	Name    string
	Package string
	File    string

	// Generate is true when this entity's file was in files-to-generate. An
	// entity reachable only as an edge target is in the schema but not here,
	// because it has to be understood and does not have to be emitted.
	Generate bool

	Fields  []*Field
	Edges   []*Edge
	Indexes []*Index

	Rpc RpcSet

	Source protoreflect.MessageDescriptor
}

// Entity is a message that said it was one.
type Entity struct {
	spec EntitySpec

	key     *Field
	version *Field
	erased  *Field

	props []Prop
	elems []Elem
}

// NewEntity builds an entity. Build resolves its key, indexes and edges.
func NewEntity(spec EntitySpec) *Entity { return &Entity{spec: spec} }

func (e *Entity) FullName() string { return e.spec.FullName }
func (e *Entity) Name() string     { return e.spec.Name }
func (e *Entity) Package() string  { return e.spec.Package }
func (e *Entity) File() string     { return e.spec.File }
func (e *Entity) Generate() bool   { return e.spec.Generate }
func (e *Entity) Rpc() RpcSet      { return e.spec.Rpc }

func (e *Entity) Source() protoreflect.MessageDescriptor { return e.spec.Source }

// Key is the single primary-key field. Never nil in a schema that Build
// accepted.
//
// calque follows upstream in allowing exactly one: every Ref and GetRequest
// shape the ecosystem generates assumes a single key field, and the fourteen
// entities in the production schema this was measured against are all keyed by
// one uuid. See the README under "why there is one key".
func (e *Entity) Key() *Field { return e.key }

// Version is the optimistic-locking field, or nil.
func (e *Entity) Version() *Field { return e.version }

// Erased is the soft-delete stamp, or nil. When it is set, every read has to
// exclude the rows that carry one.
func (e *Entity) Erased() *Field { return e.erased }

// ErasesSoftly reports whether erasing this entity stamps a row rather than
// removing it.
func (e *Entity) ErasesSoftly() bool { return e.erased != nil }

func (e *Entity) Fields() []*Field  { return e.spec.Fields }
func (e *Entity) Edges() []*Edge    { return e.spec.Edges }
func (e *Entity) Indexes() []*Index { return e.spec.Indexes }

// Props is every value-bearing member, in declaration order.
func (e *Entity) Props() []Prop { return e.props }

// Elems is every prop followed by every index, which is the order candidate
// keys are considered in.
func (e *Entity) Elems() []Elem { return e.elems }

// Keys is the Elems that are unique: the lookups this entity supports.
//
// A hidden index is unique and is not a key -- it is a constraint the store
// holds and not a way to ask for a row -- so it is left out here.
func (e *Entity) Keys() []Elem {
	var out []Elem
	for _, el := range e.elems {
		if !el.IsUnique() {
			continue
		}
		if idx, ok := el.(*Index); ok && idx.IsHidden() {
			continue
		}
		out = append(out, el)
	}
	return out
}

// Prop finds a member by proto name.
func (e *Entity) Prop(name ProtoName) (Prop, bool) {
	for _, p := range e.props {
		if p.Name() == name {
			return p, true
		}
	}
	return nil, false
}

// Index finds an index by its declared name.
func (e *Entity) Index(name ProtoName) (*Index, bool) {
	for _, i := range e.spec.Indexes {
		if i.Name() == name {
			return i, true
		}
	}
	return nil, false
}
