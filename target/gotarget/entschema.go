package gotarget

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/HeoJeongBo/calque/gen"
	"github.com/HeoJeongBo/calque/schema"
)

// The ent packages a schema type names.
var (
	entPkg       = protogen.GoImportPath("entgo.io/ent")
	entsqlPkg    = protogen.GoImportPath("entgo.io/ent/dialect/entsql")
	entschemaPkg = protogen.GoImportPath("entgo.io/ent/schema")
	edgePkg      = protogen.GoImportPath("entgo.io/ent/schema/edge")
	fieldPkg     = protogen.GoImportPath("entgo.io/ent/schema/field")
	indexPkg     = protogen.GoImportPath("entgo.io/ent/schema/index")
	uuidPkg      = protogen.GoImportPath("github.com/google/uuid")
)

// emitSchemas writes one ent schema type per entity.
//
// The whole file set has to land in one Go package, because ent requires it and
// because edge.To("tenant", Tenant.Type) names a sibling type without
// qualifying it.
func (e *emitter) emitSchemas() ([]gen.File, error) {
	sources := e.gen.Sources()
	if len(sources) == 0 {
		return nil, nil
	}

	if err := e.checkOnePackage(sources); err != nil {
		return nil, err
	}

	// One output file per *proto file*, not per entity: the name comes from the
	// proto's basename, so pose_preset.proto becomes pose_preset.go even though
	// the entity is PosePreset. A proto declaring two entities produces one file
	// with two types in it -- the schema this was measured against happens to
	// have one entity per file, which hides the distinction until it does not.
	type group struct {
		path     string
		source   string
		entities []*schema.Entity
	}
	var groups []*group
	byPath := map[string]*group{}

	for _, en := range sources {
		path, err := e.schemaPath(en)
		if err != nil {
			return nil, err
		}
		g, ok := byPath[path]
		if !ok {
			g = &group{path: path, source: en.File()}
			byPath[path] = g
			groups = append(groups, g)
		}
		g.entities = append(g.entities, en)
	}

	done := 0
	for _, grp := range groups {
		dir := strings.TrimSuffix(grp.path, "/"+lastSegment(grp.path))
		gf := e.pg.NewGeneratedFile(grp.path, protogen.GoImportPath(dir))
		gf.P(e.target.opts.Header)
		gf.P("// source: ", grp.source)
		gf.P()
		gf.P("package ", lastSegment(dir))
		gf.P()

		for _, en := range grp.entities {
			f, ok := e.file(en)
			if !ok {
				return nil, fmt.Errorf("go: %s: no protogen file", en.FullName())
			}
			if err := e.emitSchemaType(gf, f, en); err != nil {
				return nil, err
			}
			done++
			e.gen.Step(done, len(sources), en.Name())
		}
	}

	// protogen owns the response, so the files come back through it rather
	// than being returned directly. Everything it produced this run is ours:
	// nothing else writes to this plugin instance.
	res := e.pg.Response()
	if msg := res.GetError(); msg != "" {
		return nil, fmt.Errorf("go: %s", msg)
	}

	out := make([]gen.File, 0, len(res.GetFile()))
	for _, f := range res.GetFile() {
		out = append(out, gen.File{Name: f.GetName(), Body: []byte(f.GetContent())})
	}
	return out, nil
}

// checkOnePackage refuses a schema split across Go packages.
//
// ent generates from one directory, and an edge names its target as a bare
// identifier. Two packages would emit code that cannot resolve, so it is said
// here rather than discovered as an undefined symbol.
func (e *emitter) checkOnePackage(sources []*schema.Entity) error {
	seen := map[string][]string{}
	for _, en := range sources {
		f, ok := e.file(en)
		if !ok {
			continue
		}
		pkg := string(f.GoImportPath)
		seen[pkg] = append(seen[pkg], en.FullName())
	}
	if len(seen) <= 1 {
		return nil
	}

	var pkgs []string
	for pkg := range seen {
		pkgs = append(pkgs, fmt.Sprintf("%s (%s)", pkg, strings.Join(seen[pkg], ", ")))
	}
	sort.Strings(pkgs)
	return fmt.Errorf(
		"go: entities are split across %d Go packages, and ent generates from one directory:\n\t%s\n"+
			"an edge names its target as a bare identifier, so a schema spanning packages cannot compile",
		len(seen), strings.Join(pkgs, "\n\t"))
}

func (e *emitter) emitSchemaType(gf *protogen.GeneratedFile, f *protogen.File, en *schema.Entity) error {
	name := en.Name()

	gf.P("type ", name, " struct {")
	gf.P("\t", gf.QualifiedGoIdent(entPkg.Ident("Schema")))
	gf.P("}")
	gf.P()

	if err := e.emitFields(gf, f, en); err != nil {
		return err
	}
	if len(en.Edges()) > 0 {
		e.emitEdges(gf, en)
	}
	if len(en.Indexes()) > 0 {
		if err := e.emitIndexes(gf, en); err != nil {
			return err
		}
	}
	e.emitAnnotations(gf, en)
	return nil
}

