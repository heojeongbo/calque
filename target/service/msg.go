package service

import (
	"fmt"
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/heojeongbo/calque/internal/prow"
	"github.com/heojeongbo/calque/schema"
)

// The six messages an entity's contract is made of, each in its own function so
// that this file reads against the specification rather than against a loop.
//
// Field numbers are not chosen here so much as derived, and each derivation is a
// promise to code somebody else wrote:
//
//	AddRequest, Select   the entity's own numbers
//	GetRequest           ref = 1, select = 2
//	PatchRequest         2n-1 for the value, 2n for its companion
//	Ref                  each lookup's number; an index reports its first member's
//
// Nothing may renumber. The consuming deployment has a stored audit log naming
// generated methods, a browser cache keyed on the index shape, and hand-written
// code reading `date_updated_force` by name.

// emitAdd writes <Entity>AddRequest: everything a row can be created with.
func (t *Target) emitAdd(f *prow.File, e *entity) {
	f.Block("message "+e.def.Name()+"AddRequest", func() {
		for _, p := range e.def.Props() {
			// The version field is the store's to set, so there is nothing for a
			// caller to say about it.
			//
			// The soft-delete stamp is not treated the same way, deliberately. The
			// generator being replaced has no `erased` in its vocabulary at all and
			// emits that field as the ordinary nullable timestamp it looks like, and
			// nothing downstream in calque implements soft delete yet either -- so
			// leaving it in is the choice that makes no claim, and the bytes are the
			// ones the previous generator would have written. See
			// docs/targets/service.md.
			if fld, ok := p.(*schema.Field); ok && fld.IsVersion() {
				continue
			}
			fd := descriptorOf(p)
			f.Field(label(fd), e.addType(p), string(p.Name()), p.Number(), addOpts(p, fd)...)
		}
	})
}

// addType is <Target>Ref for an edge and the prop's own type for a field.
//
// A message-typed field with no (orm.edge) is a field, not an edge, so it keeps
// its own type -- which is why a contract can carry both `TenantRef tenant = 2`
// and `Tenant tenant = 2` depending on one annotation.
func (e *entity) addType(p schema.Prop) string {
	if edge, ok := p.(*schema.Edge); ok {
		return edge.Target().Name() + "Ref"
	}
	return protoType(descriptorOf(p), e.pkg)
}

// addOpts is the one field option this contract ever carries.
//
// It restates implicit presence on a field that has none, which matters because
// the generated file does not repeat the source's file-level
// `features.field_presence = IMPLICIT`: without it the field would gain presence
// it does not have and an unset value would stop being the default.
//
// A message never has it (presence is inherent), and neither does anything
// optional -- nullable, or defaulted, or repeated -- because those want presence.
func addOpts(p schema.Prop, fd protoreflect.FieldDescriptor) []string {
	if fd.IsList() || fd.IsMap() || p.IsOptional() {
		return nil
	}
	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		return nil
	}
	return []string{"features.field_presence = IMPLICIT"}
}

// emitRefBy writes one <Entity>RefBy<Index> wrapper.
//
// Members are in the order the `refs:` were written, not in field-number order.
// That is a real difference -- a slug index is declared alias(4) then tenant(2) --
// and it is the annotation's order because the annotation is where a composite key
// gets its meaning.
func (t *Target) emitRefBy(f *prow.File, e *entity, idx *schema.Index) {
	f.Block("message "+e.refByName(idx), func() {
		for _, p := range idx.Props() {
			fd := descriptorOf(p)
			f.Field(label(fd), e.addType(p), string(p.Name()), p.Number())
		}
	})
}

// refByName is RefBy plus the index's name in PascalCase.
//
// The casing is a plain word-split on separators with no initialism handling, so
// an index named `by_email` becomes `RefByByEmail`. That is not the rule
// internal/entname implements for Go identifiers, and the two must not be
// conflated: this one names a proto message.
func (e *entity) refByName(idx *schema.Index) string {
	return e.def.Name() + "RefBy" + pascal(string(idx.Name()))
}

