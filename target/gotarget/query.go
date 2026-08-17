package gotarget

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/heojeongbo/calque/schema"
)

var (
	bytesPkg     = protogen.GoImportPath("bytes")
	protojsonPkg = protogen.GoImportPath("google.golang.org/protobuf/encoding/protojson")
)

// emitQuery writes query.g.go: how a message names itself, and how a name is
// turned back into a request.
//
// Every identifier in it comes from a descriptor rather than from a casing
// function, and the reason is visible in the output. `SessionByTokenHash` takes
// the field's Go name, `StationByDevice(device_id string)` takes the index's Go
// name for the function and the member's *proto* name for the parameter, and
// `StationRef_Device_case` is protoc-gen-go's own constant. Three spellings of
// one thing in one line, and only one of them could be guessed.
func (e *emitter) emitQuery() error {
	// Everything here is written in terms of the Ref message, and a Ref message
	// is part of the service contract. An entity nothing serves does not have
	// one -- it is still modelled, and still a valid edge target, but there is
	// no name for a caller to look it up by and so nothing to write.
	var served []*schema.Entity
	for _, en := range e.gen.Sources() {
		if _, ok := e.service(en); ok {
			served = append(served, en)
		}
	}
	if len(served) == 0 {
		return nil
	}

	f, ok := e.file(served[0])
	if !ok {
		return fmt.Errorf("go: %s: no protogen file", served[0].FullName())
	}
	dir := string(f.GoImportPath)
	gf := e.pg.NewGeneratedFile(dir+"/query.g.go", f.GoImportPath)
	gf.P(e.target.opts.QueryHeader)
	gf.P()
	gf.P("package ", lastSegment(dir))
	gf.P()

	for _, en := range served {
		if err := e.emitEntityQuery(gf, en); err != nil {
			return fmt.Errorf("go: %s: %w", en.FullName(), err)
		}
	}
	return nil
}

// refShape is one entity's Ref message, resolved once and read many times.
type refShape struct {
	ref    *protogen.Message // <E>Ref
	get    *protogen.Message // <E>GetRequest
	oneof  *protogen.Oneof   // the `key` oneof on <E>Ref
	msg    *protogen.Message // <E>
	refIn  *protogen.Field   // the field on <E>GetRequest that holds a <E>Ref
	selIn  *protogen.Field   // the field on <E>GetRequest that holds a <E>Select
	sel    *protogen.Message // <E>Select
	keys   []refKey
	entity *schema.Entity
}

// refKey is one way a row can be named: one member of the Ref's oneof, paired
// with the schema element it came from.
type refKey struct {
	field *protogen.Field // the oneof member on <E>Ref
	elem  schema.Elem     // what the schema calls it

	// wrapper is the message the oneof member holds, for a key that is not a
	// plain field. An edge and an index are the same shape here: both get a
	// <E>RefBy<X> of their own, even when the edge is the only member.
	wrapper *protogen.Message
	members []*queryMember
}

// queryMember is one component of a wrapped key.
type queryMember struct {
	prop  schema.Prop
	on    *protogen.Field // the field on the wrapper message
	owned *protogen.Field // the same prop as a field on the entity
}

