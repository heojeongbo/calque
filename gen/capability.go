package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/HeoJeongBo/calque/schema"
)

// ShortfallKind names a way a backend can come up short, so that accepting one
// is a decision about that one rather than about all of them.
//
// The names are config surface: a backend that lets a shortfall through says
// which by writing it down.
type ShortfallKind string

const (
	// ShortfallUniqueCompoundIndex: the store will create the index and it
	// will not be unique.
	ShortfallUniqueCompoundIndex ShortfallKind = "unique_compound_index"
	// ShortfallPartialIndex: a unique index would cover erased rows too.
	ShortfallPartialIndex ShortfallKind = "partial_index"
	// ShortfallIndexArity: the index has more members than the store indexes.
	ShortfallIndexArity ShortfallKind = "index_arity"
	// ShortfallUnorderableMember: an index member has no order to index by.
	ShortfallUnorderableMember ShortfallKind = "unorderable_index_member"
	// ShortfallBinaryKey: a byte string cannot be a key.
	ShortfallBinaryKey ShortfallKind = "binary_key"
	// ShortfallNoTransactions: an optimistic lock cannot be made atomic.
	ShortfallNoTransactions ShortfallKind = "no_transactions"
)

// AllShortfallKinds is every kind, for a config that names one and for a test
// that wants to be sure it named a real one.
func AllShortfallKinds() []ShortfallKind {
	return []ShortfallKind{
		ShortfallUniqueCompoundIndex,
		ShortfallPartialIndex,
		ShortfallIndexArity,
		ShortfallUnorderableMember,
		ShortfallBinaryKey,
		ShortfallNoTransactions,
	}
}

