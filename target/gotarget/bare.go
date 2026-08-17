package gotarget

import (
	"fmt"
	"path"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/heojeongbo/calque/schema"
)

var (
	contextPkg  = protogen.GoImportPath("context")
	timePkg     = protogen.GoImportPath("time")
	codesPkg    = protogen.GoImportPath("google.golang.org/grpc/codes")
	statusPkg   = protogen.GoImportPath("google.golang.org/grpc/status")
	emptypbPkg  = protogen.GoImportPath("google.golang.org/protobuf/types/known/emptypb")
	sqlgraphPkg = protogen.GoImportPath("entgo.io/ent/dialect/sql/sqlgraph")
)

// bare is one entity's server, and the identifiers it needs from three
// different namers.
//
// Every line below spells the same prop up to three ways: ent's Go name
// (DeviceID), protoc-gen-go's (DeviceId), and the proto's own (device_id). They
// appear together often enough that a single "the name" would be wrong in most
// of them -- `q.SetDeviceID(req.GetDeviceId())` is one statement using two.
type bare struct {
	entity *schema.Entity
	shape  *refShape
	svc    *protogen.Service

	// entPkg is the ent package for this entity: the constants, the predicates
	// and the field names ent generated from the same schema.
	entPkg protogen.GoImportPath
	// rootPkg is ent's own package, holding the client and the query types.
	rootPkg protogen.GoImportPath
	// predPkg holds predicate.<Entity>.
	predPkg protogen.GoImportPath

	methods map[schema.Op]*protogen.Method

	// needsKey is whether anything resolves a reference to this entity's key.
	needsKey bool
}

// emitBare writes one server file per proto file.
func (e *emitter) emitBare() error {
	sources := e.gen.Sources()
	if len(sources) == 0 {
		return nil
	}

	type group struct {
		path    string
		source  string
		entries []*bare
	}
	var groups []*group
	byPath := map[string]*group{}

	for _, en := range sources {
		b, err := e.bareOf(en)
		if err != nil {
			return fmt.Errorf("go: %s: %w", en.FullName(), err)
		}
		if b == nil {
			continue
		}

		f, _ := e.file(en)
		base := strings.TrimSuffix(path.Base(en.File()), ".proto")
		p := path.Join(string(f.GoImportPath), e.target.opts.BareDir, base+".g.go")

		g, ok := byPath[p]
		if !ok {
			g = &group{path: p, source: en.File()}
			byPath[p] = g
			groups = append(groups, g)
		}
		g.entries = append(g.entries, b)
	}
	if len(groups) == 0 {
		return nil
	}

	// XGetKey resolves a reference to the key it names, and its callers are
	// always some *other* entity's Add or Patch writing an edge. The generator
	// this reproduces emitted it with the entity's own patch, which is a
	// different condition and not always the same set: an entity that is an
	// edge target and declares only `get` gets called and not generated.
	//
	// The two coincide on the schema this was measured against, so widening the
	// condition changes nothing there and fixes the case where they do not. An
	// unused exported function is a smaller problem than a missing one.
	var all []*bare
	for _, grp := range groups {
		all = append(all, grp.entries...)
	}
	needs := map[string]bool{}
	for _, b := range all {
		if _, ok := b.methods[schema.OpPatch]; ok {
			needs[b.entity.FullName()] = true
		}
		_, add := b.methods[schema.OpAdd]
		_, patch := b.methods[schema.OpPatch]
		if !add && !patch {
			continue
		}
		for _, edge := range b.entity.Edges() {
			needs[edge.Target().FullName()] = true
		}
	}
	for _, b := range all {
		b.needsKey = needs[b.entity.FullName()]
	}

	for _, grp := range groups {
		dir := strings.TrimSuffix(grp.path, "/"+lastSegment(grp.path))
		gf := e.pg.NewGeneratedFile(grp.path, protogen.GoImportPath(dir))
		gf.P(e.target.opts.Header)
		gf.P("// source: ", grp.source)
		gf.P()
		gf.P("package ", lastSegment(dir))
		gf.P()

		for _, b := range grp.entries {
			if err := e.emitBareEntity(gf, b); err != nil {
				return fmt.Errorf("go: %s: %w", b.entity.FullName(), err)
			}
		}
	}

	return e.emitBareStore(groups[0].path)
}

