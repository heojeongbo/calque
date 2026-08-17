package gotarget

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/heojeongbo/calque/schema"
)

var grpcPkg = protogen.GoImportPath("google.golang.org/grpc")

// emitStore writes store.g.go: one name for the whole set of services, on both
// sides of the wire.
//
// Six blocks over one list, and the list is the entities in source order. It is
// the only file here that is not per-entity, which is the point of it -- a
// caller that wants "the server" should not have to know how many services
// there are.
func (e *emitter) emitStore() error {
	sources := e.gen.Sources()
	if len(sources) == 0 {
		return nil
	}

	// A service per entity, named for it. An entity without one is not a
	// failure -- it is an entity nothing serves -- but it cannot be in a
	// registry of servers either.
	type svc struct {
		name string // the entity's name, which is what the accessor is called
		desc *protogen.Service
	}
	var svcs []svc
	for _, en := range sources {
		s, ok := e.service(en)
		if !ok {
			continue
		}
		svcs = append(svcs, svc{name: en.Name(), desc: s})
	}
	if len(svcs) == 0 {
		return nil
	}

	f, ok := e.file(sources[0])
	if !ok {
		return fmt.Errorf("go: %s: no protogen file", sources[0].FullName())
	}
	dir := string(f.GoImportPath)
	gf := e.pg.NewGeneratedFile(dir+"/store.g.go", f.GoImportPath)
	gf.P(e.target.opts.QueryHeader)
	gf.P()
	gf.P("package ", lastSegment(dir))
	gf.P()

	// The generated names are protoc-gen-go-grpc's, derived from the service's
	// own Go name rather than from the entity's. They coincide here and there is
	// no reason to assume they always will.
	server := func(s svc) string { return s.desc.GoName + "Server" }
	client := func(s svc) string { return s.desc.GoName + "Client" }

	gf.P("type Server interface {")
	for _, s := range svcs {
		gf.P("\t", s.name, "() ", server(s))
	}
	gf.P("}")
	gf.P()

	grpcServer := gf.QualifiedGoIdent(grpcPkg.Ident("Server"))
	gf.P("func RegisterServer(g *", grpcServer, ", s Server) {")
	for _, s := range svcs {
		gf.P("\tRegister", server(s), "(g, s.", s.name, "())")
	}
	gf.P("}")
	gf.P()

	// Two structs with identical fields and different methods. They are not one
	// struct with a flag: Unimplemented answers with grpc's own stub so a
	// partial server compiles, Static answers with what it was given so a test
	// can supply three services and nothing else.
	gf.P("type UnimplementedServer struct {")
	for _, s := range svcs {
		gf.P("\t", s.name, "Server ", server(s))
	}
	gf.P("}")
	gf.P()

	for _, s := range svcs {
		gf.P("func (UnimplementedServer) ", s.name, "() ", server(s), " { return Unimplemented", server(s), "{} }")
	}
	gf.P()

	gf.P("type StaticServer struct {")
	for _, s := range svcs {
		gf.P("\t", s.name, "Server ", server(s))
	}
	gf.P("}")
	gf.P()

	for _, s := range svcs {
		gf.P("func (s *StaticServer) ", s.name, "() ", server(s), " { return s.", s.name, "Server }")
	}
	gf.P()

	gf.P("type Client interface {")
	for _, s := range svcs {
		gf.P("\t", s.name, "() ", client(s))
	}
	gf.P("}")
	gf.P()

	grpcConn := gf.QualifiedGoIdent(grpcPkg.Ident("ClientConn"))
	gf.P("func NewClient(c *", grpcConn, ") Client {")
	gf.P("\treturn &client{")
	for _, s := range svcs {
		gf.P("\t\t_", s.name, ": New", client(s), "(c),")
	}
	gf.P("\t}")
	gf.P("}")
	gf.P()

	gf.P("type client struct {")
	for _, s := range svcs {
		gf.P("\t_", s.name, " ", client(s))
	}
	gf.P("}")
	gf.P()

	for _, s := range svcs {
		gf.P("func (c *client) ", s.name, "() ", client(s), " { return c._", s.name, " }")
	}
	return nil
}

// service finds the service that serves an entity, by the name the convention
// gives it.
func (e *emitter) service(en *schema.Entity) (*protogen.Service, bool) {
	want := protoreflect.FullName(en.FullName() + "Service")
	for _, f := range e.pg.Files {
		for _, s := range f.Services {
			if s.Desc.FullName() == want {
				return s, true
			}
		}
	}
	return nil, false
}