func (e *emitter) emitFields(gf *protogen.GeneratedFile, f *protogen.File, en *schema.Entity) error {
	gf.P("func (", en.Name(), ") Fields() []", gf.QualifiedGoIdent(entPkg.Ident("Field")), " {")
	gf.P("\treturn []", gf.QualifiedGoIdent(entPkg.Ident("Field")), "{")

	for _, fl := range en.Fields() {
		ctor, err := e.fieldCtor(gf, fl)
		if err != nil {
			return fmt.Errorf("go: %s.%s: %w", en.FullName(), fl.Name(), err)
		}
		gf.P("\t\t", ctor, modifiers(en, fl))
	}

	gf.P("\t}")
	gf.P("}")
	gf.P()
	return nil
}

// fieldCtor is the ent constructor for one field.
func (e *emitter) fieldCtor(gf *protogen.GeneratedFile, f *schema.Field) (string, error) {
	name := strconvQuote(string(f.Name()))

	switch f.Type() {
	case schema.TypeUUID:
		return fmt.Sprintf("%s(%s, %s{})",
			gf.QualifiedGoIdent(fieldPkg.Ident("UUID")), name,
			gf.QualifiedGoIdent(uuidPkg.Ident("UUID"))), nil
	case schema.TypeTime:
		return fmt.Sprintf("%s(%s)", gf.QualifiedGoIdent(fieldPkg.Ident("Time")), name), nil
	case schema.TypeJSON:
		zero, err := e.jsonZero(gf, f)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s, %s)",
			gf.QualifiedGoIdent(fieldPkg.Ident("JSON")), name, zero), nil
	}

	ctor, ok := scalarCtor[f.Type()]
	if !ok {
		return "", fmt.Errorf("no ent field constructor for %s", f.Type())
	}
	return fmt.Sprintf("%s(%s)", gf.QualifiedGoIdent(fieldPkg.Ident(ctor)), name), nil
}

var scalarCtor = map[schema.Type]string{
	schema.TypeBool:     "Bool",
	schema.TypeString:   "String",
	schema.TypeBytes:    "Bytes",
	schema.TypeInt32:    "Int32",
	schema.TypeSint32:   "Int32",
	schema.TypeSfixed32: "Int32",
	schema.TypeUint32:   "Uint32",
	schema.TypeFixed32:  "Uint32",
	schema.TypeInt64:    "Int64",
	schema.TypeSint64:   "Int64",
	schema.TypeSfixed64: "Int64",
	schema.TypeUint64:   "Uint64",
	schema.TypeFixed64:  "Uint64",
	schema.TypeFloat:    "Float32",
	schema.TypeDouble:   "Float",
	schema.TypeEnum:     "Int32",
}

// jsonZero is the Go zero value a field.JSON column is typed by.
//
// A map is its Go map type; a message is a pointer to protogen's Go type, which
// is where the import comes from.
func (e *emitter) jsonZero(gf *protogen.GeneratedFile, f *schema.Field) (string, error) {
	fd := f.Source()
	if fd == nil {
		return "", fmt.Errorf("no descriptor")
	}

	if fd.IsMap() {
		k, err := e.goScalar(gf, fd.MapKey())
		if err != nil {
			return "", err
		}
		v, err := e.goScalar(gf, fd.MapValue())
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("map[%s]%s{}", k, v), nil
	}

	if fd.Kind() == protoreflect.MessageKind {
		m, ok := e.message(fd.Message().FullName())
		if !ok {
			return "", fmt.Errorf("no Go type for %s", fd.Message().FullName())
		}
		return "&" + gf.QualifiedGoIdent(m.GoIdent) + "{}", nil
	}

	return "", fmt.Errorf("json field is neither a map nor a message (%s)", fd.Kind())
}

// goScalar is the Go type of a map's key or value.
func (e *emitter) goScalar(gf *protogen.GeneratedFile, fd protoreflect.FieldDescriptor) (string, error) {
	switch fd.Kind() {
	case protoreflect.StringKind:
		return "string", nil
	case protoreflect.BoolKind:
		return "bool", nil
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
	case protoreflect.MessageKind:
		m, ok := e.message(fd.Message().FullName())
		if !ok {
			return "", fmt.Errorf("no Go type for %s", fd.Message().FullName())
		}
		return "*" + gf.QualifiedGoIdent(m.GoIdent), nil
	default:
		return "", fmt.Errorf("no Go type for map component of kind %s", fd.Kind())
	}
}

