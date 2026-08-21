package ts

import (
	"fmt"

	"github.com/heojeongbo/calque/backend/dexie"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/tsw"
	"github.com/heojeongbo/calque/schema"
)

// emitEntity writes <entity>.db.g.ts.
func (t *Target) emitEntity(g *gen.Generator, b *dexie.Backend, e *entity) ([]byte, error) {
	def := e.def
	name := def.Name()
	desc := name + "Schema"

	stores, err := b.SchemaString(def)
	if err != nil {
		return nil, fmt.Errorf("ts: %s: %w", def.FullName(), err)
	}

	f := tsw.New()
	f.P(t.opts.Header)
	f.P()

	// The first four imports end in a semicolon and the last three do not. That
	// is what the predecessor emits, and the diff is byte-for-byte.
	f.P(`import { create, type MessageInitShape } from "@bufbuild/protobuf";`)
	f.P(`import { Code } from "@connectrpc/connect";`)
	f.P(`import type { DbOf, EntityOf, Key, ValueOf } from "`, t.opts.Runtime, `";`)
	f.P(`import { TableBase, uuid } from "`, t.opts.Runtime, `";`)
	f.P()
	f.P(`import type { `, name, `ServiceClient } from "./client.g`, t.opts.ImportExtension, `"`)
	f.P(`import { type `, name, `, `, desc, ` } from "./`, e.module(), t.opts.ImportExtension, `"`)
	f.P(`import type { `, name, `GetRequestSchema } from "./`, e.serviceModule(), t.opts.ImportExtension, `"`)
	f.P()

	f.P(`type Desc = typeof `, desc)
	f.P(`export type Db = DbOf<Desc>`)
	f.P()

	f.P(`export const TableName = "`, def.FullName(), `";`)
	f.P(`export const Schema = "`, stores, `" as const;`)
	f.P()

	f.P(`export class TableService extends TableBase<Desc> implements Partial<`, name, `ServiceClient> {`)
	f.P("\tconstructor(db: Db) {")
	f.P("\t\tsuper(db, ", desc, ");")
	f.P("\t}")
	f.P()

	t.emitDehydrate(f, def, name)
	t.emitHydrate(f, def, name)
	t.emitVersioned(f, def)

	if err := t.emitGet(f, e, name); err != nil {
		return nil, err
	}

	f.P(`}`)
	return f.Bytes(), nil
}

// emitDehydrate writes the value → row conversion.
//
// Only uuid-typed components are converted, because the conversion exists for
// one reason: IndexedDB cannot index a byte array. Everything else is stored as
// it arrives.
//
// It walks Keys() rather than Props(), so an edge gets a conversion line only
// when it is part of a candidate key. Robot has three edges and one of them is
// in the slug index; only that one appears here.
func (t *Target) emitDehydrate(f *tsw.File, e *schema.Entity, name string) {
	key := e.Key()

	f.P("\t_dehydrate(v: ", name, "): [Key, any] {")
	f.P("\t\tconst w = v as unknown as any")

	if key.Type() == schema.TypeUUID {
		f.P("\t\tconst k = uuid.u8_str(v.", key.Names().Value, ")")
		f.P(`		if(k === undefined) throw this._err("invalid key", Code.InvalidArgument)`)
	} else {
		f.P("\t\tconst k = v.", key.Names().Value)
	}
	f.P("\t\tw.", key.Names().Value, " = k")

	forEachUUIDComponent(e, false, func(write, read string) {
		f.P("\t\tw.", write, " = uuid.u8_str(v.", read, ")")
	})

	f.P("\t\treturn [k, v]")
	f.P("\t}")
	f.P()
}

// emitHydrate writes the row → value conversion, which includes the primary key
// where dehydrate handles it separately.
func (t *Target) emitHydrate(f *tsw.File, e *schema.Entity, name string) {
	f.P("\t_hydrate(v: any): ", name, " {")

	forEachUUIDComponent(e, true, func(write, read string) {
		f.P("\t\tv.", write, " = uuid.str_u8(v.", read, ")")
	})

	f.P("\t\treturn create(this._schema, v)")
	f.P("\t}")
	f.P()
}

