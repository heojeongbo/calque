package gotarget

import (
	"fmt"
	"path"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/gofeaturespb"

	"github.com/heojeongbo/calque/schema"
)

var timestamppbPkg = protogen.GoImportPath("google.golang.org/protobuf/types/known/timestamppb")

// emitEntProto writes `func (e *X) Proto() *oas.X` for every entity.
//
// It is the one direction ent generates: an entity read out of the database
// becomes the proto message a server returns. The reverse is written out
// field-by-field inside Add and Patch, because those have to distinguish a
// value that was supplied from one that was not, and a conversion function
// cannot.
//
// This is also the clearest place the two naming systems meet. The same field
// is `e.TraceID` to ent and `x.SetTraceId(...)` to protoc-gen-go, and getting
// either wrong is a compile error naming a symbol that looks almost right.
func (e *emitter) emitEntProto() error {
	type group struct {
		path     string
		source   string
		entities []*schema.Entity
	}
	var groups []*group
	byPath := map[string]*group{}

	for _, en := range e.gen.Sources() {
		p, err := e.entPath(en)
		if err != nil {
			return err
		}
		g, ok := byPath[p]
		if !ok {
			g = &group{path: p, source: en.File()}
			byPath[p] = g
			groups = append(groups, g)
		}
		g.entities = append(g.entities, en)
	}

	for _, grp := range groups {
		dir := strings.TrimSuffix(grp.path, "/"+lastSegment(grp.path))
		gf := e.pg.NewGeneratedFile(grp.path, protogen.GoImportPath(dir))
		gf.P(e.target.opts.Header)
		gf.P("// source: ", grp.source)
		gf.P()
		gf.P("package ", lastSegment(dir))
		gf.P()

		for _, en := range grp.entities {
			if err := e.emitProtoFunc(gf, en); err != nil {
				return err
			}
		}
	}
	return nil
}

// entPath is where an entity's conversion helper goes: the ent package, named
// after the proto file.
func (e *emitter) entPath(en *schema.Entity) (string, error) {
	f, ok := e.file(en)
	if !ok {
		return "", fmt.Errorf("go: %s: no protogen file for %s", en.FullName(), en.File())
	}
	base := strings.TrimSuffix(path.Base(en.File()), ".proto")
	return path.Join(string(f.GoImportPath), e.target.opts.EntDir, base+".g.go"), nil
}

func (e *emitter) emitProtoFunc(gf *protogen.GeneratedFile, en *schema.Entity) error {
	msg, ok := e.message(protoreflect.FullName(en.FullName()))
	if !ok {
		return fmt.Errorf("go: %s: no Go type", en.FullName())
	}
	// The opaque and hybrid APIs both accept SetX; only the open API does not,
	// and there the emitted code would be a compile error naming a method that
	// does not exist. Saying so here names the flag instead.
	if msg.APILevel == gofeaturespb.GoFeatures_API_OPEN {
		return fmt.Errorf(
			"go: %s uses the open protobuf API; calque emits the opaque form (SetX, X_builder)\n"+
				"\tpass default_api_level=API_OPAQUE, the same value protoc-gen-go is given",
			en.FullName())
	}

	protoType := gf.QualifiedGoIdent(msg.GoIdent)

	gf.P("func (e *", en.Name(), ") Proto() *", protoType, " {")
	gf.P("\tx := &", protoType, "{}")

	for _, p := range en.Props() {
		line, err := e.protoAssign(gf, en, p)
		if err != nil {
			return fmt.Errorf("go: %s.%s: %w", en.FullName(), p.Name(), err)
		}
		gf.P(line)
	}

	gf.P("\treturn x")
	gf.P("}")
	gf.P()
	return nil
}

// protoAssign is the line that copies one prop out of the ent entity.
func (e *emitter) protoAssign(gf *protogen.GeneratedFile, en *schema.Entity, p schema.Prop) (string, error) {
	setter, err := e.setterName(en, p)
	if err != nil {
		return "", err
	}
	entField := e.backend.EntIdent(p)

	if edge, ok := p.(*schema.Edge); ok {
		// An edge is loaded separately, so it is only there when the query
		// asked for it. An absent one leaves the proto field unset rather than
		// setting an empty message.
		_ = edge
		return fmt.Sprintf("\tif v := e.Edges.%s; v != nil {\n\t\tx.%s(v.Proto())\n\t}",
			entField, setter), nil
	}

	f, ok := p.(*schema.Field)
	if !ok {
		return "", fmt.Errorf("prop is neither a field nor an edge")
	}

	// The key is a uuid array, and the proto field is bytes.
	if f == en.Key() && f.Type() == schema.TypeUUID {
		return fmt.Sprintf("\tx.%s(e.%s[:])", setter, entField), nil
	}
	if f.Type() == schema.TypeUUID {
		if nillable(f) {
			return fmt.Sprintf("\tif e.%s != nil {\n\t\tx.%s(e.%s[:])\n\t}", entField, setter, entField), nil
		}
		return fmt.Sprintf("\tx.%s(e.%s[:])", setter, entField), nil
	}

	// A json column typed by a message is a Go pointer, so it is nil when the
	// row did not carry one — regardless of whether the schema called it
	// nullable.
	if f.Type() == schema.TypeJSON && !isMap(f) {
		return fmt.Sprintf("\tif e.%s != nil {\n\t\tx.%s(e.%s)\n\t}", entField, setter, entField), nil
	}

	if f.Type() == schema.TypeTime {
		ts := gf.QualifiedGoIdent(timestamppbPkg.Ident("New"))
		if nillable(f) {
			return fmt.Sprintf("\tif e.%s != nil {\n\t\tx.%s(%s(*e.%s))\n\t}", entField, setter, ts, entField), nil
		}
		return fmt.Sprintf("\tx.%s(%s(e.%s))", setter, ts, entField), nil
	}

	if nillable(f) {
		return fmt.Sprintf("\tif e.%s != nil {\n\t\tx.%s(*e.%s)\n\t}", entField, setter, entField), nil
	}
	return fmt.Sprintf("\tx.%s(e.%s)", setter, entField), nil
}

// nillable reports whether ent gave the field a pointer.
//
// It is the same condition the schema emitter uses for .Nillable(), and the two
// have to agree: a pointer here and a value there is a compile error, and the
// other way round silently dereferences nothing.
func nillable(f *schema.Field) bool {
	return f.IsNullable() && f.Type() != schema.TypeJSON
}

func isMap(f *schema.Field) bool {
	fd := f.Source()
	return fd != nil && fd.IsMap()
}

// setterName is protoc-gen-go's setter for a prop.
//
// It comes from protogen rather than from a casing function, because
// protoc-gen-go's Go name is the one the message actually has — and the
// disagreement between it and ent's is exactly where a hand-written mapping
// would go wrong.
func (e *emitter) setterName(en *schema.Entity, p schema.Prop) (string, error) {
	msg, ok := e.message(protoreflect.FullName(en.FullName()))
	if !ok {
		return "", fmt.Errorf("no Go type for %s", en.FullName())
	}
	for _, f := range msg.Fields {
		if f.Desc.Name() == protoreflect.Name(p.Name()) {
			return "Set" + f.GoName, nil
		}
	}
	return "", fmt.Errorf("no Go field for %s", p.Name())
}
