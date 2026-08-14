package schema

import "google.golang.org/protobuf/reflect/protoreflect"

// FieldSpec is what a Field is built from.
//
// Props are built through a spec rather than assembled field by field so that
// the accessors below are the only way to ask a question about one. A caller
// that could reach in would eventually read Nullable directly and get the wrong
// answer for a repeated prop, which is exactly the kind of rule that has to
// live in one place.
type FieldSpec struct {
	Names  Names
	Number int32
	Type   Type

	List      bool
	Unique    bool
	Nullable  bool
	Immutable bool

	HasDefault bool
	Default    string

	// Version marks the optimistic-locking field: the server stamps it on every
	// write, and a write that names an older one is refused.
	Version bool

	// Erased marks the field that says whether a row is still there. It is a
	// time, null for as long as the row is present, and stamping it is what
	// erasing does.
	Erased bool

	Source protoreflect.FieldDescriptor
}

// Field is a member of an entity backed by scalar data.
type Field struct {
	spec   FieldSpec
	parent *Entity
}

var _ Prop = (*Field)(nil)

// NewField builds a field. Build attaches it to its entity.
func NewField(spec FieldSpec) *Field { return &Field{spec: spec} }

func (f *Field) Kind() ElemKind    { return KindField }
func (f *Field) Entity() *Entity   { return f.parent }
func (f *Field) Name() ProtoName   { return f.spec.Names.Proto }
func (f *Field) Names() Names      { return f.spec.Names }
func (f *Field) Number() int32     { return f.spec.Number }
func (f *Field) Type() Type        { return f.spec.Type }
func (f *Field) IsUnique() bool    { return f.spec.Unique }
func (f *Field) IsImmutable() bool { return f.spec.Immutable }
func (f *Field) IsList() bool      { return f.spec.List }
func (f *Field) HasDefault() bool  { return f.spec.HasDefault }
func (f *Field) Default() string   { return f.spec.Default }

// Source is the descriptor this field came from. It is for diagnostics and for
// a target that needs a descriptor fact the IR does not carry.
//
// It must not be used for naming. That is what Names is for, and mixing the two
// is the whole of the device_id bug.
func (f *Field) Source() protoreflect.FieldDescriptor { return f.spec.Source }

// IsNullable reports whether the field can hold null.
//
// A repeated field never can: proto has no way to tell an empty list from an
// absent one, so treating the distinction as real would invent information.
func (f *Field) IsNullable() bool {
	if f.spec.List {
		return false
	}
	return f.spec.Nullable
}

// IsOptional reports whether the field may be left out when a row is created.
//
// A repeated field always may -- no input means the empty list, which is a
// perfectly good value -- and otherwise it is optional if it is nullable or has
// a default to fall back on.
func (f *Field) IsOptional() bool {
	if f.spec.List {
		return true
	}
	return f.IsNullable() || f.spec.HasDefault
}

// IsVersion reports whether this is the entity's optimistic-locking field.
func (f *Field) IsVersion() bool { return f.spec.Version }

// IsErased reports whether this is the entity's soft-delete stamp.
func (f *Field) IsErased() bool { return f.spec.Erased }

func (f *Field) elem() {}
func (f *Field) prop() {}
