# Extending

Two things can be added: a **target**, which is a language, and a **backend**,
which is a store. They are separate because the same language can speak to more
than one store, and the same store can be spoken to from more than one language.

Both are registered at composition time, in `main.go`:

```go
func registry() *gen.Registry {
	return gen.NewRegistry().
		Target(ts.New()).
		Target(gotarget.New()).
		Backend(dexie.New()).
		Backend(entsql.New())
}
```

There is no config-driven loading of third-party plugins. Go's runtime plugin
support is Linux-only and requires an identical toolchain and identical build
flags, which is a worse contract than a twenty-five line `main` that imports what
it wants and calls `Serve`. Fork, add a line, build.

## Adding a backend

A backend contributes no syntax. It contributes what the store can do, what a
value has to look like to be stored, and where a value lives in a record.

```go
type Backend interface {
	Name() string
	Capabilities() Capabilities
	Configure(cfg *Config, section string) error
	Strict() bool
	StorePath(p schema.Prop) (schema.StorePath, error)
	Codec(p schema.Prop) (CodecName, error)
	Lower(s *schema.Schema) (*Lowered, error)
}
```

### `Name`

The value of `backend:` in the config, and the key of this backend's options
section. Lowercase, starting with a letter.

### `Capabilities`

Facts about the store, in the terms a schema is written in.

| field | asserts |
|---|---|
| `UniqueCompoundIndex` | uniqueness across more than one property is enforced |
| `PartialIndex` | only the rows matching a predicate can be indexed — what a unique index on a softly-erasing entity needs |
| `NestedKeyPath` | a path into a nested object can be indexed |
| `BinaryKey` | a byte string can be a key or an index member |
| `Transactions` | a read and a dependent write can be made atomic |
| `MaxIndexArity` | the widest compound index; `0` means no limit |
| `Codecs` | every `CodecName` this backend's runtime implements |

Answer honestly. These are what let calque refuse at generate time rather than
ship something that quietly does something else — the entry that earns the whole
type is `UniqueCompoundIndex`, because a document store answering `true` would
produce an index that exists and is not unique, and nothing would say so.

Codecs are named by the backend and implemented by the runtime adapter:
`identity`, `uuid-string`, `uuid-bytes`, `time-epoch-millis`, `time-native`,
`json`. Naming one you do not implement is an error at generate time, and the
message says it is a calque bug — because it is.

### `Strict`

Whether a capability shortfall stops generation. `true` for a store meant to
hold what the schema says. `false` only while reproducing an older generator, and
then temporarily; see [Migrating](migrating.md).

### `Accepts` — optional, and per kind

`Strict` is all-or-nothing, which is too coarse for a store that is a cache: a
unique compound index the store of record enforces is a real constraint, and a
backend that cannot mirror it should be able to say so forever without also
accepting the next shortfall of some other kind.

```go
func (b *Backend) Accepts(kind gen.ShortfallKind) bool
```

Implement it and `gen.Run` asks per kind instead of consulting `Strict`. Do not
implement it and nothing changes. It is not on `gen.Backend` for the same reason
`EntIdent` is not: a store with no opinion should not have to answer.

The kinds are `unique_compound_index`, `partial_index`, `index_arity`,
`unorderable_index_member`, `binary_key` and `no_transactions`
(`gen.AllShortfallKinds`). An accepted shortfall is still reported on stderr on
every build.

### `Configure`

```go
func (b *Backend) Configure(cfg *gen.Config, section string) error {
	var o Options
	if _, err := cfg.Section(section, &o); err != nil {
		return err
	}
	o.setDefaults()
	b.opts = o
	return nil
}
```

`Section` decodes strictly, so a typo inside your section is loud. Not finding
the section is not an error — it means nobody configured you, and your defaults
stand.

### `StorePath` and `Codec`

Where a prop lives in a record, and what transform its value goes through.

```go
func (b *Backend) StorePath(p schema.Prop) (schema.StorePath, error) {
	return schema.VisitProp[schema.StorePath](p, pathVisitor{})
}

type pathVisitor struct{}

func (pathVisitor) VisitField(f *schema.Field) (schema.StorePath, error) {
	return schema.StorePath{schema.StoreName(f.Name())}, nil
}

func (pathVisitor) VisitEdge(e *schema.Edge) (schema.StorePath, error) {
	// A store with no nested paths flattens the edge into its own column.
	return schema.StorePath{schema.StoreName(string(e.Name()) + "_id")}, nil
}
```