// forEachUUIDComponent walks the candidate keys and calls fn for each uuid-typed
// component, with the path to write and the path to read.
//
// They differ for an edge: the write is `tenant.id` and the read is
// `tenant?.id`, because the edge may be absent. Note that the write is *not*
// guarded — `w.tenant.id = ...` throws when tenant is undefined, which is a
// live bug the consuming application works around by requiring every key edge
// in every caller's field mask. It is reproduced here; fixing it is a separate
// change with its own diff.
func forEachUUIDComponent(e *schema.Entity, includeKey bool, fn func(write, read string)) {
	for _, key := range e.Keys() {
		if !includeKey && key == schema.Elem(e.Key()) {
			continue
		}

		switch k := key.(type) {
		case *schema.Field:
			if k.Type() == schema.TypeUUID {
				name := string(k.Names().Value)
				fn(name, name)
			}

		case *schema.Edge:
			if tk := targetKey(k); tk != nil && tk.Type() == schema.TypeUUID {
				name := string(k.Names().Value)
				fn(name+"."+string(tk.Names().Value), name+"?."+string(tk.Names().Value))
			}

		case *schema.Index:
			for _, p := range k.Props() {
				switch m := p.(type) {
				case *schema.Field:
					if m.Type() == schema.TypeUUID {
						name := string(m.Names().Value)
						fn(name, name)
					}
				case *schema.Edge:
					if tk := targetKey(m); tk != nil && tk.Type() == schema.TypeUUID {
						name := string(m.Names().Value)
						fn(name+"."+string(tk.Names().Value), name+"?."+string(tk.Names().Value))
					}
				}
			}
		}
	}
}

func targetKey(e *schema.Edge) *schema.Field {
	if e.Target() == nil {
		return nil
	}
	return e.Target().Key()
}

// emitVersioned writes the optimistic-lock comparison, for an entity that has a
// version field.
//
// The comparison reads .seconds and .nanos, which is protoc-gen-es's Timestamp
// shape. ormcompat already guarantees a version field is a time, so the shape
// is known rather than assumed.
func (t *Target) emitVersioned(f *tsw.File, e *schema.Entity) {
	ver := e.Version()
	if ver == nil {
		return
	}
	a := "a." + string(ver.Names().Value)
	b := "b." + string(ver.Names().Value)

	f.P("\t_versioned(): boolean {")
	f.P("\t\treturn true")
	f.P("\t}")
	f.P()
	f.P("\t_compare(a: ValueOf<Desc> | EntityOf<Desc>, b: ValueOf<Desc> | EntityOf<Desc>): number {")
	f.P("\t\tif(", a, " === undefined) return 1")
	f.P("\t\tif(", b, " === undefined) return -1")
	f.P("\t\tconst d = ", a, ".seconds - ", b, ".seconds")
	f.P("\t\tif(d > 0) return 1")
	f.P("\t\tif(d < 0) return -1")
	f.P("\t\treturn ", a, ".nanos - ", b, ".nanos")
	f.P("\t}")
	f.P()
}

// emitGet writes the lookup, one case per candidate key.
//
// The switch has no default beyond the error arm, and every case comes from
// Keys(), so a candidate key that cannot be looked up does not silently vanish.
// The predecessor's version of this ends in
// panic("unimplemented: key type not Field") and a unique edge reaches it.
func (t *Target) emitGet(f *tsw.File, e *entity, name string) error {
	def := e.def
	key := def.Key()

	f.P("\tget(req: MessageInitShape<typeof ", name, "GetRequestSchema>): Promise<", name, "> {")
	f.P("\t\tif(req.ref?.", e.keyProp, ` === undefined) return Promise.reject(this._err("key undefined", Code.InvalidArgument));`)
	// Destructured to the local name `key` so every case body below reads the
	// same whatever the oneof is called -- and in shorthand when they coincide,
	// which is every schema that spells it `key`.
	if e.keyProp == "key" {
		f.P("\t\tconst { key } = req.ref;")
	} else {
		f.P("\t\tconst { ", e.keyProp, ": key } = req.ref;")
	}
	f.P("\t\tswitch(key.case) {")

	label, ok := caseLabel(e.ref, key)
	if !ok {
		label = string(key.Names().Value)
	}
	f.P(`			case "`, label, `": {`)
	f.P("\t\t\t\tconst k = ", keyer(key.Type(), "key.value"), ";")
	f.P(`				if(k === undefined) return Promise.reject(this._err("invalid key", Code.InvalidArgument));`)
	f.P("\t\t\t\treturn this._query(t => t.get(k));")
	f.P("\t\t\t}")
	f.P()

	for _, cand := range def.Keys() {
		if cand == schema.Elem(key) {
			continue
		}
		if err := t.emitGetCase(f, e, cand); err != nil {
			return err
		}
	}

	f.P("\t\t\tdefault:")
	f.P("				return Promise.reject(this._err(`unknown key: ${key.case}`, Code.InvalidArgument));")
	f.P("\t\t}")
	f.P("\t}")
	return nil
}

func (t *Target) emitGetCase(f *tsw.File, e *entity, cand schema.Elem) error {
	label, ok := caseLabel(e.ref, cand)
	if !ok {
		label = defaultLabel(cand)
	}
	f.P(`			case "`, label, `": {`)

	if _, err := schema.VisitElem[struct{}](cand, &getCaseVisitor{f: f, e: e}); err != nil {
		return err
	}

	f.P("\t\t\t}")
	f.P()
	return nil
}

