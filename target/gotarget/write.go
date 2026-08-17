package gotarget

import (
	"fmt"
	"strconv"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/heojeongbo/calque/schema"
)

// emitAdd writes the insert.
//
// The shape of each line is decided by three properties of the prop and
// nothing else: whether it has a default, whether it is nullable, and whether
// it is the version. A prop with neither a default nor nullability is set
// unconditionally, which is how a required field gets its "not set" error from
// the database rather than from here.
func (e *emitter) emitAdd(gf *protogen.GeneratedFile, b *bare, m *protogen.Method) error {
	name := b.entity.Name()
	ctx := gf.QualifiedGoIdent(contextPkg.Ident("Context"))
	in := gf.QualifiedGoIdent(m.Input.GoIdent)
	out := gf.QualifiedGoIdent(m.Output.GoIdent)

	// An edge is written as a foreign key, so what comes back out of ent has
	// the key and not the reference. The deferred decorators put the reference
	// back on the response, which is why a caller of Add sees the edge it
	// supplied and a caller of Get has to ask for it.
	var edges []*schema.Edge
	for _, edge := range b.entity.Edges() {
		if fieldNamed(m.Input, edge.Name()) != nil {
			edges = append(edges, edge)
		}
	}

	gf.P("func (s ", name, "ServiceServer) Add(ctx ", ctx, ", req *", in, ") (*", out, ", error) {")
	if len(edges) > 0 {
		gf.P("\tds := make([]func(v *", out, "), 0, ", len(edges), ")")
	}
	gf.P("\tq := s.Db.", name, ".Create()")

	for _, p := range b.entity.Props() {
		// The version is stamped rather than supplied, so it is written even
		// though the request has no field for it -- and at its own position,
		// which is where a reader looking for it would expect it.
		if f, ok := p.(*schema.Field); ok && f.IsVersion() {
			gf.P("\tq.", e.setter(b, f), "(", e.now(gf), ")")
			continue
		}
		on := fieldNamed(m.Input, p.Name())
		if on == nil {
			continue
		}
		if err := e.addProp(gf, b, p, on, out); err != nil {
			return err
		}
	}

	gf.P()
	gf.P("\tu, err := q.Save(ctx)")
	gf.P("\tif err != nil {")
	gf.P("\t\tif err, ok := err.(*", gf.QualifiedGoIdent(b.rootPkg.Ident("ConstraintError")), "); ok && ",
		gf.QualifiedGoIdent(sqlgraphPkg.Ident("IsUniqueConstraintError")), "(err) {")
	gf.P("\t\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "AlreadyExists"), ", \"", name, " already exists: %s\", err.Unwrap())")
	gf.P("\t\t}")
	gf.P("\t\treturn nil, err")
	gf.P("\t}")
	gf.P()
	if len(edges) > 0 {
		gf.P("\tv := u.Proto()")
		gf.P("\tfor _, d := range ds {")
		gf.P("\t\td(v)")
		gf.P("\t}")
		gf.P("\treturn v, nil")
	} else {
		gf.P("\treturn u.Proto(), nil")
	}
	gf.P("}")
	gf.P()
	return nil
}