// emitRef writes <Entity>Ref: the ways a row can be named.
//
// The oneof members are in ascending field number, which is not the order Keys()
// hands them over -- that is every unique prop and then every unique index, so an
// entity with a unique field numbered after an index's first member comes out
// differently. Both orders are live in the consuming deployment and they govern
// different things: this one is the wire contract, and Keys() order is what the
// generated query code and the stored browser cache use. Neither may be changed to
// match the other. See docs/conventions.md.
func (t *Target) emitRef(f *prow.File, e *entity) error {
	type member struct {
		name   string
		typ    string
		number int32
	}

	var ms []member
	for _, k := range e.def.Keys() {
		switch key := k.(type) {
		case *schema.Field:
			ms = append(ms, member{string(key.Name()), protoType(key.Source(), e.pkg), key.Number()})
		case *schema.Index:
			ms = append(ms, member{string(key.Name()), e.refByName(key), key.Number()})
		case *schema.Edge:
			// The generator being replaced panics here -- twice, over the same
			// three-variant union, and one of them still does. So there are no
			// bytes to match and the shape is decided by what already reads it:
			// calque's TypeScript target unpacks this case as a Ref of the edge's
			// target (`v?.key?.case`), so that is what the member holds. No
			// wrapper, because a single edge needs no composite.
			ms = append(ms, member{string(key.Name()), key.Target().Name() + "Ref", key.Number()})
		}
	}
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].number < ms[j].number })

	f.Block("message "+e.def.Name()+"Ref", func() {
		f.Block("oneof key", func() {
			for _, m := range ms {
				f.Field("", m.typ, m.name, m.number)
			}
		})
	})
	return nil
}

// emitSelect writes <Entity>Select: which columns a get should fill in.
//
// Every prop appears, version and immutable ones included -- reading is not
// writing. The key's slot is taken by `all`, in place and at the key's own number,
// so the key's name never appears and `all = 1` is not a coincidence of the key
// being field 1.
func (t *Target) emitSelect(f *prow.File, e *entity) {
	key := e.def.Key()

	f.Block("message "+e.def.Name()+"Select", func() {
		for _, p := range e.def.Props() {
			name := string(p.Name())
			typ := "bool"
			if p.Number() == key.Number() {
				name = "all"
			}
			if edge, ok := p.(*schema.Edge); ok {
				typ = edge.Target().Name() + "Select"
			}
			f.Field("", typ, name, p.Number())
		}
	})
}

// emitGet writes <Entity>GetRequest. Two fields, both numbered by fiat.
func (t *Target) emitGet(f *prow.File, e *entity) {
	name := e.def.Name()
	f.Block("message "+name+"GetRequest", func() {
		f.Field("", name+"Ref", "ref", 1)
		f.Field("", name+"Select", "select", 2)
	})
}

// emitPatch writes <Entity>PatchRequest, where the numbering is the interesting
// part.
//
// A prop at n gets 2n-1, and 2n is its companion: `<prop>_null` to mean "set this
// to nothing", or `<version>_force` to mean "overwrite regardless". proto cannot
// tell an absent field from one set to its zero value, so a patch needs the second
// bit; see docs/conventions.md.
//
// `ref` takes the key's own number, which is 2k-1 for the key -- available because
// the key is immutable by construction and therefore never patched.
func (t *Target) emitPatch(f *prow.File, e *entity) error {
	key := e.def.Key()
	version := e.def.Version()

	f.Block("message "+e.def.Name()+"PatchRequest", func() {
		f.Field("", e.def.Name()+"Ref", "ref", key.Number())

		for _, p := range e.def.Props() {
			if p.IsImmutable() {
				continue
			}
			fd := descriptorOf(p)
			n := p.Number()
			f.Field(label(fd), e.addType(p), string(p.Name()), n*2-1)

			switch {
			case version != nil && p.Number() == version.Number():
				f.Field("", "bool", string(p.Name())+"_force", n*2)
			case p.IsNullable():
				f.Field("", "bool", string(p.Name())+"_null", n*2)
			}
		}
	})

	// The doubling is unchecked upstream and cannot be checked afterwards: a
	// number in proto's reserved range is refused by protoc, and one past the
	// maximum is refused by nothing. Both are silent about *why* if the schema is
	// not the thing being read.
	for _, p := range e.def.Props() {
		if p.IsImmutable() {
			continue
		}
		if n := int64(p.Number()) * 2; n >= 19000 && n <= 19999 {
			return fmt.Errorf("%s: %s is field %d, and a patch numbers its companion %d, which is inside proto's reserved range 19000-19999\n"+
				"  renumber the prop below 9500",
				e.def.FullName(), p.Name(), p.Number(), n)
		} else if n > 536870911 {
			return fmt.Errorf("%s: %s is field %d, and a patch numbers its companion %d, past the largest field number proto allows",
				e.def.FullName(), p.Name(), p.Number(), n)
		}
	}
	return nil
}