func (e *emitter) shapeOf(en *schema.Entity) (*refShape, error) {
	full := protoreflect.FullName(en.FullName())
	msg, ok := e.message(full)
	if !ok {
		return nil, fmt.Errorf("no Go type")
	}
	ref, ok := e.message(full + "Ref")
	if !ok {
		return nil, fmt.Errorf("no %sRef message; calque reads the lookup names off it rather than computing them", en.Name())
	}
	get, ok := e.message(full + "GetRequest")
	if !ok {
		return nil, fmt.Errorf("no %sGetRequest message", en.Name())
	}

	s := &refShape{ref: ref, get: get, msg: msg, entity: en}

	for _, f := range get.Fields {
		if f.Message == nil {
			continue
		}
		switch f.Message.Desc.FullName() {
		case ref.Desc.FullName():
			s.refIn = f
		case full + "Select":
			s.selIn = f
			s.sel = f.Message
		}
	}
	if s.refIn == nil {
		return nil, fmt.Errorf("%sGetRequest has no %sRef field", en.Name(), en.Name())
	}

	// Exactly one, and its name is not read. This target used to take the first
	// of however many there were while the TypeScript target asked for one named
	// `key` -- two rules for one message, and a schema that satisfied only one of
	// them got a silently different answer from each. One oneof is unambiguous
	// whatever it is called; two are a schema neither target can read, so both
	// now say so instead of picking.
	if len(ref.Oneofs) != 1 {
		return nil, fmt.Errorf("%sRef has %d oneofs; it needs exactly one (its name does not matter), because that is where the lookups are",
			en.Name(), len(ref.Oneofs))
	}
	s.oneof = ref.Oneofs[0]

	// The schema decides the order and the descriptor decides the names, and
	// the two disagree -- which is worth stating, because following the wrong
	// one produces a file that compiles and is not the same file.
	//
	// The Ref message orders its oneof by field number, counting an index as
	// its first member's. The generator orders by what a key *is*: every prop,
	// then every index. An entity with a unique field numbered after an index's
	// first member comes out in a different order from each. Credential is
	// exactly that shape -- id(1), the holder index(2), username(3) in the Ref,
	// and id, username, holder here.
	for _, elem := range en.Keys() {
		f := oneofFieldNamed(s.oneof, elem.Name())
		if f == nil {
			return nil, fmt.Errorf("%sRef has no %s in its %s oneof, but the schema says a row can be found by it",
				en.Name(), elem.Name(), s.oneof.Desc.Name())
		}
		key := refKey{field: f, elem: elem}

		if f.Message != nil {
			key.wrapper = f.Message
			props, err := membersOf(key.elem)
			if err != nil {
				return nil, err
			}
			for _, p := range props {
				on := fieldNamed(f.Message, p.Name())
				if on == nil {
					return nil, fmt.Errorf("%s has no %s field", f.Message.GoIdent.GoName, p.Name())
				}
				owned := fieldNamed(msg, p.Name())
				if owned == nil {
					return nil, fmt.Errorf("%s has no %s field", en.Name(), p.Name())
				}
				key.members = append(key.members, &queryMember{prop: p, on: on, owned: owned})
			}
		}
		s.keys = append(s.keys, key)
	}
	return s, nil
}

// oneofFieldNamed finds a Ref's oneof member by the name the schema knows it by.
func oneofFieldNamed(o *protogen.Oneof, name schema.ProtoName) *protogen.Field {
	for _, f := range o.Fields {
		if schema.ProtoName(f.Desc.Name()) == name {
			return f
		}
	}
	return nil
}

// membersOf is what a wrapped key is made of.
//
// An edge is its own single member, which is why an edge and a one-member index
// emit the same code: the generator this reproduces made no distinction, and
// the Ref message it reads does not either.
func membersOf(elem schema.Elem) ([]schema.Prop, error) {
	return schema.VisitElem[[]schema.Prop](elem, memberVisitor{})
}

type memberVisitor struct{}

func (memberVisitor) VisitField(f *schema.Field) ([]schema.Prop, error) {
	return []schema.Prop{f}, nil
}
func (memberVisitor) VisitEdge(e *schema.Edge) ([]schema.Prop, error) {
	return []schema.Prop{e}, nil
}
func (memberVisitor) VisitIndex(i *schema.Index) ([]schema.Prop, error) {
	return i.Props(), nil
}

func fieldNamed(m *protogen.Message, name schema.ProtoName) *protogen.Field {
	for _, f := range m.Fields {
		if schema.ProtoName(f.Desc.Name()) == name {
			return f
		}
	}
	return nil
}