func (e *emitter) addProp(gf *protogen.GeneratedFile, b *bare, p schema.Prop, on *protogen.Field, out string) error {
	if edge, ok := p.(*schema.Edge); ok {
		return e.addEdge(gf, b, edge, on, out)
	}
	f, ok := p.(*schema.Field)
	if !ok {
		return fmt.Errorf("%s is neither a field nor an edge", p.Name())
	}

	setter := e.setter(b, f)

	// The key is generated when it was not supplied, which is the one place a
	// value appears that the caller never sent.
	if f == b.entity.Key() {
		if f.Type() != schema.TypeUUID {
			return fmt.Errorf("key %s is %s; only a uuid key can be generated here", f.Name(), f.Type())
		}
		gf.P("\tif req.Has", on.GoName, "() {")
		gf.P("\t\tif v, err := ", gf.QualifiedGoIdent(uuidPkg.Ident("FromBytes")), "(req.Get", on.GoName, "()); err != nil {")
		gf.P("\t\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "InvalidArgument"), ", \"", f.Name(), ": %s\", err)")
		gf.P("\t\t} else {")
		gf.P("\t\t\tq.", setter, "(v)")
		gf.P("\t\t}")
		gf.P("\t} else {")
		// v4, not a time-ordered uuid. Changing it changes the physical order
		// rows are inserted in, which is a database decision and not this one.
		gf.P("\t\tq.", setter, "(", gf.QualifiedGoIdent(uuidPkg.Ident("New")), "())")
		gf.P("\t}")
		return nil
	}

	// A repeated or map column arrives as a Go slice or map, which has no
	// presence of its own -- so emptiness stands in for it. A json column typed
	// by a *message* is not in this branch: it is a pointer, and it has
	// presence like anything else.
	if f.IsList() {
		gf.P("\tif u := req.Get", on.GoName, "(); len(u) > 0 {")
		gf.P("\t\tq.", setter, "(u)")
		if f.HasDefault() {
			gf.P("\t} else {")
			gf.P("\t\tq.", setter, "(nil)")
		}
		gf.P("\t}")
		return nil
	}

	value, err := e.fromReq(gf, f, on, "req")
	if err != nil {
		return err
	}

	switch {
	case f.HasDefault():
		zero, err := e.zero(gf, f)
		if err != nil {
			return err
		}
		gf.P("\tif req.Has", on.GoName, "() {")
		gf.P("\t\tq.", setter, "(", value, ")")
		gf.P("\t} else {")
		gf.P("\t\tq.", setter, "(", zero, ")")
		gf.P("\t}")
	case f.IsNullable():
		gf.P("\tif req.Has", on.GoName, "() {")
		gf.P("\t\tq.", setter, "(", value, ")")
		gf.P("\t}")
	default:
		gf.P("\tq.", setter, "(", value, ")")
	}
	return nil
}

func (e *emitter) addEdge(gf *protogen.GeneratedFile, b *bare, edge *schema.Edge, on *protogen.Field, out string) error {
	target := edge.Target()
	ind := "\t"
	if edge.IsNullable() {
		gf.P("\tif req.Has", on.GoName, "() {")
		ind = "\t\t"
	}

	gf.P(ind, "if k, err := ", target.Name(), "GetKey(ctx, s.Db, req.Get", on.GoName, "()); err != nil {")
	gf.P(ind, "\treturn nil, err")
	gf.P(ind, "} else {")
	gf.P(ind, "\tq.Set", e.backend.EntIdent(edge), "ID(k)")

	tmsg, ok := e.message(protoreflect.FullName(target.FullName()))
	if !ok {
		return fmt.Errorf("no Go type for %s", target.FullName())
	}
	tkey := fieldNamed(tmsg, target.Key().Name())
	if tkey == nil {
		return fmt.Errorf("%s has no %s field", target.Name(), target.Key().Name())
	}
	keyExpr := "k"
	if target.Key().Type() == schema.TypeUUID {
		keyExpr = "k[:]"
	}

	gf.P(ind, "\tds = append(ds, func(v *", out, ") {")
	gf.P(ind, "\t\tv.Set", on.GoName, "(", gf.QualifiedGoIdent(tmsg.GoIdent.GoImportPath.Ident(tmsg.GoIdent.GoName+"_builder")), "{", tkey.GoName, ": ", keyExpr, "}.Build())")
	gf.P(ind, "\t})")
	gf.P(ind, "}")

	if edge.IsNullable() {
		gf.P("\t}")
	}
	return nil
}