// modifiers is the chained calls after a field constructor.
//
// The rule that matters most: HasDefault becomes .Optional() and never
// .Default(...). Emitting a default would make the column NOT NULL DEFAULT ”,
// which changes the shape of every column that carries `default: ""` -- and in
// the schema this was measured against that is most of them.
func modifiers(en *schema.Entity, f *schema.Field) string {
	var out []string

	if f == en.Key() {
		// A key is unique and immutable and nothing else. It carries
		// `default: ""` like the others and does not get .Optional().
		out = append(out, "Unique()", "Immutable()")
		return chain(out)
	}

	if f.IsUnique() {
		out = append(out, "Unique()")
	}
	// A JSON column is a pointer or a map already, so ent refuses Nillable on
	// it: there is nothing to make nil-able.
	if f.IsNullable() && f.Type() != schema.TypeJSON {
		out = append(out, "Nillable()")
	}
	if f.IsImmutable() {
		out = append(out, "Immutable()")
	}
	if f.IsOptional() {
		out = append(out, "Optional()")
	}
	return chain(out)
}

// chain renders modifier calls as ent's own formatting does: one per line,
// indented, with the dot leading. gofmt would not produce this on its own, and
// protogen runs format.Source over the result, so the shape survives only
// because it is already valid.
func chain(calls []string) string {
	if len(calls) == 0 {
		return ","
	}
	var b strings.Builder
	for _, c := range calls {
		b.WriteString(".\n\t\t\t")
		b.WriteString(c)
	}
	b.WriteString(",")
	return b.String()
}

func (e *emitter) emitEdges(gf *protogen.GeneratedFile, en *schema.Entity) {
	gf.P("func (", en.Name(), ") Edges() []", gf.QualifiedGoIdent(entPkg.Ident("Edge")), " {")
	gf.P("\treturn []", gf.QualifiedGoIdent(entPkg.Ident("Edge")), "{")

	for _, ed := range en.Edges() {
		var calls []string
		if !ed.IsList() {
			calls = append(calls, "Unique()")
		}
		if !ed.IsNullable() {
			calls = append(calls, "Required()")
		}
		if ed.IsImmutable() {
			calls = append(calls, "Immutable()")
		}

		target := "<unresolved>"
		if ed.Target() != nil {
			target = ed.Target().Name()
		}
		gf.P("\t\t", gf.QualifiedGoIdent(edgePkg.Ident("To")),
			"(", strconvQuote(string(ed.Name())), ", ", target, ".Type)", chain(calls))
	}

	gf.P("\t}")
	gf.P("}")
	gf.P()
}

// emitIndexes writes the index set.
//
// Fields come first and edges second, each in declared order. That is ent's API
// shape rather than the schema's, and it is what decides the column order of
// the physical index: robot_alias_robot_tenant is (alias, robot_tenant).
func (e *emitter) emitIndexes(gf *protogen.GeneratedFile, en *schema.Entity) error {
	gf.P("func (", en.Name(), ") Indexes() []", gf.QualifiedGoIdent(entPkg.Ident("Index")), " {")
	gf.P("\treturn []", gf.QualifiedGoIdent(entPkg.Ident("Index")), "{")

	for _, idx := range en.Indexes() {
		var fields, edges []string
		for _, p := range idx.Props() {
			switch m := p.(type) {
			case *schema.Field:
				fields = append(fields, strconvQuote(string(m.Name())))
			case *schema.Edge:
				edges = append(edges, strconvQuote(string(m.Name())))
			}
		}
		if len(fields) == 0 && len(edges) == 0 {
			return fmt.Errorf("go: %s: index %s has no members", en.FullName(), idx.Name())
		}

		var head string
		var calls []string
		if len(fields) > 0 {
			head = fmt.Sprintf("%s(%s)",
				gf.QualifiedGoIdent(indexPkg.Ident("Fields")), strings.Join(fields, ", "))
			if len(edges) > 0 {
				calls = append(calls, fmt.Sprintf("Edges(%s)", strings.Join(edges, ", ")))
			}
		} else {
			head = fmt.Sprintf("%s(%s)",
				gf.QualifiedGoIdent(indexPkg.Ident("Edges")), strings.Join(edges, ", "))
		}
		if idx.IsUnique() {
			calls = append(calls, "Unique()")
		}

		gf.P("\t\t", head, chain(calls))
	}

	gf.P("\t}")
	gf.P("}")
	gf.P()
	return nil
}

// emitAnnotations pins the physical table name.
//
// Without it ent would name the table by its own default, which is a
// snake-cased plural: `pose_presets` rather than `posepreset`. The annotation
// is the difference between regenerating a schema and renaming every table in
// the database.
func (e *emitter) emitAnnotations(gf *protogen.GeneratedFile, en *schema.Entity) {
	gf.P("func (", en.Name(), ") Annotations() []", gf.QualifiedGoIdent(entschemaPkg.Ident("Annotation")), " {")
	gf.P("\treturn []", gf.QualifiedGoIdent(entschemaPkg.Ident("Annotation")), "{")
	gf.P("\t\t", gf.QualifiedGoIdent(entsqlPkg.Ident("Annotation")),
		"{Table: ", strconvQuote(e.backend.TableName(en)), "},")
	gf.P("\t}")
	gf.P("}")
	gf.P()
}

func strconvQuote(s string) string { return `"` + s + `"` }

func lastSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
