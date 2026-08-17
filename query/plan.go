// Package query is what a generated operation says it wants, in terms no
// storage engine owns.
//
// It replaces the shape protoc-gen-orm-ts had, where a generated method handed
// the runtime a closure over a Dexie table:
//
//	return this._query(t => t.where(q).first());
//
// A closure cannot be inspected. Nothing can ask it which index it uses, cache
// its result under a key, subscribe to it, or run it against a second store —
// and the generated file's *type* then depends on one store's query builder,
// so removing it later breaks every generated file and the runtime's public
// types at once. A plan is a value: a target emits it, an adapter interprets
// it, and a test can read it.
//
// The parity bar is small — the predecessor generated one lookup — so cutting
// this seam now is nearly free and later is not.
package query

import "github.com/heojeongbo/calque/schema"

// Op is one storage operation.
type Op string

const (
	// OpGet reads exactly one row and fails when there is none.
	OpGet Op = "get"
	// OpList reads many rows.
	OpList Op = "list"
	// OpInsert adds a row that must not already exist.
	OpInsert Op = "insert"
	// OpUpdate writes to a row that must already exist.
	OpUpdate Op = "update"
	// OpErase stamps a row's erased field, leaving it in place.
	OpErase Op = "erase"
	// OpDelete removes a row.
	OpDelete Op = "delete"
)

// LookupKind is how rows are found.
type LookupKind string

const (
	// LookupPrimary finds by the entity's key.
	LookupPrimary LookupKind = "primary"
	// LookupIndex finds by a named index.
	LookupIndex LookupKind = "index"
	// LookupScan reads without an index.
	LookupScan LookupKind = "scan"
)

// Lookup says how a plan finds its rows.
type Lookup struct {
	Kind LookupKind

	// Index is the stored index name when Kind is LookupIndex.
	Index string

	// Args are indices into Plan.Args supplying each index member, in index
	// member order. Exactly one for LookupPrimary.
	Args []int
}

// Arg is one parameter of a plan.
type Arg struct {
	// Name is the parameter's name in the generated signature.
	Name string

	// Path is where the value comes from in the request message, in value
	// names: {"ref","key","value"}. A target renders it as an access
	// expression, so nobody hand-writes `v.tenant?.key?.value` twice and
	// spells it differently the second time.
	Path []schema.ValueName

	// Codec is the named transform applied on the way to storage.
	Codec string

	// Optional says the path may be absent, so a target renders a presence
	// check with a named error rather than passing undefined into the store.
	Optional bool

	// OneofCase, when set, is the oneof case this arg is only valid under.
	OneofCase string

	// Prop is what this argument is a value of, for a target that needs its
	// type. Nil for an argument that is not a prop value, such as a limit.
	Prop schema.Prop
}

// Assign is one value a write sets.
type Assign struct {
	// Prop is what is being written.
	Prop schema.Prop

	// Arg is an index into Plan.Args, when the value comes from the caller.
	Arg *int

	// Now stamps the store's clock. It is how an erase writes the erased time
	// and an update writes the version without the caller supplying either.
	Now bool

	// Clear writes null. It is how a patch's `<field>_null` companion erases a
	// value rather than setting it to zero — which is a distinction proto's
	// implicit presence cannot otherwise carry.
	Clear bool
}

// Guard is an optimistic lock.
//
// The comparison must happen inside the write, not before it. The Go side of
// this ecosystem folds it into the UPDATE's WHERE clause, so the whole
// compare-and-swap is one statement; a store without that has to open a
// transaction. Either way a stale write is refused, which is what the
// conformance suite asserts.
type Guard struct {
	// Prop is the version prop.
	Prop schema.Prop
	// Arg is the value the caller claims the row currently holds.
	Arg int
	// Force, when set, is an index into Args holding a boolean that drops the
	// guard. It is how `<field>_force` overrides a lock the caller cannot
	// satisfy.
	Force *int
}

// OrderBy is one sort term.
type OrderBy struct {
	Prop schema.Prop
	Desc bool
}

// Plan is one storage operation, fully described and free of any storage API.
type Plan struct {
	// ID is the plan's key in the emitted table: "get", "getBySlug".
	ID string

	Op Op

	// Entity is the entity this operates on.
	Entity *schema.Entity

	// By is how rows are found. Nil means every row, which is only valid for
	// OpList and OpInsert.
	By *Lookup

	// LiveOnly excludes rows carrying an erasure stamp.
	//
	// It is set here, once, for every read of a softly-erasing entity, because
	// "every read path" is a promise no per-call-site code keeps and a target
	// that had to remember it would forget it.
	LiveOnly bool

	// Args declares the plan's parameters in order, so a target emits a call
	// with the right arity and a test can assert it.
	Args []Arg

	// Set is what is written, for OpInsert, OpUpdate and OpErase.
	Set []Assign

	// Guard is the optimistic lock, for OpUpdate on a versioned entity.
	Guard *Guard

	Order []OrderBy
}

// Set is every plan for one entity.
type Set struct {
	Entity *schema.Entity
	Plans  []*Plan
}

// Plan finds a plan by id.
func (s Set) Plan(id string) (*Plan, bool) {
	for _, p := range s.Plans {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

// IDs is every plan id, in emission order.
func (s Set) IDs() []string {
	out := make([]string, len(s.Plans))
	for i, p := range s.Plans {
		out[i] = p.ID
	}
	return out
}