// emitPatch writes the update, and with it the compare-and-swap.
//
// The version check folds into the same statement as the write: the caller's
// version becomes a WHERE clause, so the read and the write are one round trip
// and no transaction is needed to make them atomic. Zero rows affected then
// means either the row is gone or someone else got there first, and which one
// it was is what the two error codes at the bottom distinguish.
func (e *emitter) emitPatch(gf *protogen.GeneratedFile, b *bare, m *protogen.Method) error {
	name := b.entity.Name()
	ctx := gf.QualifiedGoIdent(contextPkg.Ident("Context"))
	in := gf.QualifiedGoIdent(m.Input.GoIdent)
	out := gf.QualifiedGoIdent(m.Output.GoIdent)

	version := b.entity.Version()
	var vOn, vForce *protogen.Field
	if version != nil {
		vOn = fieldNamed(m.Input, version.Name())
		vForce = fieldNamed(m.Input, version.Name()+"_force")
		if vOn == nil || vForce == nil {
			return fmt.Errorf("%s has a version field but %s has no %s/%s_force",
				name, m.Input.GoIdent.GoName, version.Name(), version.Name())
		}
	}

	gf.P("func (s ", name, "ServiceServer) Patch(ctx ", ctx, ", req *", in, ") (*", out, ", error) {")

	if version != nil {
		gf.P("\tis_force := req.Get", vForce.GoName, "()")
		gf.P("\tif !req.Has", vOn.GoName, "() && !is_force {")
		gf.P("\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "InvalidArgument"), ", \"version not given: %s\", ", strconv.Quote(string(version.Name())), ")")
		gf.P("\t}")
		gf.P()
	}

	gf.P("\tp, err := ", name, "Pick(req.Get", b.shape.refIn.GoName, "())")
	gf.P("\tif err != nil {")
	gf.P("\t\treturn nil, err")
	gf.P("\t}")
	gf.P()
	gf.P("\tq := s.Db.", name, ".Update().Where(p)")

	if version != nil {
		value, err := e.fromReq(gf, version, vOn, "req")
		if err != nil {
			return err
		}
		gf.P("\tif !is_force {")
		gf.P("\t\tq.Where(", e.predicate(gf, b, version, "EQ"), "(", value, "))")
		gf.P("\t}")
	}

	for _, p := range b.entity.Props() {
		if p == schema.Prop(b.entity.Key()) || p.IsImmutable() {
			continue
		}
		on := fieldNamed(m.Input, p.Name())
		if on == nil {
			continue
		}
		if f, ok := p.(*schema.Field); ok && f.IsVersion() {
			// Forcing means the caller supplies the new version rather than
			// having one stamped, which is what makes a restore reproducible.
			gf.P("\tif is_force && req.Has", on.GoName, "() {")
			value, err := e.fromReq(gf, f, on, "req")
			if err != nil {
				return err
			}
			gf.P("\t\tq.", e.setter(b, f), "(", value, ")")
			gf.P("\t} else {")
			gf.P("\t\tq.", e.setter(b, f), "(", e.now(gf), ")")
			gf.P("\t}")
			continue
		}
		if err := e.patchProp(gf, b, m, p, on); err != nil {
			return err
		}
	}

	gf.P()
	gf.P("\tif n, err := q.Save(ctx); err != nil {")
	gf.P("\t\treturn nil, err")
	gf.P("\t} else if n == 0 {")
	if version != nil {
		gf.P("\t\tif is_force {")
		gf.P("\t\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "NotFound"), ", \"not found\")")
		gf.P("\t\t} else {")
		gf.P("\t\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "FailedPrecondition"), ", \"version not matched: %s\", ", strconv.Quote(string(version.Name())), ")")
		gf.P("\t\t}")
	} else {
		gf.P("\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "NotFound"), ", \"not found\")")
	}
	gf.P("\t}")
	gf.P()
	gf.P("\treturn s.Get(ctx, req.Get", b.shape.refIn.GoName, "().Pick())")
	gf.P("}")
	gf.P()
	return nil
}