func (e *emitter) bareOf(en *schema.Entity) (*bare, error) {
	svc, ok := e.service(en)
	if !ok {
		// An entity nothing serves has no server. It is still an edge target,
		// and still gets a schema; it just has no methods to implement.
		return nil, nil
	}
	shape, err := e.shapeOf(en)
	if err != nil {
		return nil, err
	}
	f, ok := e.file(en)
	if !ok {
		return nil, fmt.Errorf("no protogen file")
	}

	root := protogen.GoImportPath(path.Join(string(f.GoImportPath), e.target.opts.EntDir))
	b := &bare{
		entity:  en,
		shape:   shape,
		svc:     svc,
		rootPkg: root,
		// ent names a package after the type, lower-cased whole -- PosePreset
		// becomes posepreset, not pose_preset. It is the same spelling the
		// backend chose for the table, and for the same reason: it is ent's.
		entPkg:  protogen.GoImportPath(path.Join(string(root), strings.ToLower(en.Name()))),
		predPkg: protogen.GoImportPath(path.Join(string(root), "predicate")),
		methods: map[schema.Op]*protogen.Method{},
	}

	for _, op := range schema.AllOps() {
		if !en.Rpc().Has(op) {
			continue
		}
		want := strings.ToUpper(string(op)[:1]) + string(op)[1:]
		for _, m := range svc.Methods {
			if string(m.Desc.Name()) == want {
				b.methods[op] = m
				break
			}
		}
	}
	return b, nil
}

func (e *emitter) emitBareEntity(gf *protogen.GeneratedFile, b *bare) error {
	name := b.entity.Name()
	client := gf.QualifiedGoIdent(b.rootPkg.Ident("Client"))
	server := gf.QualifiedGoIdent(b.shape.msg.GoIdent.GoImportPath.Ident(b.svc.GoName + "Server"))
	unimpl := gf.QualifiedGoIdent(b.shape.msg.GoIdent.GoImportPath.Ident("Unimplemented" + b.svc.GoName + "Server"))

	gf.P("type ", name, "ServiceServer struct {")
	gf.P("\tDb *", client)
	gf.P("\t", unimpl)
	gf.P("}")
	gf.P()

	// A value receiver, and the constructor returns the interface rather than
	// the struct. Both are load-bearing: hand-written code embeds this by value
	// and assigns the result to an interface variable.
	gf.P("func New", name, "ServiceServer(db *", client, ") ", server, " {")
	gf.P("\treturn ", name, "ServiceServer{Db: db}")
	gf.P("}")
	gf.P()

	if m, ok := b.methods[schema.OpAdd]; ok {
		if err := e.emitAdd(gf, b, m); err != nil {
			return err
		}
	}
	if m, ok := b.methods[schema.OpGet]; ok {
		e.emitGet(gf, b, m)
	}

	e.emitSelect(gf, b)

	if m, ok := b.methods[schema.OpPatch]; ok {
		if err := e.emitPatch(gf, b, m); err != nil {
			return err
		}
	}
	if b.needsKey {
		if err := e.emitGetKey(gf, b); err != nil {
			return err
		}
	}
	if m, ok := b.methods[schema.OpErase]; ok {
		e.emitErase(gf, b, m)
	}

	return e.emitPick(gf, b)
}