// getCaseVisitor writes the body of one lookup case.
//
// It is a visitor so that a new Elem variant is a compile error here. This is
// the exact spot the predecessor panics.
type getCaseVisitor struct {
	f *tsw.File
	e *entity
}

func (v *getCaseVisitor) VisitField(field *schema.Field) (struct{}, error) {
	name := string(field.Names().Value)
	v.f.P("\t\t\t\tconst k = ", keyer(field.Type(), "key.value"))
	v.f.P("\t\t\t\tconst q = { ", name, ": k }")
	v.f.P("\t\t\t\treturn this._query(t => t.where(q).first());")
	return struct{}{}, nil
}

func (v *getCaseVisitor) VisitEdge(edge *schema.Edge) (struct{}, error) {
	tk := targetKey(edge)
	if tk == nil {
		return struct{}{}, fmt.Errorf("ts: %s points at an entity with no key", edge.Name())
	}
	name := string(edge.Names().Value)
	path := name + "." + string(tk.Names().Value)

	tp := v.e.targetKeyProp(edge)
	v.f.P("\t\t\t\tconst v = key.value;")
	v.f.P("\t\t\t\tif(v?.", tp, `?.case !== "`, tk.Names().Value, `"){`)
	v.f.P(`					return Promise.reject(this._err("composite query with non-keyed field not supported", Code.Unimplemented))`)
	v.f.P("\t\t\t\t}")
	v.f.P("\t\t\t\tconst q = { '", path, "': ", keyer(tk.Type(), "v?."+tp+"?.value"), " }")
	v.f.P("\t\t\t\treturn this._query(t => t.where(q).first());")
	return struct{}{}, nil
}

func (v *getCaseVisitor) VisitIndex(idx *schema.Index) (struct{}, error) {
	v.f.P("\t\t\t\tconst v = key.value;")

	// Guards first, in member order: an edge can only be addressed by its own
	// key, and anything else is refused at run time rather than mis-queried.
	for _, p := range idx.Props() {
		edge, isEdge := p.(*schema.Edge)
		if !isEdge {
			continue
		}
		tk := targetKey(edge)
		if tk == nil {
			return struct{}{}, fmt.Errorf("ts: %s points at an entity with no key", edge.Name())
		}
		v.f.P("\t\t\t\tif(v.", edge.Names().Value, "?.", v.e.targetKeyProp(edge), `?.case !== "`, tk.Names().Value, `"){`)
		v.f.P(`					return Promise.reject(this._err("composite query with non-keyed field not supported", Code.Unimplemented))`)
		v.f.P("\t\t\t\t}")
	}

	v.f.P("\t\t\t\tconst q = {")
	for _, p := range idx.Props() {
		switch m := p.(type) {
		case *schema.Field:
			name := string(m.Names().Value)
			v.f.P("\t\t\t\t\t'", name, "': v.", name, ",")
		case *schema.Edge:
			tk := targetKey(m)
			name := string(m.Names().Value)
			v.f.P("\t\t\t\t\t'", name, ".", tk.Names().Value, "': ",
				keyer(tk.Type(), "v."+name+"?."+v.e.targetKeyProp(m)+"?.value"), ",")
		}
	}
	v.f.P("\t\t\t\t}")
	v.f.P("\t\t\t\treturn this._query(t => t.where(q).first());")
	return struct{}{}, nil
}

// defaultLabel is the fallback when the Ref message is not in the request.
//
// It is the predecessor's computation, kept only so a schema compiled without
// its service still emits something readable. When the Ref is present — which
// in production it always is — the descriptor answers instead.
func defaultLabel(cand schema.Elem) string {
	if p, ok := cand.(schema.Prop); ok {
		return string(p.Names().Value)
	}
	return toCamel(string(cand.Name()))
}

// toCamel lower-camels an underscore-separated name, matching what
// ettle/strcase.ToCamel does for the names an index can have.
func toCamel(s string) string {
	var out []rune
	upper := false
	for i, r := range s {
		switch {
		case r == '_' || r == '-':
			upper = true
		case upper:
			out = append(out, upperRune(r))
			upper = false
		case i == 0:
			out = append(out, lowerRune(r))
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

// upperRune and lowerRune are ASCII, and target/service has its own copy of the
// first one.
//
// Six duplicated lines, kept: the two callers are toCamel here and pascal
// there, and both carry a comment saying they reproduce a specific generator's
// spelling and must not be replaced by a casing library. A shared helper is the
// first half of that library, and the reason it would be wrong is written down
// twice already.
func upperRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

func lowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// keyer wraps a value in the codec its type needs on the way into the store.
func keyer(t schema.Type, expr string) string {
	if t == schema.TypeUUID {
		return "uuid.u8_str(" + expr + ")"
	}
	return expr
}