func (e *emitter) patchProp(gf *protogen.GeneratedFile, b *bare, m *protogen.Method, p schema.Prop, on *protogen.Field) error {
	// A nullable prop needs two bits from the caller, because proto cannot tell
	// "leave it alone" from "set it to nothing". The companion says which.
	null := fieldNamed(m.Input, p.Name()+"_null")

	if edge, ok := p.(*schema.Edge); ok {
		ind := "\t"
		if null != nil {
			gf.P("\tif req.Get", null.GoName, "() {")
			gf.P("\t\tq.Clear", e.backend.EntIdent(edge), "()")
			gf.P("\t} else if req.Has", on.GoName, "() {")
			ind = "\t\t"
		} else {
			gf.P("\tif req.Has", on.GoName, "() {")
			ind = "\t\t"
		}
		gf.P(ind, "if id, err := ", edge.Target().Name(), "GetKey(ctx, s.Db, req.Get", on.GoName, "()); err != nil {")
		gf.P(ind, "\treturn nil, err")
		gf.P(ind, "} else {")
		gf.P(ind, "\tq.Set", e.backend.EntIdent(edge), "ID(id)")
		gf.P(ind, "}")
		gf.P("\t}")
		return nil
	}

	f, ok := p.(*schema.Field)
	if !ok {
		return fmt.Errorf("%s is neither a field nor an edge", p.Name())
	}
	setter := e.setter(b, f)

	if f.IsList() {
		gf.P("\tif u := req.Get", on.GoName, "(); len(u) > 0 {")
		gf.P("\t\tq.", setter, "(u)")
		gf.P("\t}")
		return nil
	}

	value, err := e.fromReq(gf, f, on, "req")
	if err != nil {
		return err
	}
	if null != nil {
		gf.P("\tif req.Get", null.GoName, "() {")
		gf.P("\t\tq.Clear", e.backend.EntIdent(f), "()")
		gf.P("\t} else if req.Has", on.GoName, "() {")
		gf.P("\t\tq.", setter, "(", value, ")")
		gf.P("\t}")
		return nil
	}
	gf.P("\tif req.Has", on.GoName, "() {")
	gf.P("\t\tq.", setter, "(", value, ")")
	gf.P("\t}")
	return nil
}

// setter is ent's setter for a prop. The key is ent's own ID.
func (e *emitter) setter(b *bare, f *schema.Field) string {
	if f == b.entity.Key() {
		return "SetID"
	}
	return "Set" + e.backend.EntIdent(f)
}

// fromReq reads a prop off the request in the type ent wants.
func (e *emitter) fromReq(gf *protogen.GeneratedFile, f *schema.Field, on *protogen.Field, recv string) (string, error) {
	get := recv + ".Get" + on.GoName + "()"
	switch f.Type() {
	case schema.TypeTime:
		return get + ".AsTime()", nil
	case schema.TypeUUID:
		// A uuid that is not the key would need FromBytes and an error return,
		// which is a statement rather than an expression. Nothing in the
		// measured schema has one, so rather than emit something untested this
		// says what it cannot do.
		return "", fmt.Errorf("%s is a uuid that is not the key, which this target does not write yet", f.Name())
	default:
		return get, nil
	}
}

// zero is what a prop with a default is set to when the caller sent nothing.
//
// The default's *value* is not used: the annotation says a default exists, and
// the generator picks the type's own. A time is the exception, and it is the
// reason date_created works without anybody writing a timestamp down.
func (e *emitter) zero(gf *protogen.GeneratedFile, f *schema.Field) (string, error) {
	switch f.Type() {
	case schema.TypeString:
		return `""`, nil
	case schema.TypeTime:
		return e.now(gf), nil
	case schema.TypeBytes:
		return "nil", nil
	case schema.TypeBool:
		return "false", nil
	case schema.TypeJSON:
		return "nil", nil
	default:
		if f.Type().IsInteger() {
			return "0", nil
		}
		switch f.Type() {
		case schema.TypeFloat, schema.TypeDouble:
			return "0", nil
		}
		return "", fmt.Errorf("%s is %s, which has no default this target can write", f.Name(), f.Type())
	}
}

func (e *emitter) now(gf *protogen.GeneratedFile) string {
	return gf.QualifiedGoIdent(timePkg.Ident("Now")) + "().UTC()"
}
