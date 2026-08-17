// Package dexie is the Dexie/IndexedDB backend.
//
// It contributes storage policy: what the store can hold, where a value lives
// in a row, and what transform a value goes through. The syntax — the actual
// Dexie calls — lives in the TypeScript runtime adapter, not here.
//
// The one exception is the schema string. Dexie's `version().stores()` takes a
// mini-DSL ("&id,[alias+tenant.id]") that has no structured form, so it is
// built here and emitted as a constant. That makes this file the single place
// the store's index grammar is known, and the single place the compatibility
// branch below has to exist.
package dexie

import (
	"fmt"
	"slices"
	"strings"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/schema"
)

// Options is the `dexie:` config section.
type Options struct {
	// Compat reproduces protoc-gen-orm-ts bug for bug.
	//
	// "orm-ts" spells an index component with the field's proto name where the
	// row actually carries its JSON name, which is conformance item 4: the
	// store declares "&token_hash" and the query asks for {tokenHash}, so the
	// index is declared under a name no row has. IndexedDB does not error on an
	// unresolvable key path — it silently declines to index the record — which
	// is why it has been latent.
	//
	// It stays the default for now because changing a schema string changes the
	// stored schema, which needs a Dexie version bump, and a Dexie version
	// cannot be lowered: a rollback would leave browsers on a version the
	// reverted code cannot open. Fixing it is a planned forward migration, not
	// something to smuggle into a generator swap.
	Compat string `yaml:"compat"`

	// Accept is the capability shortfalls to warn about rather than refuse,
	// named one at a time.
	//
	// It exists because turning the spelling on and refusing what the store
	// cannot hold are two different decisions, and the schema this was built
	// against needs the first without the second: nine of its unique compound
	// indexes are real constraints that SQL enforces and a browser cache simply
	// cannot mirror. Dropping `unique` to silence them would delete the
	// constraint from the database that does hold it.
	//
	// A list rather than a boolean, because a boolean would also accept the
	// next shortfall, of some other kind, that nobody has looked at.
	Accept []string `yaml:"accept"`
}

const (
	// CompatORMTS reproduces protoc-gen-orm-ts, bugs included.
	CompatORMTS = "orm-ts"
	// CompatNone spells everything the way the row actually carries it.
	CompatNone = "none"
)

// Backend is the Dexie backend.
type Backend struct {
	opts Options
}

// New returns the backend, defaulting to bug-for-bug compatibility.
func New() *Backend { return &Backend{opts: Options{Compat: CompatORMTS}} }

func (b *Backend) Name() string { return "dexie" }

func (b *Backend) Configure(cfg *gen.Config, section string) error {
	opts := Options{Compat: CompatORMTS}
	if _, err := cfg.Section(section, &opts); err != nil {
		return err
	}
	switch opts.Compat {
	case CompatORMTS, CompatNone:
	case "":
		opts.Compat = CompatORMTS
	default:
		return fmt.Errorf("dexie: compat %q is not %q or %q", opts.Compat, CompatORMTS, CompatNone)
	}

	// A misspelled kind would accept nothing and look like it accepted
	// something, which is the failure mode this whole option exists to avoid.
	known := map[string]bool{}
	for _, k := range gen.AllShortfallKinds() {
		known[string(k)] = true
	}
	for _, name := range opts.Accept {
		if !known[name] {
			return fmt.Errorf("dexie: accept %q is not a shortfall kind\n\tcalque knows: %s",
				name, strings.Join(gen.ShortfallKindNames(), ", "))
		}
	}

	b.opts = opts
	return nil
}

// Strict is false while reproducing an older generator.
//
// Refusing on day one would mean generating nothing for any schema that already
// has a unique compound index, which is not a migration anyone can perform. The
// shortfall is still computed and reported; it just does not stop the build.
func (b *Backend) Strict() bool { return b.opts.Compat == CompatNone }

// Accepts refines Strict per kind.
//
// Compat mode accepts everything by definition -- reproducing a generator means
// not refusing what it produced. Once the spelling is fixed, only what the
// config named is let through, and anything else stops the build.
func (b *Backend) Accepts(kind gen.ShortfallKind) bool {
	if !b.Strict() {
		return true
	}
	return slices.Contains(b.opts.Accept, string(kind))
}

