# Architecture

How a `CodeGeneratorRequest` becomes files, and why the seams are where they
are. Most of what follows is a decision with a cost; where the cost was paid by
the generator calque replaces, it is named.

## The pipeline

```
CodeGeneratorRequest
  │
  ├─ plugin/       parse opt=
  ├─ config/       read calque.yaml, record the overrides
  │
  ├─ ormcompat/    read (orm.*) annotations → schema specs   ─┐ everything about
  │                                                            │ annotations,
  ├─ schema.Build  resolve edges, indexes, keys              ─┘ then everything
  │                                                            about structure
  ├─ gen.Run
  │    ├─ resolve every target and backend name
  │    ├─ let each claim its config section
  │    ├─ reject sections nobody claimed
  │    ├─ per target:
  │    │    ├─ CheckCapabilities  → refuse, or warn
  │    │    ├─ Backend.Lower      → codecs
  │    │    └─ Target.Emit        → files
  │    └─ collect diagnostics
  │
CodeGeneratorResponse
```

The order in `gen.Run` is the point rather than an implementation detail. Every
name is resolved before anything is configured, so a typo in the last entry is
reported before the first one has done any work; every option is claimed before
anything is lowered, so a bad option fails before a file exists.

## Everything is told, nothing is discovered

Three things follow from one commitment: a mistake should be a message, and a
message should come as early as it can.

**Errors are collected, not raised.** A file with four problems reports four
problems. `schema.Diagnostics` accumulates `path: message` with an optional
hint, and validation runs in two passes — everything about annotations, and only
if that passes, everything about structure — so an error about a broken
reference is never noise from an annotation that was going to be rejected
anyway.

**Nothing panics.** A test walks the AST of every package outside the generated
and developer-tools directories and fails on a call to `panic`. The predecessor
had two, both spelled `default: panic("unimplemented: key type not Field")`,
guarding a type switch over the same three-variant union; both were reached by
the same input, a unique edge used as a lookup key. One was fixed. The other
still crashes that generator today.

**A schema error is not a crash.** The plugin's exit code is narrower than it
looks: a schema you got wrong is `response.error` with exit 0, which buf prints
and fails the build over. A non-zero exit is reserved for not being able to read
the request or write the response — the only failure buf cannot explain on
calque's behalf.

## Names have types

The single largest source of bugs in the generator calque replaces was that a
thing in a schema has more than one name and the code had one word for all of
them. There were two functions computing "the name" — `strcase.ToCamel` in one
app, a hand-rolled `camel()` in another — and neither asked the descriptor. A
field written `device_id` is `deviceId` on a decoded message, so the generated
code read `v.DeviceId`, wrote `w.device_id`, and indexed a third spelling.
Nothing failed. The descriptor just quietly read `undefined`.

The fix is not a better casing function:

| type | is | comes from |
|---|---|---|
| `ProtoName` | `device_id` | the `.proto`. A prop's identity; the only name stable across languages, so diagnostics and config use it — and generated code that touches a value never should |
| `ValueName` | `deviceId` | the descriptor's `JSONName()`. **calque does not compute it.** There is no casing function in the repository that returns one |
| `StoreName` | whatever a row calls it | the backend, not the schema. Nothing on `Prop` returns one; `Backend.StorePath` does |
| `StorePath` | `{"tenant", "id"}` | a sequence, until a backend renders it — `tenant.id` in a store that indexes into nested objects, `tenant_id` in one that does not |

Putting one where another belongs does not compile. There is deliberately no
`Name() string` on `Prop`: a caller has to say which world it is naming into, and
the compiler checks the answer.

`StorePath` staying a sequence is also what makes a second backend possible. The
predecessor joined it with a dot in the schema builder and again in the query
builder, which hardcoded one store's path syntax in two places.

## Adding a variant is a compile error

`Elem` — a field, an edge, or an index — is a sealed interface: it has an
unexported method, so a fourth variant can only be added in the `schema`
package. That makes `ElemVisitor` a total function:

```go
type ElemVisitor[R any] interface {
	VisitField(*Field) (R, error)
	VisitEdge(*Edge) (R, error)
	VisitIndex(*Index) (R, error)
}
```

Add a variant and every visitor fails to compile. A type switch with a
panicking default keeps compiling — which is to say the compiler stops helping
exactly when the code stops being right.

It has to be an interface rather than a struct of function fields: a keyed
struct literal still compiles when a field is added, and the missing one is nil
at run time.

`Prop` — a field or an edge — has its own visitor with two arms. An index is an
`Elem` and not a `Prop`, which is the difference between reading a value and
looking a row up.

## Facts and policy are different questions

A backend answers two things that look alike and are not.