// emitService writes the service, whose comments are as fixed as its method names.
//
// Add/Get/Patch/Erase and nothing else: List, Watch and Scrape are not in the
// annotation vocabulary and were never generated, so a contract that has them got
// them by hand.
func (t *Target) emitService(f *prow.File, e *entity, empty func() string) {
	name := e.def.Name()

	f.Block("service "+name+"Service", func() {
		if e.def.Rpc().Has(schema.OpAdd) {
			f.Comment("Add creates a new " + name)
			f.P("rpc Add(", name, "AddRequest) returns (", name, ");")
		}
		if e.def.Rpc().Has(schema.OpGet) {
			f.Comment(article("Get retrieves", name) + name)
			f.P("rpc Get(", name, "GetRequest) returns (", name, ");")
		}
		if e.def.Rpc().Has(schema.OpPatch) {
			f.Comment("Patch updates an existing " + name)
			f.P("rpc Patch(", name, "PatchRequest) returns (", name, ");")
		}
		if e.def.Rpc().Has(schema.OpErase) {
			f.Comment(article("Erase deletes", name) + name)
			f.P("rpc Erase(", name, "Ref) returns (", empty(), ");")
		}
	})
}

// article picks "a" or "an".
//
// The generator being replaced wrote "a" unconditionally, so an entity named Env or
// Audit got "Get retrieves a Env". Fixed here, because it is a comment -- no
// descriptor moves and no generated code changes -- and leaving it would mean
// writing the mistake deliberately every time a vowel-initial entity is added.
//
// U is excluded on purpose. The article follows the sound and not the letter, and
// U is the one where they part company often enough to matter: "a User" and "a
// Unit" are right, "an Umbrella" is too, and nothing in a name says which. The
// four letters that are left are unambiguous, and "a User" is also what the
// previous generator wrote, so the entity most likely to exist does not move.
func article(verb, name string) string {
	if name == "" {
		return verb + " a "
	}
	switch name[0] {
	case 'A', 'E', 'I', 'O':
		return verb + " an "
	}
	return verb + " a "
}

// pascal upper-cases the first letter of each separator-delimited word and leaves
// everything else alone.
//
// It reproduces ettle/strcase.ToPascal for the shape an index name actually has --
// lower-case, underscore-separated -- which is every one in the corpus this target
// was measured against. That function also splits on case transitions; for these
// inputs the two agree, and for a mixed-case index name they may not, which is why
// this says what it does rather than claiming to be a casing library.
//
// Deliberately not internal/entname: that folds initialisms for Go identifiers,
// and this names a proto message that hand-written code already refers to. An
// index called `by_email` produces `RefByByEmail`, which looks wrong and is the
// name that exists.
func pascal(s string) string {
	var out []rune
	upper := true
	for _, r := range s {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ':
			upper = true
		case upper:
			out = append(out, upperRune(r))
			upper = false
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func upperRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}