func (e *emitter) emitEntityQuery(gf *protogen.GeneratedFile, en *schema.Entity) error {
	s, err := e.shapeOf(en)
	if err != nil {
		return err
	}

	msg := gf.QualifiedGoIdent(s.msg.GoIdent)
	ref := gf.QualifiedGoIdent(s.ref.GoIdent)
	get := gf.QualifiedGoIdent(s.get.GoIdent)
	builder := s.get.GoIdent.GoName + "_builder"

	gf.P("func (x *", ref, ") Pick() *", get, " {")
	gf.P("\treturn ", builder, "{", s.refIn.GoName, ": x}.Build()")
	gf.P("}")
	gf.P()

	if err := e.emitRef(gf, s, msg, ref); err != nil {
		return err
	}

	gf.P("func (x *", msg, ") Pick() *", get, " {")
	gf.P("\treturn x.Ref().Pick()")
	gf.P("}")
	gf.P()

	if err := e.emitPicks(gf, s, msg, ref); err != nil {
		return err
	}

	if s.selIn != nil {
		sel := gf.QualifiedGoIdent(s.sel.GoIdent)
		gf.P("func (x *", get, ") With", s.selIn.GoName, "(f func(s *", sel, ")) *", get, " {")
		gf.P("\tif !x.Has", s.selIn.GoName, "() {")
		gf.P("\t\tx.Set", s.selIn.GoName, "(&", sel, "{})")
		gf.P("\t}")
		gf.P("\tf(x.Get", s.selIn.GoName, "())")
		gf.P("\treturn x")
		gf.P("}")
		gf.P()
	}

	marshal := gf.QualifiedGoIdent(protojsonPkg.Ident("Marshal"))
	unmarshal := gf.QualifiedGoIdent(protojsonPkg.Ident("Unmarshal"))
	gf.P("func (x *", msg, ") MarshalJSON() ([]byte, error) { return ", marshal, "(x) }")
	gf.P("func (x *", msg, ") UnmarshalJSON(b []byte) error { return ", unmarshal, "(b, x) }")
	gf.P()

	return e.emitConstructors(gf, s, ref, get, builder)
}

// emitRef writes the method that asks a message what it is called.
//
// A plain field is an `if` with an initialiser. A wrapped key is a braced block
// with numbered locals, because it has to read every member before it can
// decide whether it has all of them -- and the block keeps v1, v2 from
// colliding with the next key's.
func (e *emitter) emitRef(gf *protogen.GeneratedFile, s *refShape, msg, ref string) error {
	gf.P("func (x *", msg, ") Ref() *", ref, " {")

	for _, key := range s.keys {
		ctor := s.entity.Name() + "By" + key.field.GoName

		if key.wrapper == nil {
			owned := fieldNamed(s.msg, schema.ProtoName(key.field.Desc.Name()))
			if owned == nil {
				return fmt.Errorf("%s has no %s field", s.entity.Name(), key.field.Desc.Name())
			}
			gf.P("\tif v := x.Get", owned.GoName, "(); ", present("v", owned), " {")
			gf.P("\t\treturn ", ctor, "(v)")
			gf.P("\t}")
			continue
		}

		gf.P("\t{")
		var conds, args string
		for i, m := range key.members {
			local := fmt.Sprintf("v%d", i+1)
			gf.P("\t\t", local, " := x.Get", m.owned.GoName, "()")
			if i > 0 {
				conds += " && "
				args += ", "
			}
			conds += present(local, m.owned)
			if _, isEdge := m.prop.(*schema.Edge); isEdge {
				args += local + ".Ref()"
			} else {
				args += local
			}
		}
		gf.P("\t\tif ", conds, " {")
		gf.P("\t\t\treturn ", ctor, "(", args, ")")
		gf.P("\t\t}")
		gf.P("\t}")
	}

	gf.P()
	gf.P("\treturn nil")
	gf.P("}")
	gf.P()
	return nil
}

