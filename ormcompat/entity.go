package ormcompat

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/heojeongbo/calque/ormopt"
	"github.com/heojeongbo/calque/schema"
)

// isEntity reports whether a message opted in.
//
// A message with no (orm.message) is not an entity and is not an error: most
// messages in most protos are just messages.
func isEntity(md protoreflect.MessageDescriptor) bool {
	if !proto.HasExtension(md.Options(), ormopt.E_Message) {
		return false
	}
	opts, _ := proto.GetExtension(md.Options(), ormopt.E_Message).(*ormopt.MessageOptions)
	return !opts.GetDisabled()
}

// parseEntity turns one annotated message into an entity.
//
// It returns nil only when the entity cannot be built at all. Everything else
// is reported into diags and parsing continues, so one run names every problem
// in the schema rather than the first.
func parseEntity(md protoreflect.MessageDescriptor, generate bool, diags *schema.Diagnostics) *schema.Entity {
	opts, _ := proto.GetExtension(md.Options(), ormopt.E_Message).(*ormopt.MessageOptions)
	at := diags.At(string(md.FullName()))

	props := make([]*parsed, 0, md.Fields().Len())
	for i := range md.Fields().Len() {
		if p := parseProp(md.Fields().Get(i), at); p != nil {
			props = append(props, p)
		}
	}

	key := resolveKey(props, at)

	var (
		fields []*schema.Field
		edges  []*schema.Edge
		keyPtr *schema.Field
	)
	for _, p := range props {
		if p.isEdge {
			edges = append(edges, schema.NewEdge(p.edge))
			continue
		}
		f := schema.NewField(p.field)
		fields = append(fields, f)
		if p == key {
			keyPtr = f
		}
	}

	e := schema.NewEntity(schema.EntitySpec{
		FullName: string(md.FullName()),
		Name:     string(md.Name()),
		Package:  string(md.ParentFile().Package()),
		File:     md.ParentFile().Path(),
		Generate: generate,
		Fields:   fields,
		Edges:    edges,
		Indexes:  parseIndexes(opts, at),
		Rpc:      parseRpc(opts.GetRpc()),
		Source:   md,
	})
	if keyPtr != nil {
		e.SetKey(keyPtr)
	}
	return e
}

// resolveKey finds the one prop marked key and checks that nothing else the
// annotation said contradicts being one.
//
// A key is unique and immutable by definition, so saying so is redundant and
// saying the opposite is a mistake worth naming rather than overruling.
func resolveKey(props []*parsed, at *schema.Diagnostics) *parsed {
	var key *parsed
	for _, p := range props {
		if !p.keyed {
			continue
		}
		name := string(p.field.Names.Proto)

		if key != nil {
			at.Addf("", "there can be only one key, but %s(%d) and %s(%d) are both marked",
				key.field.Names.Proto, key.field.Number, p.field.Names.Proto, p.field.Number)
			continue
		}
		switch {
		case p.fo.HasUnique() && !p.fo.GetUnique():
			at.Add(name, "the key must be unique")
			continue
		case p.fo.HasNullable() && p.fo.GetNullable():
			at.Add(name, "the key cannot be nullable")
			continue
		case p.fo.HasImmutable() && !p.fo.GetImmutable():
			at.Add(name, "the key must be immutable")
			continue
		}

		// Derived rather than required: a key is these things whether or not
		// the annotation bothered to say so.
		p.field.Unique = true
		p.field.Immutable = true
		p.field.Nullable = false
		key = p
	}

	if key == nil {
		at.Hintf("", "no key is defined",
			"exactly one field carries (orm.field) = {key: true}; it is how a row is named")
	}
	return key
}

func parseIndexes(opts *ormopt.MessageOptions, at *schema.Diagnostics) []*schema.Index {
	var out []*schema.Index
	for n, io := range opts.GetIndexes() {
		if io.GetDisabled() {
			continue
		}
		where := fmt.Sprintf("{indexes}[%d](%s)", n, io.GetName())

		if io.GetName() == "" {
			at.Add(where, "an index must be named")
			continue
		}

		refs := make([]schema.Ref, 0, len(io.GetRefs()))
		for _, r := range io.GetRefs() {
			refs = append(refs, schema.Ref{
				Name:   schema.ProtoName(r.GetName()),
				Number: r.GetNumber(),
			})
		}

		// includes_erased is refused rather than ignored where it says nothing:
		// a declaration that has no effect is one somebody wrote for a reason
		// that is not going to happen.
		if io.GetIncludesErased() && !io.GetUnique() {
			at.Add(where, "includes_erased says nothing about an index that is not unique")
			continue
		}

		out = append(out, schema.NewIndex(schema.IndexSpec{
			Name:           schema.ProtoName(io.GetName()),
			Refs:           refs,
			Unique:         io.GetUnique(),
			Immutable:      io.GetImmutable(),
			Hidden:         io.GetHidden(),
			IncludesErased: io.GetIncludesErased(),
		}))
	}
	return out
}

// parseRpc reads the declared operation set.
//
// crud enables all four; an individual operation can then be turned off. The
// set is what the generator walks, so an operation that is on here is one that
// gets generated -- there is no way for a target to quietly emit fewer.
func parseRpc(o *ormopt.RpcOptions) schema.RpcSet {
	set := schema.RpcSet{}
	if o == nil || o.GetDisabled() {
		return set
	}

	crud := o.GetCrud()
	set[schema.OpAdd] = enabled(crud, o.HasAdd(), o.GetAdd().GetDisabled())
	set[schema.OpGet] = enabled(crud, o.HasGet(), o.GetGet().GetDisabled())
	set[schema.OpPatch] = enabled(crud, o.HasPatch(), o.GetPatch().GetDisabled())
	set[schema.OpErase] = enabled(crud, o.HasErase(), o.GetErase().GetDisabled())
	return set
}

// enabled decides one operation: crud turns everything on, naming an operation
// turns that one on, and either can be taken back by disabling it.
func enabled(crud, named, disabled bool) bool {
	if disabled {
		return false
	}
	return crud || named
}