// Capabilities states what IndexedDB can hold, not what calque wishes it could.
func (b *Backend) Capabilities() gen.Capabilities {
	return gen.Capabilities{
		// It does hold one. `&[a+b]` parses as unique and compound -- Dexie
		// decides the two independently -- and createIndex gets `unique: true`
		// with the array key path, which IndexedDB enforces.
		//
		// This said false for a while, and docs/conformance.md item 3 was built
		// on it. The claim came from the generator being reproduced, which
		// emitted `[a+b]` and dropped the constraint; that the store could not
		// hold it was the inference, and it was wrong. The runtime package
		// measures it now: a second row with the same pair is a ConstraintError.
		UniqueCompoundIndex: true,
		// IndexedDB has no partial index, so a unique index cannot be narrowed
		// to the rows that are still there.
		PartialIndex: false,
		// It can index a path into a nested object: "tenant.id".
		NestedKeyPath: true,
		// A byte array cannot be a key, which is why a uuid is stored as text.
		BinaryKey:    false,
		Transactions: true,
		Codecs: []gen.CodecName{
			gen.CodecIdentity,
			gen.CodecUUIDString,
			gen.CodecJSON,
		},
	}
}

// StorePath is where a prop's value lives in a row.
//
// An edge is stored as its target's key under the edge's own name, which
// IndexedDB indexes as a nested key path.
func (b *Backend) StorePath(p schema.Prop) (schema.StorePath, error) {
	return schema.VisitProp[schema.StorePath](p, storePathVisitor{compat: b.compat()})
}

type storePathVisitor struct{ compat bool }

func (v storePathVisitor) VisitField(f *schema.Field) (schema.StorePath, error) {
	return schema.StorePath{schema.StoreName(name(f, v.compat))}, nil
}

func (v storePathVisitor) VisitEdge(e *schema.Edge) (schema.StorePath, error) {
	if e.Target() == nil || e.Target().Key() == nil {
		return nil, fmt.Errorf("dexie: %s points at an entity with no key", e.Name())
	}
	// Both components go through name(). The second one is read off the embedded
	// ref, which protoc-gen-es builds from the same descriptor and therefore
	// gives the same JSON name -- so spelling it with the proto name was the
	// same mistake as the first component, hidden by a corpus whose every key is
	// `id`.
	return schema.StorePath{
		schema.StoreName(name(e, v.compat)),
		schema.StoreName(name(e.Target().Key(), v.compat)),
	}, nil
}

// Codec is how a value is written.
//
// A uuid becomes canonical text because IndexedDB cannot index a byte array.
// The Go side of this ecosystem stores the same text, through google/uuid's
// driver.Valuer, so the two agree by construction rather than by accident —
// conformance item 7.
func (b *Backend) Codec(p schema.Prop) (gen.CodecName, error) {
	switch p.Type() {
	case schema.TypeUUID:
		return gen.CodecUUIDString, nil
	case schema.TypeJSON:
		return gen.CodecJSON, nil
	case schema.TypeMessage:
		// An edge is stored as its target's key, so its codec is the target's.
		if e, ok := p.(*schema.Edge); ok && e.Target() != nil && e.Target().Key() != nil {
			return b.Codec(e.Target().Key())
		}
		return gen.CodecJSON, nil
	default:
		return gen.CodecIdentity, nil
	}
}

func (b *Backend) Lower(s *schema.Schema) (*gen.Lowered, error) {
	l := &gen.Lowered{Schema: s, Backend: b.Name(), Tables: map[*schema.Entity]*gen.Table{}}

	for _, e := range s.Entities() {
		if e.Key() == nil {
			return nil, fmt.Errorf("dexie: %s has no key", e.FullName())
		}

		stores, err := b.SchemaString(e)
		if err != nil {
			return nil, err
		}

		t := &gen.Table{
			Entity: e,
			// The store name is the entity's full proto name, which the
			// consuming cache also derives by stripping "Service" from a
			// service name. Nothing enforces that coincidence; see the warning
			// the TypeScript target emits.
			Name:  e.FullName(),
			Path:  map[schema.Prop]schema.StorePath{},
			Codec: map[schema.Prop]gen.CodecName{},
			Extra: map[string]any{"stores": stores},
		}

		for _, p := range e.Props() {
			path, err := b.StorePath(p)
			if err != nil {
				return nil, fmt.Errorf("dexie: %s.%s: %w", e.FullName(), p.Name(), err)
			}
			codec, err := b.Codec(p)
			if err != nil {
				return nil, fmt.Errorf("dexie: %s.%s: %w", e.FullName(), p.Name(), err)
			}
			t.Path[p] = path
			t.Codec[p] = codec
		}

		l.Tables[e] = t
	}
	return l, nil
}