// present is the test for "this value was set".
//
// It is a shape test rather than a presence test on purpose: the generator this
// reproduces reads the value and asks whether it is empty, so a key explicitly
// set to its zero value is treated as absent. That is a real difference from
// proto presence and it is preserved, because a Ref that started answering
// differently would change which lookup a message resolves to.
func present(local string, f *protogen.Field) string {
	switch f.Desc.Kind() {
	case protoreflect.BytesKind, protoreflect.StringKind:
		return "len(" + local + ") > 0"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return local + " != nil"
	case protoreflect.BoolKind:
		return local
	default:
		return local + " != 0"
	}
}

// emitPicks writes the method that asks whether a name names a given message.
func (e *emitter) emitPicks(gf *protogen.GeneratedFile, s *refShape, msg, ref string) error {
	gf.P("func (x *", ref, ") Picks(v *", msg, ") bool {")
	gf.P("\tswitch x.Which", s.oneof.GoName, "() {")

	for _, key := range s.keys {
		gf.P("\tcase ", s.ref.GoIdent.GoName, "_", key.field.GoName, "_case:")

		if key.wrapper == nil {
			owned := fieldNamed(s.msg, schema.ProtoName(key.field.Desc.Name()))
			if owned == nil {
				return fmt.Errorf("%s has no %s field", s.entity.Name(), key.field.Desc.Name())
			}
			gf.P("\t\treturn ", e.equal(gf, owned, "x.Get"+owned.GoName+"()", "v.Get"+owned.GoName+"()"))
			continue
		}

		// The inner x shadows the outer one. It is what the generator being
		// reproduced emits, and it reads badly enough to be worth naming: from
		// here down, x is the wrapper and v is still the message.
		gf.P("\t\tx := x.Get", key.field.GoName, "()")
		for i, m := range key.members {
			line := "(" + e.equal(gf, m.owned, "x.Get"+m.on.GoName+"()", "v.Get"+m.owned.GoName+"()") + ")"
			switch {
			case len(key.members) == 1:
				gf.P("\t\treturn ", line)
			case i == 0:
				gf.P("\t\treturn ", line, " &&")
			case i == len(key.members)-1:
				gf.P("\t\t\t", line)
			default:
				gf.P("\t\t\t", line, " &&")
			}
		}
	}

	gf.P("\tdefault:")
	gf.P("\t\treturn false")
	gf.P("\t}")
	gf.P("}")
	gf.P()
	return nil
}

// equal compares one prop on a name against the same prop on a message.
func (e *emitter) equal(gf *protogen.GeneratedFile, f *protogen.Field, lhs, rhs string) string {
	switch f.Desc.Kind() {
	case protoreflect.BytesKind:
		return gf.QualifiedGoIdent(bytesPkg.Ident("Equal")) + "(" + lhs + ", " + rhs + ")"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		// An edge compares by name, recursively: two references are the same
		// reference when each names the other's target.
		return lhs + ".Picks(" + rhs + ")"
	default:
		return lhs + " == " + rhs
	}
}

