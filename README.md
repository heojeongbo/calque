# calque

calque generates ORM code from orm-annotated `.proto` files, for more than one
target language and more than one storage backend, from one configuration.

It is a protoc plugin. buf runs it; you do not.

```yaml
# buf.gen.yaml
version: v2
plugins:
  - local: [go, run, github.com/heojeongbo/calque@latest]
    out: gen
    opt: [config=calque.yaml]
```

```yaml
# calque.yaml
version: 1
targets:
  - {target: ts, backend: dexie, out: ts}
  - {target: go, backend: entsql, out: go}
```

The schema is the proto. A field says how it is stored, a message says what
operations it has, and both targets read the same answer:

```proto
message User {
  bytes id = 1 [(orm.field) = {type: TYPE_UUID key: true default: ""}];
  Tenant tenant = 2 [(orm.edge) = {}];
  string alias = 4 [(orm.field) = {default: ""}];
  google.protobuf.Timestamp date_updated = 14 [(orm.field) = {version: {}}];

  option (orm.message) = {
    rpc: {crud: true}
    indexes: [{
      name: "slug"
      refs: [{name: "alias" number: 4}, {name: "tenant" number: 2}]
      unique: true
    }]
  };
}
```

## Why

Two generators already existed for this annotation vocabulary — one for Go on
ent and SQL, one for TypeScript on Dexie and IndexedDB — and in a production
deployment they are driven from the same proto tree. Which means where they
disagree is observable, and they disagree in seven measured ways: different
entity counts from the same schema, different operations from the same
`crud: true`, a unique constraint one store holds and the other silently drops,
an index declared under one spelling and queried under another.

[docs/conformance.md](docs/conformance.md) is the list, with the evidence. It is
calque's acceptance criteria rather than a wish list.

The short version of what changes here: names have types, so putting a store key
where a message property belongs does not compile; a backend states what it can
do and generation stops when the schema asks for more; and what a target may
read is a narrow interface rather than the descriptor tree, so two targets
cannot end up with two ideas of what a field is called.

## Status

calque is being built against a real deployment, and each part is finished when
it reproduces that deployment's committed output byte for byte. That is a
stricter gate than a test suite: it means the generated code can be swapped in
and the diff is empty.

| | |
|---|---|
| **TypeScript**, on Dexie | **complete** — 28 of 28 files byte-identical |
| **Go**, on ent and SQL | **complete** — 42 of 42 files byte-identical, and the replaced tree's own test suites pass against the generated code |
| **the service contract**, as `.proto` | **complete** — the input the other two read, and it takes no backend ([why](docs/targets/service.md)) |

The two npm runtime packages are published from [`ts/`](ts). There is no Go
equivalent and none is needed — ent already is that layer, so the generated Go
depends on calque for nothing at run time
([why](docs/targets/go.md#runtime)).

`go test ./...` is the whole suite: protos are compiled in process, so there is
no protoc and no buf in the test path.

The generator itself runs on the standard library and `google.golang.org/protobuf`,
with `goccy/go-yaml` in one package for the config file. The command tree adds
`lesomnus/xli` and `lesomnus/z`, and they are confined to
[`cmd/`](cmd) — calque is a build-time plugin, so what it imports lands in the
go.mod of every repository that generates with it, and the extension surface
should not make anyone take a CLI framework along. A test enforces that rather
than a convention.

## Beyond the plugin

buf runs calque with no arguments. A person can ask it three other things:

```sh
calque config       # read calque.yaml and say what this build makes of it
calque targets      # the targets and backends this binary knows
calque version      # which calque this is
```

`calque config` is the interesting one: it resolves and configures everything
the file names without generating, so every error a build can hit before it
touches a proto is reachable without buf and without a proto tree — and it
prints the *effective* settings, defaults and `CALQUE_*` variables included.
See [the CLI](docs/cli.md).

## Documentation

| | |
|---|---|
| [Annotations](docs/annotations.md) | what you write in a `.proto`, what is inferred when you write nothing, and every validation error |
| [Proto conventions](docs/conventions.md) | how to lay a proto tree out — the names calque looks up, and which ones fail quietly |
| [Configuration](docs/configuration.md) | `calque.yaml`, the plugin's `opt=` parameters and the `CALQUE_*` environment, in full |
| [The command line](docs/cli.md) | what a person can ask calque, and why buf asks it nothing |
| [Architecture](docs/architecture.md) | how a request becomes files, and why the seams are where they are |
| [Extending](docs/extending.md) | adding a target language or a storage backend |
| [Service target](docs/targets/service.md) | the `.proto` contract the other two read |
| [TypeScript target](docs/targets/typescript.md) | what it emits and what it needs at runtime |
| [Go target](docs/targets/go.md) | what it emits today, and what it does not yet |
| [Migrating](docs/migrating.md) | coming from `protoc-gen-orm-ts` or `protoc-gen-orm-ent` |
| [Conformance](docs/conformance.md) | what two targets have to agree about, measured |
| [Versioning](docs/versioning.md) | three version numbers that are not the same number, and which one can change behaviour without a regeneration |
| [Development](docs/development.md) | running the tests, and what each one is for |

## License

MIT. See [LICENSE](LICENSE).