// SchemaString builds the `version().stores()` value for one entity.
//
// The grammar: the primary key first with "&", then every other candidate key —
// "&name" for a unique field or edge, "[a+b]" for an index. A compound index
// gets no "&" because Dexie has no unique form of one, which is exactly how the
// uniqueness silently disappears; Capabilities says so and the run reports it.
func (b *Backend) SchemaString(e *schema.Entity) (string, error) {
	var sb strings.Builder
	// The key goes through name() like everything else. It reads as though it
	// could not matter -- every key in the corpus is `id` -- but a multi-word
	// key would have been spelled wrong even with compat off, which is the one
	// place this bug could have survived the fix.
	sb.WriteString("&" + name(e.Key(), b.compat()))

	for _, key := range e.Keys() {
		if key == schema.Elem(e.Key()) {
			continue
		}
		part, err := b.keyPart(key)
		if err != nil {
			return "", err
		}
		sb.WriteString("," + part)
	}
	return sb.String(), nil
}

func (b *Backend) keyPart(key schema.Elem) (string, error) {
	return schema.VisitElem[string](key, keyPartVisitor{compat: b.compat()})
}

type keyPartVisitor struct{ compat bool }

func (v keyPartVisitor) VisitField(f *schema.Field) (string, error) {
	return "&" + name(f, v.compat), nil
}

func (v keyPartVisitor) VisitEdge(e *schema.Edge) (string, error) {
	if e.Target() == nil || e.Target().Key() == nil {
		return "", fmt.Errorf("%s points at an entity with no key", e.Name())
	}
	return "&" + name(e, v.compat) + "." + name(e.Target().Key(), v.compat), nil
}

func (v keyPartVisitor) VisitIndex(idx *schema.Index) (string, error) {
	parts := make([]string, 0, len(idx.Props()))
	for _, p := range idx.Props() {
		part, err := schema.VisitProp[string](p, indexMemberVisitor{compat: v.compat})
		if err != nil {
			return "", fmt.Errorf("{indexes}(%s): %w", idx.Name(), err)
		}
		parts = append(parts, part)
	}
	// Brackets say how many members there are; "&" says the index is unique.
	// They are separate axes and the generator being reproduced ran them
	// together -- it always bracketed and never marked unique, which is two
	// distinct failures.
	//
	// A one-member index registers under the name "[a]", and `where({a})` takes
	// Dexie's single-key branch, which looks an index up by the name "a". So the
	// index it declares can never be found by the query it generates. A
	// multi-member one is queryable, because that branch searches by key path,
	// but it is not unique -- and Dexie holds `&[a+b]` perfectly well, which is
	// measured in the runtime package's tests rather than assumed here.
	if v.compat {
		return "[" + strings.Join(parts, "+") + "]", nil
	}

	// Every index that reaches a schema string came from Entity.Keys(), which
	// yields only unique elements, so this is always "&". It is asked rather
	// than assumed because the answer is the whole point of the line.
	prefix := ""
	if idx.IsUnique() {
		prefix = "&"
	}
	if len(parts) == 1 {
		return prefix + parts[0], nil
	}
	return prefix + "[" + strings.Join(parts, "+") + "]", nil
}

type indexMemberVisitor struct{ compat bool }

func (v indexMemberVisitor) VisitField(f *schema.Field) (string, error) {
	return name(f, v.compat), nil
}

func (v indexMemberVisitor) VisitEdge(e *schema.Edge) (string, error) {
	if e.Target() == nil || e.Target().Key() == nil {
		return "", fmt.Errorf("%s points at an entity with no key", e.Name())
	}
	return name(e, v.compat) + "." + name(e.Target().Key(), v.compat), nil
}

func (b *Backend) compat() bool { return b.opts.Compat == CompatORMTS }

// name is the whole of conformance item 4.
//
// A row carries a prop under its JSON name, because that is what protoc-gen-es
// puts on a message. In compat mode the schema string spells it with the proto
// name instead, which for a multi-word prop declares an index nothing has.
func name(p schema.Prop, compat bool) string {
	if compat {
		return string(p.Names().Proto)
	}
	return string(p.Names().Value)
}

// Mismatches reports the props whose two spellings differ, so a run can say
// which indexes are declared under a name no row carries.
//
// It answers nothing when compat is off, because then there is nothing to
// report.
func (b *Backend) Mismatches(e *schema.Entity) []schema.Prop {
	if !b.compat() {
		return nil
	}
	var out []schema.Prop
	for _, key := range e.Keys() {
		if key == schema.Elem(e.Key()) {
			continue
		}
		switch k := key.(type) {
		case *schema.Field:
			if differs(k) {
				out = append(out, k)
			}
		case *schema.Edge:
			if differs(k) {
				out = append(out, k)
			}
		case *schema.Index:
			for _, p := range k.Props() {
				if differs(p) {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func differs(p schema.Prop) bool {
	return string(p.Names().Proto) != string(p.Names().Value)
}