`Capabilities` states **facts**: does this store enforce uniqueness across more
than one property, can it index a path into a nested object, can a byte string be
a key, does it have transactions, how wide can a compound index be, which codecs
does its runtime implement. Every entry is there because some schema is
expressible in proto and not expressible in some store.

`Strict()` states **policy**: does a shortfall stop generation. It is separate
because a backend reproducing an older generator bug for bug has to keep
generating — otherwise adopting calque on an existing schema produces nothing at
all, which is not a migration anyone can perform. It answers true once the
constraint is meant to be real.

So `CheckCapabilities` never downgrades and never warns; it returns everything
it found. `gen.Run` decides what that means.

One check is not subject to policy. If a backend's lowering names a codec its
own capabilities do not list, that is an error regardless — and the message ends
`(this is a calque bug)`, because it is.

## Where each thing is allowed to look

`Generator` is what a target may read, and it is deliberately narrow. A target
that needs something not on it should be adding it there, where every target
gets it, rather than reaching into descriptors — which is how the predecessor
ended up with two ideas of what a field is called.

`Request()` and `Files()` are on it, for descriptor facts the schema
deliberately does not carry: the TypeScript target reads each entity's service
and `Ref` message, and the Go target hands the whole request to protogen. They
**must not be used for names**.

`Sources()` is what to emit — the entities whose file was in files-to-generate.
`Schema()` is everything, including entities reachable only as an edge target,
which have to be understood for the edge to mean anything but must not be
emitted or two runs would both claim the same file.

## protogen is a cost the Go target pays alone

`protogen.Options.New` fails on any file in the closure without a `go_package`.
A proto tree generated only for TypeScript has no reason to carry one, so
`ormcompat` reads descriptors directly.

The Go target has every reason: it needs Go identifiers, Go import paths, and
the api-level flags protoc-gen-go was given. Deriving those independently would
mean agreeing with protoc-gen-go by imitation, and disagreeing silently. So it
uses protogen, and the cost lands on the target that benefits.

Which is also why `ormcompat` has a second entry point. A plugin that wants
calque's schema and nothing else calls `Parse`; one that has already built a
`protogen.Plugin` — and therefore already paid — calls `ParseProtogen`. Same
schema either way; see [Extending](extending.md#reading-the-schema-from-your-own-plugin).

## The vocabulary is vendored by number

calque carries its own copy of the `orm.*` options in package `calque.orm`, with
upstream's extension numbers. Extensions resolve by number, so a schema still
says `import "orm.proto"` and `(orm.field)`, and calque does not depend on the
upstream module to read them.

What that would otherwise hide is caught explicitly: an option carrying a field
this calque does not know is an error saying the schema was written against a
newer `buf.build/orm/orm`, rather than an annotation silently doing nothing.

## Tests compile protos in process

`internal/protoc` compiles `.proto` source with `bufbuild/protocompile`, so
there is no protoc and no buf in the test path and `go test ./...` is the whole
suite.

It does one thing that looks unnecessary and is not: it marshals the request and
unmarshals it again before use. In process, an extension resolved against a
compiled-in type arrives as a typed value; over the wire from real protoc it
arrives as `dynamicpb`. Round-tripping makes the test path identical to
production. Without it, a code path that works in tests can panic in the field —
which it did, once.

## Seams that exist and are not yet wired

Stated because a reader will find them and assume they do something.

- **`Table.Extra`** is a JSON-able bag for anything a runtime adapter needs that
  the neutral schema cannot say. Both backends fill it. **No target reads it** —
  both call concrete backend methods instead.

There were three more, and they are gone rather than listed. `query.Derive` was
a plan AST turning an entity into its lookups, arguments, guards and
assignments, with tests and no callers. `(orm.edge)`'s `bind:` was parsed onto
`EdgeSpec` and read by nothing — it still parses, because a schema that carries
one is not wrong, but it is not carried any further. `Index.Refs()`,
`StorePath.Equal()` and `tsw.Raw()` were written for a caller who never came.

A seam that has been unwired long enough to be documented as unwired is not a
seam; it is a design nobody has had to keep honest. What conformance item 2
needs is still needed, and it gets built against the target that consumes it.

## Target-specific backend extensions

A target and its backend are paired by `Target.Backends()`, checked before
anything is configured. So the Go target can assert its backend is
`*entsql.Backend` and ask it ent-specific questions — the table name it decided,
the Go identifier ent will give a prop — and the TypeScript target can ask
`*dexie.Backend` for a schema string.

These are not on `gen.Backend`, and the reason is that ent's API is per-entity
generated types: `predicate.Robot` and `predicate.Run` are unrelated named types
with no common method set, and nothing abstracts over them. Putting `EntIdent`
on the shared interface would mean every backend answering a question only one
can, which makes "a second backend" a stub rather than a fact.
