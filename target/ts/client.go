package ts

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/tsw"
	"github.com/heojeongbo/calque/schema"
)

// emitClient writes client.g.ts: the per-service client types, the method bags,
// and the `queries` table.
//
// It is emitted for every generate-marked package, including ones with no
// services at all — those come out as a header, two imports and two empty
// declarations, which is what the predecessor produces and therefore what the
// diff expects.
func (t *Target) emitClient(g *gen.Generator, p *pkg) ([]byte, error) {
	services := orderedServices(p)

	f := tsw.New()
	gen.Preamble(f, t.opts.Header, "")
	f.P()
	f.P(`import type { Client as C } from "@connectrpc/connect";`)
	f.P(`import type { QueryDescOf } from "`, t.opts.Runtime, `";`)
	f.P()

	// The blank lines are unconditional. A package with no services still gets
	// all of them, which is why a package with no entities ends up with three
	// blank lines in a row --
	// and a diff is a diff.
	for _, svc := range services {
		f.P(`import { `, svc.Name(), ` } from "./`, pbModule(svc.ParentFile().Path()), t.opts.ImportExtension, `";`)
	}
	f.P()

	for _, svc := range services {
		f.P(`export type `, svc.Name(), `Client = C<typeof `, svc.Name(), `>`)
	}
	f.P()

	f.P(`export interface ServiceClient {`)
	for _, svc := range services {
		f.P("\treadonly ", clientKey(svc), ": ", svc.Name(), "Client;")
	}
	f.P(`}`)
	f.P()

	for _, svc := range services {
		// No space before "=". That is what the predecessor emits.
		f.P(`export const `, clientKey(svc), `= `, svc.Name(), `.method;`)
	}
	f.P()

	// queries is keyed by service and ordered by service, not by entity. The two
	// orders differ -- the committed output starts with AuditService while the
	// entities start with Tenant -- so walking the wrong one reorders the whole
	// table.
	f.P(`export const queries = {`)
	for _, svc := range services {
		for _, e := range p.entities {
			if e.service == svc {
				t.emitQueryDesc(g, f, e)
				break
			}
		}
	}
	f.P(`}`)
	f.P()

	return f.Bytes(), nil
}

// clientKey is the property a service is reached through.
//
// It is tsw.LowerFirst — protoc-gen-orm-ts's `camel` — and not a correct
// camelCase. "BTExecutorService" becomes "bTExecutor", which is the spelling
// 265 call sites and a hand-written ServiceClient in the consuming application
// depend on. Fixing it here would break all of them.
func clientKey(svc protoreflect.ServiceDescriptor) string {
	return tsw.LowerFirst(strings.TrimSuffix(string(svc.Name()), "Service"))
}

// emitQueryDesc writes one entity's entry in the `queries` table.
//
// `pick` is the entity's canonical ref, `refs` is every ref it can be found by,
// and `rpc[m].extract` pulls entities out of a response. Together they are what
// a normalized cache needs: harvest the entities from any reply, and know every
// key each one is cached under so a write can invalidate all of them.
func (t *Target) emitQueryDesc(g *gen.Generator, f *tsw.File, e *entity) {
	def := e.def
	svc := e.service
	key := def.Key()

	f.P("\t[\"", svc.FullName(), "\"]: {")

	// pick: the primary key, always.
	label, ok := caseLabel(e.ref, key)
	if !ok {
		label = string(key.Names().Value)
	}
	f.P("\t\tpick: v => ({", e.keyProp, ":{case: \"", label, "\", value: v.", key.Names().Value, "}}),")

	f.P("\t\trefs: v => [")
	for _, cand := range def.Keys() {
		t.emitRef(f, e, cand)
	}
	f.P("\t\t],")

	f.P("\t\trpc: {")
	methods := svc.Methods()
	for i := range methods.Len() {
		m := methods.Get(i)
		name := tsw.LowerFirst(string(m.Name()))
		f.P("\t\t\t", name, ": {")
		f.P("\t\t\t\tdesc: ", clientKey(svc), ".", name, ",")
		f.P("\t\t\t\textract: v => ", extractor(def, m))
		f.P("\t\t\t},")
	}
	f.P("\t\t}")

	f.P("\t} satisfies QueryDescOf<typeof ", svc.Name(), ">,")

	// The consuming cache derives the table name by stripping "Service" from
	// the service's type name and indexing DbService with it. Nothing enforces
	// that coincidence, so say when it stops holding.
	if want := strings.TrimSuffix(string(svc.FullName()), "Service"); want != def.FullName() {
		g.Warnf("%s: the cache finds this table by stripping \"Service\" from %q, which gives %q — they no longer match",
			def.FullName(), svc.FullName(), want)
	}
}

// emitRef writes one entry of the refs array.
func (t *Target) emitRef(f *tsw.File, e *entity, cand schema.Elem) {
	label, ok := caseLabel(e.ref, cand)
	if !ok {
		label = defaultLabel(cand)
	}

	switch k := cand.(type) {
	case *schema.Field:
		f.P("\t\t\t{", e.keyProp, ":{case: \"", label, "\", value: v.", k.Names().Value, "}},")

	case *schema.Edge:
		tk := targetKey(k)
		if tk == nil {
			return
		}
		// The inner one is the *target* entity's Ref, a different message that
		// may spell its oneof differently.
		f.P("\t\t\t{", e.keyProp, ":{case: \"", label, "\", value: v.", k.Names().Value,
			" && {", e.targetKeyProp(k), ":{case: \"", tk.Names().Value, "\", value: v.", k.Names().Value, ".", tk.Names().Value, "}}}},")

	case *schema.Index:
		f.P("\t\t\t{", e.keyProp, ":{")
		f.P("\t\t\t\tcase: \"", label, "\",")
		f.P("\t\t\t\tvalue: {")
		for _, p := range k.Props() {
			switch m := p.(type) {
			case *schema.Field:
				f.P("\t\t\t\t\t", m.Names().Value, ": v.", m.Names().Value, ",")
			case *schema.Edge:
				tk := targetKey(m)
				if tk == nil {
					continue
				}
				f.P("\t\t\t\t\t", m.Names().Value, ": v.", m.Names().Value,
					" && {", e.targetKeyProp(m), ":{case: \"", tk.Names().Value, "\", value: v.", m.Names().Value, ".", tk.Names().Value, "}},")
			}
		}
		f.P("\t\t\t\t}")
		f.P("\t\t\t}},")
	}
}

// extractor decides how an entity is pulled out of one method's response.
//
// Three cases, in order: the response is the entity; the response has a field
// whose type is the entity, which is how `list` becomes `v.items`; or the
// method produces no entity at all.
func extractor(e *schema.Entity, m protoreflect.MethodDescriptor) string {
	out := m.Output()
	if string(out.FullName()) == e.FullName() {
		return "v"
	}

	fields := out.Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		if f.Kind() != protoreflect.MessageKind || f.IsMap() {
			continue
		}
		if string(f.Message().FullName()) == e.FullName() {
			return "v." + f.JSONName()
		}
	}
	return "undefined"
}