// emitGet reads one row by its reference.
func (e *emitter) emitGet(gf *protogen.GeneratedFile, b *bare, m *protogen.Method) {
	name := b.entity.Name()
	ctx := gf.QualifiedGoIdent(contextPkg.Ident("Context"))
	in := gf.QualifiedGoIdent(m.Input.GoIdent)
	out := gf.QualifiedGoIdent(m.Output.GoIdent)

	gf.P("func (s ", name, "ServiceServer) Get(ctx ", ctx, ", req *", in, ") (*", out, ", error) {")
	gf.P("\tq := s.Db.", name, ".Query()")
	gf.P()
	gf.P("\tif p, err := ", name, "Pick(req.Get", b.shape.refIn.GoName, "()); err != nil {")
	gf.P("\t\treturn nil, err")
	gf.P("\t} else {")
	gf.P("\t\tq.Where(p)")
	gf.P("\t}")
	if b.shape.selIn != nil {
		gf.P("\t", name, "SelectInit(q, req.Get", b.shape.selIn.GoName, "())")
	}
	gf.P()
	gf.P("\tv, err := q.Only(ctx)")
	gf.P("\tif err != nil {")
	gf.P("\t\tif ", gf.QualifiedGoIdent(b.rootPkg.Ident("IsNotFound")), "(err) {")
	gf.P("\t\t\treturn nil, ", e.status(gf, "Error"), "(", e.code(gf, "NotFound"), ", \"", name, " not found\")")
	gf.P("\t\t}")
	gf.P("\t\treturn nil, err")
	gf.P("\t}")
	gf.P("\treturn v.Proto(), nil")
	gf.P("}")
	gf.P()
}

// emitErase removes a row.
//
// It returns (nil, nil) rather than an empty message, which is a nil pointer
// where the signature promises one. grpc-go marshals it as an empty message, so
// it works; a caller dereferencing the result does not. It is what the
// generator being reproduced emits.
func (e *emitter) emitErase(gf *protogen.GeneratedFile, b *bare, m *protogen.Method) {
	name := b.entity.Name()
	ctx := gf.QualifiedGoIdent(contextPkg.Ident("Context"))
	in := gf.QualifiedGoIdent(m.Input.GoIdent)
	out := gf.QualifiedGoIdent(m.Output.GoIdent)
	_ = emptypbPkg

	gf.P("func (s ", name, "ServiceServer) Erase(ctx ", ctx, ", req *", in, ") (*", out, ", error) {")
	gf.P("\tp, err := ", name, "Pick(req)")
	gf.P("\tif err != nil {")
	gf.P("\t\treturn nil, err")
	gf.P("\t}")
	gf.P()
	gf.P("\tif _, err := s.Db.", name, ".Delete().Where(p).Exec(ctx); err != nil {")
	gf.P("\t\treturn nil, err")
	gf.P("\t}")
	gf.P("\treturn nil, nil")
	gf.P("}")
	gf.P()
}

// emitSelect writes the four functions that decide which columns and which
// edges a query loads.
func (e *emitter) emitSelect(gf *protogen.GeneratedFile, b *bare) {
	name := b.entity.Name()
	query := gf.QualifiedGoIdent(b.rootPkg.Ident(name + "Query"))
	fieldID := gf.QualifiedGoIdent(b.entPkg.Ident("FieldID"))

	gf.P("func select", name, "Key(q *", query, ") {")
	gf.P("\tq.Select(", fieldID, ")")
	gf.P("}")
	gf.P()

	if b.shape.sel == nil {
		return
	}
	sel := gf.QualifiedGoIdent(b.shape.sel.GoIdent)
	columns := gf.QualifiedGoIdent(b.entPkg.Ident("Columns"))

	gf.P("func ", name, "SelectedFields(m *", sel, ") []string {")
	gf.P("\tif m.GetAll() {")
	gf.P("\t\treturn ", columns)
	gf.P("\t}")
	gf.P()
	gf.P("\tvs := make([]string, 0, len(", columns, "))")
	// The key is not optional, so it is appended unconditionally -- inside a
	// bare block, because it comes out of the same loop that writes the `if`s
	// for everything else.
	gf.P("\t{")
	gf.P("\t\tvs = append(vs, ", fieldID, ")")
	gf.P("\t}")
	for _, f := range b.entity.Fields() {
		if f == b.entity.Key() {
			continue
		}
		on := fieldNamed(b.shape.sel, f.Name())
		if on == nil {
			continue
		}
		gf.P("\tif m.Get", on.GoName, "() {")
		gf.P("\t\tvs = append(vs, ", gf.QualifiedGoIdent(b.entPkg.Ident("Field"+e.backend.EntIdent(f))), ")")
		gf.P("\t}")
	}
	gf.P()
	gf.P("\treturn vs")
	gf.P("}")
	gf.P()

	gf.P("func ", name, "Select(q *", query, ", m *", sel, ") {")
	gf.P("\tif !m.GetAll() {")
	gf.P("\t\tfields := ", name, "SelectedFields(m)")
	gf.P("\t\tq.Select(fields...)")
	gf.P("\t}")
	for _, edge := range b.entity.Edges() {
		on := fieldNamed(b.shape.sel, edge.Name())
		if on == nil {
			continue
		}
		target := edge.Target().Name()
		tq := gf.QualifiedGoIdent(b.rootPkg.Ident(target + "Query"))
		gf.P("\tif m.Has", on.GoName, "() {")
		gf.P("\t\tq.With", e.backend.EntIdent(edge), "(func(q *", tq, ") {")
		gf.P("\t\t\t", target, "Select(q, m.Get", on.GoName, "())")
		gf.P("\t\t})")
		gf.P("\t}")
	}
	gf.P("}")
	gf.P()

	// The else branch loads every edge key-only, so that a caller who asked for
	// nothing still gets references it can resolve. An entity with no edges
	// gets an empty else, which gofmt keeps.
	gf.P("func ", name, "SelectInit(q *", query, ", m *", sel, ") {")
	gf.P("\tif m != nil {")
	gf.P("\t\t", name, "Select(q, m)")
	gf.P("\t} else {")
	for _, edge := range b.entity.Edges() {
		if fieldNamed(b.shape.sel, edge.Name()) == nil {
			continue
		}
		gf.P("\t\tq.With", e.backend.EntIdent(edge), "(select", edge.Target().Name(), "Key)")
	}
	gf.P("\t}")
	gf.P("}")
	gf.P()
}

