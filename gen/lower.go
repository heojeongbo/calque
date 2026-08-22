package gen

import (
	"fmt"

	"github.com/heojeongbo/calque/schema"
)

// CodecTable is the transform a store puts each type through, as data.
//
// Every backend wrote the same switch: a few types it treats specially, an edge
// that takes its target's key's codec, and identity for everything else. Two of
// those three arms were the same in all three backends and the edge arm was
// byte-identical, which is to say a fix to one was a fix to one.
//
// A table rather than a method because there is nothing to decide: which codec a
// type gets is a fact about the store, and a fact is data. What is left in code
// is the two rules that are the same wherever the table came from, and they are
// below.
type CodecTable map[schema.Type]CodecName

// Codec is the transform one prop's value goes through.
//
// Two rules, and neither is the table's: an edge is stored as its target's key,
// so its codec is the target's; and a type the table does not name is stored as
// it arrives.
//
// It cannot fail. An edge whose target has no key falls through to the table,
// which is what all three backends did by writing the guard into the condition
// -- schema.Build refuses that schema long before a backend sees it, so the arm
// is unreachable rather than lenient.
func (t CodecTable) Codec(p schema.Prop) CodecName {
	if e, ok := p.(*schema.Edge); ok {
		if key, err := e.TargetKey(); err == nil {
			return t.Codec(key)
		}
	}
	if c, ok := t[p.Type()]; ok {
		return c
	}
	return CodecIdentity
}

// LowerTables is the loop every backend wrote: one Table per entity, a codec for
// every prop, and whatever the runtime adapter needs in Extra.
//
// It covers s.Entities() and not s.Sources(), which docs/extending.md had to say
// in prose because nothing enforced it: an entity reachable only as an edge
// target is not emitted, and a target still asks what its key looks like.
//
// The error prefix is the backend's own name, so the three copies that spelled
// it as a literal cannot drift from the name the config uses to select them.
//
// extra may be nil, for a backend whose runtime adapter needs nothing the
// neutral schema cannot say.
func LowerTables(b Backend, s *schema.Schema, extra func(*schema.Entity) (map[string]any, error)) (*Lowered, error) {
	l := &Lowered{Schema: s, Backend: b.Name(), Tables: map[*schema.Entity]*Table{}}

	for _, e := range s.Entities() {
		if e.Key() == nil {
			return nil, fmt.Errorf("%s: %s has no key", b.Name(), e.FullName())
		}

		t := &Table{Entity: e, Codec: map[schema.Prop]CodecName{}}
		if extra != nil {
			x, err := extra(e)
			if err != nil {
				return nil, err
			}
			t.Extra = x
		}

		for _, p := range e.Props() {
			codec, err := b.Codec(p)
			if err != nil {
				return nil, fmt.Errorf("%s: %s.%s: %w", b.Name(), e.FullName(), p.Name(), err)
			}
			t.Codec[p] = codec
		}

		l.Tables[e] = t
	}
	return l, nil
}
