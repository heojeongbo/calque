package gen

import (
	"fmt"

	"github.com/heojeongbo/calque/schema"
)

// CodecName is the transform a value goes through on its way into and out of
// storage.
//
// The name is the contract. A Go backend picks it; a runtime adapter implements
// it. That split is deliberate: protoc-gen-orm-ts inlined `uuid.u8_str(...)`
// into the generated code in six places, which made the generated file's
// meaning depend on one store's idea of what a key looks like.
type CodecName string

const (
	// CodecIdentity stores the value as it arrives.
	CodecIdentity CodecName = "identity"
	// CodecUUIDString stores a uuid as canonical hyphenated text. IndexedDB
	// cannot index a byte array, and SQL drivers overwhelmingly write uuids
	// this way, so it is what both existing targets converge on.
	CodecUUIDString CodecName = "uuid-string"
	// CodecUUIDBytes stores a uuid as its sixteen bytes.
	CodecUUIDBytes CodecName = "uuid-bytes"
	// CodecTimeEpochMillis stores a time as milliseconds since the epoch.
	CodecTimeEpochMillis CodecName = "time-epoch-millis"
	// CodecTimeNative stores a time as whatever the store's own time type is.
	CodecTimeNative CodecName = "time-native"
	// CodecJSON stores a value as a document.
	CodecJSON CodecName = "json"
)

// Capabilities is what a backend can do, in the terms a schema is written in.
//
// Every entry is here because some schema is expressible in proto and not
// expressible in some store, and naming the difference is what lets calque
// refuse at generate time instead of shipping something that quietly does
// something else.
type Capabilities struct {
	// UniqueCompoundIndex: the store enforces uniqueness across more than one
	// property.
	//
	// This is the entry that earns the whole type. ent writes
	// index.Fields("alias").Edges("tenant").Unique() and the database holds it.
	// Dexie has no unique form of a compound index, so protoc-gen-orm-ts
	// emitted `[alias+tenant.id]` without the `&` and said nothing -- and the
	// consuming application later carried a schema version bump to recover from
	// an index that had gone non-unique in production.
	UniqueCompoundIndex bool

	// PartialIndex: the store can index only the rows matching a predicate,
	// which is what a unique index on a softly-erasing entity needs. Without
	// it, an erased row goes on holding the name it had forever.
	PartialIndex bool

	// NestedKeyPath: the store can index a path into a nested object.
	NestedKeyPath bool

	// BinaryKey: a byte string can be a key or an index member.
	BinaryKey bool

	// Transactions: a read and a dependent write can be made atomic, which
	// runtime-enforced uniqueness and optimistic locking both need.
	Transactions bool

	// MaxIndexArity is the largest compound index the store supports; 0 means
	// no limit.
	MaxIndexArity int

	// Codecs is every CodecName this backend's runtime implements. The core
	// checks that every codec a lowering chose is in here, so a backend cannot
	// name a transform nobody wrote.
	Codecs []CodecName
}

// Supports reports whether the backend implements the named codec.
func (c Capabilities) Supports(codec CodecName) bool {
	for _, have := range c.Codecs {
		if have == codec {
			return true
		}
	}
	return false
}

// Backend is a storage engine calque can target.
//
// It contributes no syntax. What it contributes is what it can do, what a value
// has to look like to be stored, and where a value lives in a record.
type Backend interface {
	// Name is the value of `backend:` in the config, and the key of this
	// backend's own options section.
	Name() string

	Capabilities() Capabilities

	// Configure claims this backend's config section. Called once, before
	// anything is lowered, so a bad option fails before a file is rendered.
	Configure(cfg *Config, section string) error

	// Strict reports whether a capability shortfall stops generation.
	//
	// Capabilities states facts; this states policy, and they are separate on
	// purpose. A backend reproducing an older generator bug for bug answers
	// false, and the shortfall is reported on stderr instead of refusing —
	// otherwise adopting calque on an existing schema would generate nothing at
	// all, which is not a migration anyone can perform. It answers true once
	// the constraint is meant to be real.
	Strict() bool

	// StorePath is where a prop's value lives in a stored record.
	//
	// It uses schema.VisitElem, so a new Elem variant is a compile error here
	// rather than a panic at run time.
	StorePath(p schema.Prop) (schema.StorePath, error)

	// Codec names the transform a prop's value goes through.
	Codec(p schema.Prop) (CodecName, error)

	// Lower turns a validated neutral schema into storage decisions.
	//
	// Capability checking has already run, so an error here is a bug or a
	// schema-specific limit the capability set was too coarse to state. It
	// reports; it does not panic.
	Lower(s *schema.Schema) (*Lowered, error)
}

// Enforcement says who guarantees a constraint.
type Enforcement string

const (
	// EnforceStore means the store holds the constraint itself.
	EnforceStore Enforcement = "store"
	// EnforceRuntime means the adapter must check inside the write
	// transaction. It is only ever chosen by an explicit config opt-in,
	// because it costs a transaction on every write.
	EnforceRuntime Enforcement = "runtime"
	// EnforceNone means nothing holds it. It is never chosen implicitly: a
	// constraint nothing holds is a constraint the schema does not have.
	EnforceNone Enforcement = "none"
)

// StoredIndex is one index as the store will create it.
type StoredIndex struct {
	// Name is the index's identity in the store and in a query plan's lookup.
	Name string

	// Members are the stored paths, in index order.
	Members []schema.StorePath

	Unique bool

	// Partial narrows the index. For soft delete it is "the erased column is
	// null", expressed neutrally.
	Partial *schema.StorePath

	// Enforcement says who guarantees Unique.
	Enforcement Enforcement

	// Source is the schema element this came from, for diagnostics. It is nil
	// for an index the backend synthesized.
	Source schema.Elem
}

// Table is one entity as one backend decided to store it.
type Table struct {
	Entity *schema.Entity

	// Name is the storage-level table name.
	Name string

	PrimaryKey StoredIndex

	// Indexes is every index the store will create, including ones promoted
	// from a unique field.
	Indexes []StoredIndex

	// Path is where each prop's value lives.
	Path map[schema.Prop]schema.StorePath

	// Codec is the transform each prop's value goes through.
	Codec map[schema.Prop]CodecName

	// Extra is JSON-able and is emitted verbatim under the descriptor's
	// backend key. Anything a runtime adapter needs and the neutral schema
	// cannot say goes here, and nothing in the core reads it.
	Extra map[string]any
}

// Lowered is what a backend makes of one schema: the neutral IR plus every
// decision the backend had to make about it.
//
// Nothing in schema is mutated, so two backends can lower the same schema in
// one run and a target can hold both. The alternative -- hanging a hints map on
// schema.Entity -- makes the IR mutable and shared, so two backends in one run
// trample each other and every test that compares an entity has to reason about
// hint state.
type Lowered struct {
	Schema  *schema.Schema
	Backend string
	Tables  map[*schema.Entity]*Table
}

// Table finds the lowering for an entity.
func (l *Lowered) Table(e *schema.Entity) (*Table, error) {
	t, ok := l.Tables[e]
	if !ok {
		return nil, fmt.Errorf("gen: backend %q did not lower %s", l.Backend, e.FullName())
	}
	return t, nil
}