// emitGetKey resolves a reference to the key it names, cheaply when it can.
func (e *emitter) emitGetKey(gf *protogen.GeneratedFile, b *bare) error {
	name := b.entity.Name()
	key := b.entity.Key()
	keyType, err := e.entKeyType(gf, b, key)
	if err != nil {
		return err
	}
	ctx := gf.QualifiedGoIdent(contextPkg.Ident("Context"))
	client := gf.QualifiedGoIdent(b.rootPkg.Ident("Client"))
	ref := gf.QualifiedGoIdent(b.shape.ref.GoIdent)
	keyField := fieldNamed(b.shape.msg, key.Name())

	gf.P("func ", name, "GetKey(ctx ", ctx, ", db *", client, ", ref *", ref, ") (", keyType, ", error) {")
	gf.P("\tvar z ", keyType)
	// The short circuit is not an optimisation so much as the reason the
	// function is usable at all: resolving a reference that is already the key
	// must not require the row to exist.
	gf.P("\tif ref.Has", keyField.GoName, "() {")
	if key.Type() == schema.TypeUUID {
		gf.P("\t\tif v, err := ", gf.QualifiedGoIdent(uuidPkg.Ident("FromBytes")), "(ref.Get", keyField.GoName, "()); err != nil {")
		gf.P("\t\t\treturn z, ", e.status(gf, "Errorf"), "(", e.code(gf, "InvalidArgument"), ", \"", key.Name(), ": %s\", err)")
		gf.P("\t\t} else {")
		gf.P("\t\t\treturn v, nil")
		gf.P("\t\t}")
	} else {
		gf.P("\t\treturn ref.Get", keyField.GoName, "(), nil")
	}
	gf.P("\t}")
	gf.P()
	gf.P("\tp, err := ", name, "Pick(ref)")
	gf.P("\tif err != nil {")
	gf.P("\t\treturn z, err")
	gf.P("\t}")
	gf.P()
	gf.P("\tv, err := db.", name, ".Query().Where(p).OnlyID(ctx)")
	gf.P("\tif err != nil {")
	gf.P("\t\tif ", gf.QualifiedGoIdent(b.rootPkg.Ident("IsNotFound")), "(err) {")
	gf.P("\t\t\treturn z, ", e.status(gf, "Error"), "(", e.code(gf, "NotFound"), ", \"", name, " not found\")")
	gf.P("\t\t}")
	gf.P("\t\treturn z, err")
	gf.P("\t}")
	gf.P()
	gf.P("\treturn v, nil")
	gf.P("}")
	gf.P()
	return nil
}

