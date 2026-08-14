package gen

import (
	"fmt"

	"github.com/HeoJeongBo/calque/schema"
)

// CheckCapabilities reports every place the schema needs something the backend
// cannot do.
//
// It never downgrades and never warns. A constraint the store will not hold is
// a constraint the schema does not have, and the difference between those two
// is data corruption nobody sees until it is already there.
//
// This is not hypothetical. protoc-gen-orm-ts emitted a unique compound index
// as a plain one, because Dexie has no unique form of it, and said nothing —
// and the application that consumed it later carried a database version bump
// whose comment records restoring an index that had gone non-unique in
// production. That is the failure this function exists to convert into a
// message.
func CheckCapabilities(s *schema.Schema, b Backend) error {
	caps := b.Capabilities()
	var diags schema.Diagnostics

	for _, e := range s.Entities() {
		at := diags.At(e.FullName())

		for _, idx := range e.Indexes() {
			checkIndex(idx, caps, b.Name(), at)
		}
		for _, p := range e.Props() {
			checkProp(p, caps, b.Name(), at)
		}
		if e.Version() != nil && !caps.Transactions {
			at.Hintf(string(e.Version().Name()),
				fmt.Sprintf("backend %q has no transactions, so it cannot hold an optimistic lock", b.Name()),
				"a version check has to be atomic with the write it guards; without that it is decoration")
		}
	}

	return diags.Err()
}

func checkIndex(idx *schema.Index, caps Capabilities, backend string, at *schema.Diagnostics) {
	where := fmt.Sprintf("{indexes}(%s)", idx.Name())

	if idx.IsUnique() && idx.IsComposite() && !caps.UniqueCompoundIndex {
		at.Hintf(where,
			fmt.Sprintf("backend %q cannot enforce a unique index over %d properties", backend, len(idx.Props())),
			"the index would be created and would not be unique -- drop `unique: true`, or choose a backend that holds it")
		return
	}

	if idx.ExcludesErased() && !caps.PartialIndex {
		at.Hintf(where,
			fmt.Sprintf("backend %q has no partial index, so this unique index would cover erased rows too", backend),
			"an erased row would go on holding the name it had, so the name could never be used again")
	}

	if n := caps.MaxIndexArity; n > 0 && len(idx.Props()) > n {
		at.Addf(where, "backend %q indexes at most %d properties, but this index has %d",
			backend, n, len(idx.Props()))
	}

	for _, p := range idx.Props() {
		if _, isEdge := p.(*schema.Edge); isEdge && !caps.NestedKeyPath {
			// Not an error on its own: a backend that flattens an edge into a
			// column of its own says so through StorePath. It is only a problem
			// if the backend also has no way to name that column, which Lower
			// reports.
			continue
		}
		if !p.Type().IsOrderable() {
			at.Addf(where, "%s is %s, which has no order a store can index by",
				p.Name(), p.Type())
		}
	}
}

func checkProp(p schema.Prop, caps Capabilities, backend string, at *schema.Diagnostics) {
	// Only a prop that has to be a key or an index member is constrained by the
	// key-type rules; an ordinary column can be anything the codec can carry.
	if !p.IsUnique() {
		return
	}
	if p.Type() == schema.TypeBytes && !caps.BinaryKey {
		at.Hintf(string(p.Name()),
			fmt.Sprintf("backend %q cannot use a byte string as a key", backend),
			"give it (orm.field) = {type: TYPE_UUID} so it is stored as text, or make it not unique")
	}
}
