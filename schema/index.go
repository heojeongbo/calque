package schema

// Ref names a prop by both its name and its number.
//
// Either alone would find the prop. Requiring both is what makes a rename loud:
// an index that says {alias, 4} stops resolving the moment field 4 is called
// something else, rather than silently indexing whatever took the name.
type Ref struct {
	Name   ProtoName
	Number int32
}

// IndexSpec is what an Index is built from.
type IndexSpec struct {
	Name ProtoName

	// Refs name the members, in the order the index orders them. Build resolves
	// them to Props.
	Refs []Ref

	Unique    bool
	Immutable bool

	// Hidden marks an index that is written to the store but is not a lookup
	// key: it is excluded from the generated request messages.
	//
	// protoc-gen-orm-ts never read this, so a hidden index would have produced
	// a get() case that nothing could call.
	Hidden bool

	// IncludesErased says a unique index covers erased rows as well as live
	// ones. Off is the default and what almost every unique index wants: a row
	// that is gone should not go on holding the name it had.
	IncludesErased bool
}

// Index groups one or more props.
type Index struct {
	spec   IndexSpec
	parent *Entity
	props  []Prop
}

var _ Elem = (*Index)(nil)

// NewIndex builds an index. Build resolves its refs to props.
func NewIndex(spec IndexSpec) *Index { return &Index{spec: spec} }

func (i *Index) Kind() ElemKind    { return KindIndex }
func (i *Index) Entity() *Entity   { return i.parent }
func (i *Index) Name() ProtoName   { return i.spec.Name }
func (i *Index) IsUnique() bool    { return i.spec.Unique }
func (i *Index) IsImmutable() bool { return i.spec.Immutable }
func (i *Index) IsHidden() bool    { return i.spec.Hidden }

// Number reports the first member's field number.
//
// An index has no number of its own. This exists so that Elem has one way to be
// ordered stably, and it means nothing else.
func (i *Index) Number() int32 {
	if len(i.props) == 0 {
		return 0
	}
	return i.props[0].Number()
}

// Props are the index members, in declared order. Never empty after Build.
func (i *Index) Props() []Prop { return i.props }

// Refs are the members as they were written.
func (i *Index) Refs() []Ref { return i.spec.Refs }

// IsComposite reports whether the index spans more than one prop, which is the
// question a backend asks when deciding whether it can enforce uniqueness.
func (i *Index) IsComposite() bool { return len(i.props) > 1 }

// ExcludesErased reports whether this index covers only the rows still there,
// which a store writes as a partial index.
//
// It is true for a unique index on an entity that erases softly, unless the
// index said includes_erased. On an entity with no erased field it is always
// false: there is nothing to exclude.
func (i *Index) ExcludesErased() bool {
	if !i.spec.Unique || i.spec.IncludesErased {
		return false
	}
	return i.parent != nil && i.parent.Erased() != nil
}

func (i *Index) elem() {}
