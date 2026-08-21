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

// Table is one entity as one backend decided to store it.
//
// It is narrow, and it used to be wider. There were a table name, a primary
// key, a list of indexes and a path per prop, modelled neutrally so that a
// target could read a store's decisions without knowing which store. No target
// ever did: both ask their own backend directly, because the questions worth
// asking turned out to be store-specific ones — the ent identifier for a prop,
// the Dexie schema string for an entity — and a neutral answer to a
// store-specific question is a stub. Carrying the fields anyway meant every
// backend maintained a second description of itself that nothing read, and a
// mistake in it could not be caught by anything.
//
// What is left is what the core itself reads. See
// docs/architecture.md#target-specific-backend-extensions for the seam that
// replaced it.
type Table struct {
	Entity *schema.Entity

	// Codec is the transform each prop's value goes through. Run checks every
	// entry against the backend's own Capabilities, which is the one thing the
	// core does with a lowering.
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