// emitConstructors writes XByY and XGetByY.
//
// The grouping is not what it looks like. Plain fields come first in two
// passes -- every By, then every Get -- and each wrapped key follows as its own
// By/Get pair. Tenant makes it visible (ById, ByAlias, GetById, GetByAlias) and
// Credential makes it unambiguous (ById, ByUsername, GetById, GetByUsername,
// ByHolder, GetByHolder).
func (e *emitter) emitConstructors(gf *protogen.GeneratedFile, s *refShape, ref, get, builder string) error {
	var plain []refKey
	for _, key := range s.keys {
		if key.wrapper == nil {
			plain = append(plain, key)
		}
	}

	for _, key := range plain {
		owned := fieldNamed(s.msg, schema.ProtoName(key.field.Desc.Name()))
		typ, err := e.goType(gf, owned)
		if err != nil {
			return err
		}
		gf.P("func ", s.entity.Name(), "By", key.field.GoName, "(v ", typ, ") *", ref, " {")
		gf.P("\tx := &", ref, "{}")
		gf.P("\tx.Set", key.field.GoName, "(v)")
		gf.P("\treturn x")
		gf.P("}")
		gf.P()
	}
	for _, key := range plain {
		owned := fieldNamed(s.msg, schema.ProtoName(key.field.Desc.Name()))
		typ, err := e.goType(gf, owned)
		if err != nil {
			return err
		}
		name := s.entity.Name() + "By" + key.field.GoName
		gf.P("func ", s.entity.Name(), "Get", "By", key.field.GoName, "(v ", typ, ") *", get, " {")
		gf.P("\treturn ", builder, "{", s.refIn.GoName, ": ", name, "(v)}.Build()")
		gf.P("}")
		gf.P()
	}

	for _, key := range s.keys {
		if key.wrapper == nil {
			continue
		}
		params, args, err := e.paramsOf(gf, key)
		if err != nil {
			return err
		}
		wrapper := gf.QualifiedGoIdent(key.wrapper.GoIdent)
		name := s.entity.Name() + "By" + key.field.GoName

		gf.P("func ", name, "(", params, ") *", ref, " {")
		gf.P("\tx := &", wrapper, "{}")
		for _, m := range key.members {
			gf.P("\tx.Set", m.on.GoName, "(", string(m.prop.Name()), ")")
		}
		gf.P("\treturn ", s.ref.GoIdent.GoName, "_builder{", key.field.GoName, ": x}.Build()")
		gf.P("}")
		gf.P()

		gf.P("func ", s.entity.Name(), "Get", "By", key.field.GoName, "(", params, ") *", get, " {")
		gf.P("\treturn ", builder, "{", s.refIn.GoName, ": ", name, "(", args, ")}.Build()")
		gf.P("}")
		gf.P()
	}
	return nil
}

// paramsOf is a wrapped key's parameter list, and the same names again as
// arguments.
//
// The parameter names are the members' *proto* names, verbatim -- which is how
// `StationByDevice(device_id string)` ends up with an underscore in a Go
// identifier. It is not a mistake worth fixing here: the names are part of the
// signature, and callers wrote them.
func (e *emitter) paramsOf(gf *protogen.GeneratedFile, key refKey) (params, args string, err error) {
	for i, m := range key.members {
		if i > 0 {
			params += ", "
			args += ", "
		}
		name := string(m.prop.Name())
		var typ string
		if edge, ok := m.prop.(*schema.Edge); ok {
			target, ok := e.message(protoreflect.FullName(edge.Target().FullName()) + "Ref")
			if !ok {
				return "", "", fmt.Errorf("no %sRef message for edge %s", edge.Target().Name(), name)
			}
			typ = "*" + gf.QualifiedGoIdent(target.GoIdent)
		} else {
			typ, err = e.goType(gf, m.owned)
			if err != nil {
				return "", "", err
			}
		}
		params += name + " " + typ
		args += name
	}
	return params, args, nil
}

// goType is the Go type protoc-gen-go gives a scalar field.
func (e *emitter) goType(gf *protogen.GeneratedFile, f *protogen.Field) (string, error) {
	switch f.Desc.Kind() {
	case protoreflect.BoolKind:
		return "bool", nil
	case protoreflect.StringKind:
		return "string", nil
	case protoreflect.BytesKind:
		return "[]byte", nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "int32", nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "uint32", nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "int64", nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "uint64", nil
	case protoreflect.FloatKind:
		return "float32", nil
	case protoreflect.DoubleKind:
		return "float64", nil
	case protoreflect.EnumKind:
		return gf.QualifiedGoIdent(f.Enum.GoIdent), nil
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return "*" + gf.QualifiedGoIdent(f.Message.GoIdent), nil
	default:
		return "", fmt.Errorf("%s has kind %s, which has no Go type here", f.Desc.Name(), f.Desc.Kind())
	}
}