Use the visitor rather than a type switch. A new prop variant then fails to
compile here, which is the whole reason the interface is sealed.

Return a `StorePath` — a sequence — rather than a string. Whether
`{"tenant", "id"}` renders as `tenant.id` or `tenant_id` is yours to decide when
you render it, and keeping it a sequence is what lets a second backend decide
differently.

### `Lower`

Turn the validated neutral schema into storage decisions: one `Table` per
entity, holding a codec for every prop and whatever else your runtime adapter
needs, in `Extra`.

`Table` is deliberately narrow, and it was not always. It used to carry a table
name, a primary key, a list of indexes and a path per prop, modelled neutrally
so a target could read them without knowing the store. No target ever did — the
questions worth asking turned out to be store-specific, and they are asked the
way [target-specific backend questions](#target-specific-backend-questions)
describes. So do not populate a neutral description of your store for a reader
that does not exist; expose what your target needs as a method on your backend,
where it can have the right type.

`Codec` is the one the core reads: `gen.Run` checks every entry against your
`Capabilities`, and naming one you do not implement is an error saying it is a
calque bug.

Cover **`s.Entities()`**, not `s.Sources()`. An entity reachable only as an edge
target is not emitted, but a target still asks what its key looks like.

Capability checking has already run, so an error here is a bug or a
schema-specific limit your capability set was too coarse to state. Report it;
do not panic.

### Register it

```go
Backend(mystore.New())
```

And a target has to accept it — add the name to that target's `Backends()`, or
have it return `nil`, which means any. Note that `nil` and `[]string{}` are
different: an empty slice accepts nothing.

## Adding a target

```go
type Target interface {
	Name() string
	Backends() []string
	Configure(cfg *Config, section string) error
	Emit(g *Generator) ([]File, error)
}
```

`Backends()` is the set of stores this target's runtime actually has an adapter
for. It is checked before anything is configured, so pairing a language with a
store it cannot speak to fails naming both — rather than emitting a package that
imports an adapter nobody wrote.

### A target with no store

Some output describes what may be asked for rather than where anything is kept —
a service contract, a client, a document. Such a target declares one more method
and takes no `backend`:

```go
// Storeless says this target emits nothing store-specific.
func (t *Target) Storeless() {}
```

`gen.Run` then skips backend resolution, capability checking and lowering for it,
and `Lowered()`, `Table()` and `Backend()` on its `Generator` are nil — `Table()`
says so rather than dereferencing one. Naming a `backend` for a storeless target
is an error: it would claim the output depends on a store when it does not.

It is an optional interface rather than a method on `Target` for the same reason
`ShortfallAccepter` is one on the backend side — a method every implementation
stubs out with `false` is not information.

`Emit` returns files whose names are paths relative to the plugin's output root.
Do **not** apply the entry's `out` yourself; `gen.Run` does it. Two targets
claiming the same path is an error naming both, not last-one-wins.

### What `Generator` gives you

| | |
|---|---|
| `Sources()` | the entities to emit, in stable order |
| `Schema()` | every entity, including ones reachable only as an edge target |
| `Table(e)` | the backend's decisions for one entity — its codecs, and its `Extra` |
| `Lowered()` | all of them |
| `Backend()` | the paired backend, for a target-specific question |
| `Entry()` | this target's own config entry — its label, and its `out` |
| `Request()`, `Files()` | descriptors, for facts the schema does not carry |
| `Warnf(...)` | something worth saying that is not worth stopping for |
| `Diag()` | a schema problem only this target can see |
| `Step(done, total, what)` | progress, for a target with enough entities that one line at the end is not enough |

`Request()` and `Files()` **must not be used for names**. `schema.Names` is the
only source of those, and mixing the two is exactly the bug that motivated the
name types.

`Warnf` goes to stderr and cannot be silenced. `Diag()` is collected and becomes
an error at the end of the run — note *at the end*, after every file has been
rendered, so a target diagnostic fails the run rather than stopping the work.

### Reading the schema

```go
for _, e := range g.Sources() {
	e.FullName()      // "apptest.User"
	e.Key()           // the primary key; never nil after Build
	e.Version()       // optimistic-lock field, or nil
	e.Erased()        // soft-delete stamp, or nil
	e.Props()         // fields and edges, in field-number order
	e.Keys()          // every unique Elem — the lookups this entity supports
	e.Rpc().Has(schema.OpPatch)

	for _, p := range e.Props() {
		p.Names().Proto   // "device_id"  — identity, diagnostics, config
		p.Names().Value   // "deviceId"   — what a decoded message calls it
		p.Type()
		p.IsNullable()
		p.IsOptional()
	}
}
```

`Keys()` is the one to reach for when generating lookups: it is every unique
element, which is precisely the set of ways a row can be found, and it excludes
indexes marked `hidden`.

### Target-specific backend questions

If your target only works with one backend, ask it directly:

```go
backend, ok := g.Backend().(*mystore.Backend)
if !ok {
	return nil, fmt.Errorf("mylang: backend %q is not mystore", g.Backend().Name())
}
```

The assertion cannot fail — `Backends()` is enforced before `Emit` — but the
check says what went wrong if it ever does.

This is the right shape for a decision only one store can make. Do not push it
onto `gen.Backend`: a method every implementation stubs out makes "a second
backend" a claim rather than a fact.

## Emitting

There is no formatter and no template engine. `internal/tsw` is about forty
lines: a `P()` that writes a line and tracks indentation. Both targets build
strings.

That is deliberate. calque's first job is to reproduce an existing generator's
output byte for byte, and a formatter would normalise away the trailing space in
`export type Db = ` and the two blank lines that a real committed file has. Once
you cannot produce a specific byte, you cannot prove the swap is a no-op.

The Go target uses `protogen.GeneratedFile`, which handles import management and
runs gofmt — appropriate there, because gofmt output *is* the specific bytes.

## Testing it

Point the golden tests at your target. `target/ts/golden_test.go` compiles
`testdata/proto/valid/*.proto` in process and compares against committed output;
`UPDATE_GOLDEN=1 go test ./...` rewrites it.

There is no constructor for a `Generator`, on purpose: a target's test builds a
registry with one entry and calls `gen.Run`, the same way a build does.

```go
s, files, err := protoc.ParseFiles(req)
out, err := gen.Run(s, cfg, gen.NewRegistry().Target(mylang.New()).Backend(mystore.New()),
	gen.WithDescriptors(req, files))
```

Handing a target a `Generator` you assembled yourself would skip resolution,
configuration and the unclaimed-section check — which is to say it would test
the target against a state `Run` never produces.

If you are replacing an existing generator, add a reference test like
`target/ts/reference_test.go`: it takes a proto root and an expected output tree
from environment variables and diffs every file, and skips when they are unset.
That keeps a private tree out of the repository while still gating on it.

## Reading the schema from your own plugin

Not every use of calque is a calque target. A plugin that already generates
something of its own — an OpenAPI document, a client, a migration — may want
calque's model of the schema without calque's output: the entities, their props,
edges and indexes, with the annotations read and validated.

`ormcompat` is that entry point, and it has two forms because there are two
things a plugin might be holding.

```go
// If you have the raw request:
s, err := ormcompat.Parse(req)              // req *pluginpb.CodeGeneratorRequest

// If you have already built a protogen.Plugin, which most Go plugins have:
s, err := ormcompat.ParseProtogen(p)        // p *protogen.Plugin
```

Both return the same `*schema.Schema` for the same input, and a test asserts it
(`ormcompat/protogen_test.go`) — including that a schema one refuses is refused
by the other with the same message. Which you call is about what you have, never
about what you get.

The split exists because of the `go_package` cost described in
[Architecture](architecture.md#protogen-is-a-cost-the-go-target-pays-alone).
`ParseProtogen` does not impose it: a caller who built a `protogen.Plugin` has
already paid it. `Parse` is for a caller who has not, and a proto tree generated
only for TypeScript is a real example of one.

From there you are in `schema`, which is documented in
[Reading the schema](#reading-the-schema) above — the same API a target sees, and
the same guarantee that `Sources()` is what to emit and `Schema()` is what to
understand.