// ShortfallKindNames is the kinds as strings, sorted, for an error that has to
// list what it understands.
func ShortfallKindNames() []string {
	out := make([]string, 0, len(AllShortfallKinds()))
	for _, k := range AllShortfallKinds() {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}

// Shortfall is one place the schema needs something the backend cannot do.
type Shortfall struct {
	Kind ShortfallKind
	Path string
	Msg  string
	Hint string
}

func (s Shortfall) String() string {
	out := s.Path + ": " + s.Msg
	if s.Hint != "" {
		out += "\n\t" + s.Hint
	}
	return out
}

// Shortfalls is what a check found.
type Shortfalls []Shortfall

// Err renders them the way a schema diagnostic is rendered: one per line.
func (s Shortfalls) Err() error {
	if len(s) == 0 {
		return nil
	}
	parts := make([]string, len(s))
	for i, sf := range s {
		parts[i] = sf.String()
	}
	return fmt.Errorf("%s", strings.Join(parts, "\n"))
}

// ShortfallAccepter is a backend that decides per kind rather than all at once.
//
// It is optional, and a backend that does not implement it is judged by
// Strict() exactly as before. Putting it on gen.Backend would make every
// implementation answer a question only some of them have an opinion about,
// which is the same reason EntIdent is not there either.
type ShortfallAccepter interface {
	Accepts(ShortfallKind) bool
}

// Partition splits what was found into what this backend will live with and
// what stops the run.
func (s Shortfalls) Partition(b Backend) (accepted, refused Shortfalls) {
	acc, perKind := b.(ShortfallAccepter)
	for _, sf := range s {
		ok := !b.Strict()
		if perKind {
			ok = acc.Accepts(sf.Kind)
		}
		if ok {
			accepted = append(accepted, sf)
			continue
		}
		refused = append(refused, sf)
	}
	return accepted, refused
}

// CheckCapabilities reports every place the schema needs something the backend
// cannot do.
//
// It never downgrades and never warns. A constraint the store will not hold is
// a constraint the schema does not have, and the difference between those two
// is data corruption nobody sees until it is already there. What to do about
// each one is Partition's question, and the backend's.
//
// This is not hypothetical. protoc-gen-orm-ts emitted a unique compound index
// as a plain one, because Dexie has no unique form of it, and said nothing —
// and the application that consumed it later carried a database version bump
// whose comment records restoring an index that had gone non-unique in
// production. That is the failure this function exists to convert into a
// message.
func CheckCapabilities(s *schema.Schema, b Backend) Shortfalls {
	caps := b.Capabilities()
	var out Shortfalls

	for _, e := range s.Entities() {
		at := e.FullName()

		for _, idx := range e.Indexes() {
			out = append(out, checkIndex(idx, caps, b.Name(), at)...)
		}
		for _, p := range e.Props() {
			out = append(out, checkProp(p, caps, b.Name(), at)...)
		}
		if e.Version() != nil && !caps.Transactions {
			out = append(out, Shortfall{
				Kind: ShortfallNoTransactions,
				Path: at + "." + string(e.Version().Name()),
				Msg:  fmt.Sprintf("backend %q has no transactions, so it cannot hold an optimistic lock", b.Name()),
				Hint: "a version check has to be atomic with the write it guards; without that it is decoration",
			})
		}
	}

	return out
}

func checkIndex(idx *schema.Index, caps Capabilities, backend, at string) Shortfalls {
	where := fmt.Sprintf("%s.{indexes}(%s)", at, idx.Name())
	var out Shortfalls

	if idx.IsUnique() && idx.IsComposite() && !caps.UniqueCompoundIndex {
		out = append(out, Shortfall{
			Kind: ShortfallUniqueCompoundIndex,
			Path: where,
			Msg:  fmt.Sprintf("backend %q cannot enforce a unique index over %d properties", backend, len(idx.Props())),
			Hint: "the index would be created and would not be unique -- drop `unique: true`, or accept " +
				string(ShortfallUniqueCompoundIndex) + " if another store holds it and this one is a cache",
		})
		// Everything below is also true of this index, and saying four things
		// about one declaration buries the one that matters.
		return out
	}

	if idx.ExcludesErased() && !caps.PartialIndex {
		out = append(out, Shortfall{
			Kind: ShortfallPartialIndex,
			Path: where,
			Msg:  fmt.Sprintf("backend %q has no partial index, so this unique index would cover erased rows too", backend),
			Hint: "an erased row would go on holding the name it had, so the name could never be used again",
		})
	}

	if n := caps.MaxIndexArity; n > 0 && len(idx.Props()) > n {
		out = append(out, Shortfall{
			Kind: ShortfallIndexArity,
			Path: where,
			Msg: fmt.Sprintf("backend %q indexes at most %d properties, but this index has %d",
				backend, n, len(idx.Props())),
		})
	}

	for _, p := range idx.Props() {
		out = append(out, checkMember(p, where, backend)...)
	}
	return out
}

// checkMember asks whether an index member has an order to index by.
//
// An edge is asked about its *target's key*, never about itself. Its own type
// is always message, which is never orderable, so asking directly reports every
// edge in every index as an error -- which is what happened, to backends that
// support nested key paths, because the guard was written the other way round.
// Whether the backend reaches the key as `holder.id` or as a flattened
// `holder_id` column is StorePath's answer and does not change the type.
func checkMember(p schema.Prop, where, backend string) Shortfalls {
	t := p.Type()
	if edge, ok := p.(*schema.Edge); ok {
		if edge.Target() == nil || edge.Target().Key() == nil {
			return nil // Build reports this; it is not a capability question.
		}
		t = edge.Target().Key().Type()
	}
	if t.IsOrderable() {
		return nil
	}
	return Shortfalls{{
		Kind: ShortfallUnorderableMember,
		Path: where,
		Msg:  fmt.Sprintf("%s is %s, which has no order a store can index by", p.Name(), t),
	}}
}

func checkProp(p schema.Prop, caps Capabilities, backend, at string) Shortfalls {
	// Only a prop that has to be a key or an index member is constrained by the
	// key-type rules; an ordinary column can be anything the codec can carry.
	if !p.IsUnique() {
		return nil
	}
	if p.Type() == schema.TypeBytes && !caps.BinaryKey {
		return Shortfalls{{
			Kind: ShortfallBinaryKey,
			Path: at + "." + string(p.Name()),
			Msg:  fmt.Sprintf("backend %q cannot use a byte string as a key", backend),
			// Not TYPE_UUID: the fields this actually catches are content
			// digests and token hashes, which are longer than sixteen bytes and
			// would not fit one.
			Hint: "store it as text instead, or make it not unique, or accept " +
				string(ShortfallBinaryKey) + " and leave the index not working",
		}}
	}
	return nil
}
