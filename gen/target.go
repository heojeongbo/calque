package gen

import (
	"fmt"
	"io"

	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/HeoJeongBo/calque/schema"
)

// Target is a language calque emits.
//
// It sees a validated schema, the storage decisions the chosen backend made,
// and its own config section; it returns files whose names are paths relative
// to the plugin's output root.
type Target interface {
	// Name is the value of `target:` in the config, and the key of this
	// target's own options section.
	Name() string

	// Backends is the set of backend names this target's runtime has an
	// adapter for. Nil means any.
	//
	// A config pairing a target with a backend it does not list is an error
	// naming both and listing what it does support. This is the seam that stops
	// someone selecting ts+entsql and getting a package that imports an adapter
	// nobody wrote.
	Backends() []string

	// Configure claims this target's config section and validates it. Called
	// once, before Emit, so a bad option fails before any file is rendered.
	Configure(cfg *Config, section string) error

	// Emit renders the target's files.
	Emit(g *Generator) ([]File, error)
}

// File is one emitted file.
type File struct {
	// Name is a slash-separated path relative to the plugin's output root. It
	// must not be absolute and must not contain "..".
	Name string
	Body []byte
}

// Generator is everything a Target may read.
//
// It is deliberately narrow. A target that needs something not here should be
// adding it here, where every target gets it, rather than reaching into
// descriptors — which is how the predecessor ended up with two different ideas
// of what a field is called.
type Generator struct {
	schema  *schema.Schema
	lowered *Lowered
	backend Backend
	config  *Config
	entry   TargetConfig
	diags   *schema.Diagnostics

	req   *pluginpb.CodeGeneratorRequest
	files *protoregistry.Files
	warn  io.Writer

	progress *Progress
	label    string
}

// Step reports movement inside a target, for one with enough entities that a
// single line at the end is not enough to tell a slow run from a stuck one.
//
// A target that never calls it still gets its own line when it finishes.
func (g *Generator) Step(done, total int, what string) {
	if g.progress == nil {
		return
	}
	g.progress.Step(g.label, done, total, what)
}

// Request is the plugin request this generation came from.
//
// It is here for a target that needs a descriptor fact the schema deliberately
// does not carry — the TypeScript target reads each entity's service and Ref
// message, and the Go target hands the whole request to protogen for Go
// identifiers and import paths.
//
// It must not be used for names. schema.Names is the only source of those, and
// mixing the two is the device_id bug.
func (g *Generator) Request() *pluginpb.CodeGeneratorRequest { return g.req }

// Files is the resolved descriptor set, for looking up a message or service by
// name.
func (g *Generator) Files() *protoregistry.Files { return g.files }

// Warnf reports something worth saying that is not worth stopping for. buf
// shows a plugin's stderr, so this reaches the person running the build.
func (g *Generator) Warnf(format string, a ...any) {
	if g.warn == nil {
		return
	}
	fmt.Fprintf(g.warn, "calque: "+format+"\n", a...)
}

// NewGenerator builds the view a target is given. It is exported for targets'
// own tests; the plugin builds one per configured target.
func NewGenerator(s *schema.Schema, l *Lowered, b Backend, cfg *Config, entry TargetConfig, diags *schema.Diagnostics) *Generator {
	return &Generator{schema: s, lowered: l, backend: b, config: cfg, entry: entry, diags: diags}
}

// Schema is every entity, including ones reachable only as an edge target.
func (g *Generator) Schema() *schema.Schema { return g.schema }

// Sources is the entities whose file was in files-to-generate, in stable order.
//
// An entity reachable only through an edge is in Schema and not here: it has to
// be understood for the edge to mean anything, and emitting it would make two
// runs both claim its file.
func (g *Generator) Sources() []*schema.Entity { return g.schema.Sources() }

// Lowered is the storage decisions the chosen backend made.
func (g *Generator) Lowered() *Lowered { return g.lowered }

// Table is the lowering for one entity.
func (g *Generator) Table(e *schema.Entity) (*Table, error) { return g.lowered.Table(e) }

// Backend is the backend this target was paired with.
func (g *Generator) Backend() Backend { return g.backend }

// Config is the whole config, for a target that needs to claim its section.
func (g *Generator) Config() *Config { return g.config }

// Entry is this target's own config entry.
func (g *Generator) Entry() TargetConfig { return g.entry }

// Diag is where a target reports a problem it can see and the core cannot.
func (g *Generator) Diag() *schema.Diagnostics { return g.diags }

// Out resolves a path against the target's configured `out` subdirectory.
func (g *Generator) Out(rel string) string {
	if g.entry.Out == "" {
		return rel
	}
	return g.entry.Out + "/" + rel
}