// emitPick turns a reference into a predicate.
func (e *emitter) emitPick(gf *protogen.GeneratedFile, b *bare) error {
	name := b.entity.Name()
	ref := gf.QualifiedGoIdent(b.shape.ref.GoIdent)
	pred := gf.QualifiedGoIdent(b.predPkg.Ident(name))

	gf.P("func ", name, "Pick(req *", ref, ") (", pred, ", error) {")
	gf.P("\tswitch req.Which", b.shape.oneof.GoName, "() {")

	for _, key := range b.shape.keys {
		gf.P("\tcase ", gf.QualifiedGoIdent(b.shape.ref.GoIdent.GoImportPath.Ident(b.shape.ref.GoIdent.GoName+"_"+key.field.GoName+"_case")), ":")

		if key.wrapper == nil {
			prop, _ := b.entity.Prop(schema.ProtoName(key.field.Desc.Name()))
			if err := e.emitPickField(gf, b, key.field, prop, "req", "\t\t", true); err != nil {
				return err
			}
			continue
		}

		gf.P("\t\tk := req.Get", key.field.GoName, "()")
		gf.P("\t\tps := make([]", pred, ", 0, ", len(key.members), ")")
		for _, m := range key.members {
			if err := e.emitPickMember(gf, b, key, m); err != nil {
				return err
			}
		}
		gf.P("\t\treturn ", gf.QualifiedGoIdent(b.entPkg.Ident("And")), "(ps...), nil")
	}

	gf.P("\tcase ", gf.QualifiedGoIdent(b.shape.ref.GoIdent.GoImportPath.Ident(b.shape.ref.GoIdent.GoName+"_"+b.shape.oneof.GoName+"_not_set_case")), ":")
	gf.P("\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "InvalidArgument"), ", \"key not set: ", name, "\")")
	gf.P("\tdefault:")
	gf.P("\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "Unimplemented"), ", \"unknown type of key: %s\", req.Which", b.shape.oneof.GoName, "())")
	gf.P("\t}")
	gf.P("}")
	gf.P()
	return nil
}

// emitPickField writes the arm for a key that is a plain column.
func (e *emitter) emitPickField(gf *protogen.GeneratedFile, b *bare, f *protogen.Field, prop schema.Prop, recv, ind string, ret bool) error {
	field, ok := prop.(*schema.Field)
	if !ok {
		return fmt.Errorf("%s is not a field", prop.Name())
	}
	eq := e.predicate(gf, b, field, "EQ")

	if field.Type() == schema.TypeUUID {
		gf.P(ind, "if v, err := ", gf.QualifiedGoIdent(uuidPkg.Ident("FromBytes")), "(", recv, ".Get", f.GoName, "()); err != nil {")
		gf.P(ind, "\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "InvalidArgument"), ", \"", field.Name(), ": %s\", err)")
		gf.P(ind, "} else {")
		gf.P(ind, "\treturn ", eq, "(v), nil")
		gf.P(ind, "}")
		return nil
	}
	gf.P(ind, "return ", eq, "(", recv, ".Get", f.GoName, "()), nil")
	return nil
}

// emitPickMember writes one component of a wrapped key.
func (e *emitter) emitPickMember(gf *protogen.GeneratedFile, b *bare, key refKey, m *queryMember) error {
	// The dotted path is the key's name and the member's, which is what the
	// caller sees when a nested reference is bad: "slug.tenant".
	where := string(key.elem.Name()) + "." + string(m.prop.Name())

	if edge, ok := m.prop.(*schema.Edge); ok {
		target := edge.Target().Name()
		gf.P("\t\tif p, err := ", target, "Pick(k.Get", m.on.GoName, "()); err != nil {")
		gf.P("\t\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "InvalidArgument"), ", \"", where, ": %s\", err)")
		gf.P("\t\t} else {")
		gf.P("\t\t\tps = append(ps, ", gf.QualifiedGoIdent(b.entPkg.Ident("Has"+e.backend.EntIdent(edge)+"With")), "(p))")
		gf.P("\t\t}")
		return nil
	}

	field, ok := m.prop.(*schema.Field)
	if !ok {
		return fmt.Errorf("%s is neither a field nor an edge", m.prop.Name())
	}
	eq := e.predicate(gf, b, field, "EQ")

	if field.Type() == schema.TypeUUID {
		gf.P("\t\tif v, err := ", gf.QualifiedGoIdent(uuidPkg.Ident("FromBytes")), "(k.Get", m.on.GoName, "()); err != nil {")
		gf.P("\t\t\treturn nil, ", e.status(gf, "Errorf"), "(", e.code(gf, "InvalidArgument"), ", \"", where, ": %s\", err)")
		gf.P("\t\t} else {")
		gf.P("\t\t\tps = append(ps, ", eq, "(v))")
		gf.P("\t\t}")
		return nil
	}
	gf.P("\t\tps = append(ps, ", eq, "(k.Get", m.on.GoName, "()))")
	return nil
}

// predicate names one of ent's generated predicates for a field.
//
// The key is ent's own ID rather than a column of its name, so it is IDEQ and
// not the field's.
func (e *emitter) predicate(gf *protogen.GeneratedFile, b *bare, f *schema.Field, op string) string {
	if f == b.entity.Key() {
		return gf.QualifiedGoIdent(b.entPkg.Ident("ID" + op))
	}
	return gf.QualifiedGoIdent(b.entPkg.Ident(e.backend.EntIdent(f) + op))
}

// entKeyType is the Go type ent gave the key.
func (e *emitter) entKeyType(gf *protogen.GeneratedFile, b *bare, key *schema.Field) (string, error) {
	switch key.Type() {
	case schema.TypeUUID:
		return gf.QualifiedGoIdent(uuidPkg.Ident("UUID")), nil
	case schema.TypeString:
		return "string", nil
	case schema.TypeInt64, schema.TypeSint64, schema.TypeSfixed64:
		return "int64", nil
	case schema.TypeInt32, schema.TypeSint32, schema.TypeSfixed32:
		return "int32", nil
	default:
		return "", fmt.Errorf("key %s is %s, which has no ent key type here", key.Name(), key.Type())
	}
}

func (e *emitter) status(gf *protogen.GeneratedFile, name string) string {
	return gf.QualifiedGoIdent(statusPkg.Ident(name))
}

func (e *emitter) code(gf *protogen.GeneratedFile, name string) string {
	return gf.QualifiedGoIdent(codesPkg.Ident(name))
}

// emitBareStore writes the one file that gathers every server.
func (e *emitter) emitBareStore(sibling string) error {
	sources := e.gen.Sources()
	var served []*schema.Entity
	for _, en := range sources {
		if _, ok := e.service(en); ok {
			served = append(served, en)
		}
	}
	if len(served) == 0 {
		return nil
	}

	dir := strings.TrimSuffix(sibling, "/"+lastSegment(sibling))
	gf := e.pg.NewGeneratedFile(dir+"/store.g.go", protogen.GoImportPath(dir))
	gf.P(e.target.opts.Header)
	gf.P()
	gf.P("package ", lastSegment(dir))
	gf.P()

	f, _ := e.file(served[0])
	root := protogen.GoImportPath(path.Join(string(f.GoImportPath), e.target.opts.EntDir))
	client := gf.QualifiedGoIdent(root.Ident("Client"))

	gf.P("type Server struct {")
	gf.P("\tDb *", client)
	gf.P("}")
	gf.P()
	gf.P("func NewServer(db *", client, ") Server {")
	gf.P("\treturn Server{Db: db}")
	gf.P("}")
	gf.P()

	for _, en := range served {
		svc, _ := e.service(en)
		msg, _ := e.message(protoreflect.FullName(en.FullName()))
		iface := gf.QualifiedGoIdent(msg.GoIdent.GoImportPath.Ident(svc.GoName + "Server"))
		gf.P("func (s Server) ", en.Name(), "() ", iface, " { return New", en.Name(), "ServiceServer(s.Db) }")
	}
	return nil
}
