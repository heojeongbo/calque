# The Go target

Emits [ent](https://entgo.io) schemas and the code around them, over SQL.

```yaml
targets:
  - {target: go, backend: entsql, out: go}
```

**Status: complete.** Against the real proto tree it replaces, it produces 42
of 42 files byte-identical — and the seven hand-written servers in that tree
compile against the generated code unchanged, with both of its test suites
passing.

## Setup

This target needs the same flags protoc-gen-go was given, because it builds its
view of Go names from the same parameter string:

```yaml
plugins:
  - local: [protoc-gen-go]
    out: gen
    opt: [module=example.com/app, default_api_level=API_OPAQUE]
  - local: [go, run, github.com/heojeongbo/calque@latest]
    out: gen
    opt: [config=calque.yaml, module=example.com/app, default_api_level=API_OPAQUE]
```

Two requirements follow:

- **Every file in the closure needs a `go_package`.** That is protogen's rule,
  not calque's, and it is why only this target uses protogen.
- **`default_api_level=API_OPAQUE`.** calque emits the opaque form — `SetX`,
  `X_builder`. Under the open API those methods do not exist, so the emitted
  code would fail to compile against a method that was never generated. calque
  refuses up front instead, naming the flag.

## What it emits

Per proto file, not per entity — `pose_preset.proto` produces `pose_preset.go`,
whatever the entities inside are called.

| file | |
|---|---|
| `<pkg>/schema/<protofile>.go` | the ent schema types |
| `<pkg>/ent/<protofile>.g.go` | `func (e *X) Proto() *pb.X` |
| `<pkg>/query.g.go` | `Ref`, `Pick`, `Picks`, `WithSelect`, and the `XByY` constructors |
| `<pkg>/store.g.go` | one `Server` and one `Client` over every service |
| `<pkg>/server/bare/<protofile>.g.go` | the server: `Add`, `Get`, `Patch`, `Erase`, and the helpers they need |
| `<pkg>/server/bare/store.g.go` | a `Server` that constructs all of them from one `*ent.Client` |

The conversion helpers have to live in the ent package, because they are methods
on ent's own generated types.

### The ent schema

```go
func (PosePreset) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Unique().
			Immutable(),
		field.String("name").
			Optional(),
		field.JSON("joints", map[string]float32{}).
			Optional(),
		field.Time("date_created").
			Immutable().
			Optional(),
	}
}

func (PosePreset) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("holder", Holder.Type).
			Unique().
			Required().
			Immutable(),
	}
}
```

Getting these right pins every column, index and foreign key in the physical
database, because ent expands them into the rest. Which is also why it is
checkable on its own, before a line of server code exists: run `ent generate`
and diff `migrate/schema.go`.

Three details that look like they could be improved and cannot:

- **A default becomes `.Optional()`, not `.Default(...)`.** Changing it would
  make every column with a default `NOT NULL`.
- **`entsql.Annotation{Table: ...}` is always emitted**, with the entity name
  lower-cased whole. ent's own default is a snake-cased plural — `pose_presets`
  — so without the annotation the physical table renames itself.
- **An index is `.Fields(...).Edges(...)`**, fields first, whatever order they
  were declared in. That is ent's API, not a sort.

### `Proto()`

```go
func (e *PosePreset) Proto() *oas.PosePreset {
	x := &oas.PosePreset{}
	x.SetId(e.ID[:])
	if v := e.Edges.Holder; v != nil {
		x.SetHolder(v.Proto())
	}
	x.SetName(e.Name)
	x.SetJoints(e.Joints)
	x.SetDateCreated(timestamppb.New(e.DateCreated))
	return x
}
```

One direction only. The reverse is written out field by field inside `Add` and
`Patch`, because those have to tell a value that was supplied from one that was
not, and a function taking a whole entity cannot.

Four shapes, each for a reason:

| | |
|---|---|
| an edge | checked for nil — it is loaded separately and is only there when the query asked for it |
| a `json` column typed by a message | checked for nil — its Go type is a pointer regardless of what the schema said |
| a nullable field | dereferenced |
| a uuid key | sliced — ent holds an array, the proto field is bytes |

## Two naming systems

This is the sharpest thing about the target. ent and protoc-gen-go disagree
about the same field, and they disagree exactly on initialisms:

| the proto says | ent calls it | protoc-gen-go calls it |
|---|---|---|
| `id` | `ID` | `Id` |
| `device_id` | `DeviceID` | `DeviceId` |
| `trace_id` | `TraceID` | `TraceId` |
| `name` | `Name` | `Name` |

So every generated line spells the field twice, from two different sources:
`internal/entname` for the ent side, protogen for the proto side. A hand-written
mapping would be right on most fields and wrong on exactly the ones with an
initialism in them.

`internal/entname` is a copy of ent's acronym list. A test parses ent's own
`func.go` out of the module cache and fails if the two have drifted, so the copy
cannot quietly go stale.

## Runtime

There is no calque runtime for Go, and nothing to install. The generated code
imports only what it stores into and speaks over:

| | |
|---|---|
| the standard library | `bytes`, `context`, `time` |
| [ent](https://entgo.io) | `entgo.io/ent` and its `schema`, `dialect/entsql`, `dialect/sql/sqlgraph` packages |
| gRPC | `google.golang.org/grpc`, and `codes`/`status` in the servers |
| protobuf | `protojson`, `timestamppb` |
| uuid | `github.com/google/uuid`, for a uuid key |
| your own package | the `.pb.go` types and ent's generated client |

That list is the complete set across the golden corpus, taken from it rather than
written from memory. A table in prose goes stale silently, so here is how it was
got:

```sh
grep -rhoE '^\t[a-z0-9_]* "[^"]+"' target/gotarget/testdata/golden/ |
	sed 's/.*"\(.*\)"/\1/' | sort -u
```

**ent is the runtime.** The TypeScript target needs
[`@heojeongbo/calque-dexie`](typescript.md#runtime) because Dexie is a thin index
API with no idea what a message is, so something has to hold the dehydrate,
hydrate, compare and reconcile logic. ent already generates a typed client per
entity, so the equivalent code has somewhere to be: it is emitted per entity —
`Proto()` in the ent package, and the helpers inside each bare server — rather
than shared in a library.

### What follows from that

**There is no version contract on this side.** The only way generated Go changes
is regeneration, so the generator's version and the emitted code cannot disagree.

The TypeScript side has a second axis: generated code and the runtime package have
to agree, and a runtime released separately can change behaviour under output that
did not move. `TableBase._reconcile` is the worked example — see
[conformance item 5](../conformance.md) and the version note on
[`TableBase`](typescript.md#tablebase).

Neither arrangement is better in the abstract. It is worth knowing which one you
are in, because it decides whether upgrading calque can change anything without a
regeneration. Here it cannot.

## Configuration

| key | default | |
|---|---|---|
| `schema_dir` | `schema` | where ent schema types go, under the proto's Go package |
| `ent_dir` | `ent` | where conversion helpers go |
| `bare_dir` | `server/bare` | where the generated servers go |
| `header` | `// Code generated by calque. DO NOT EDIT.` | first line of every file |
| `query_header` | same as `header` | first line of `query.g.go` and `store.g.go` |

And on the backend:

| key | default | values |
|---|---|---|
| `dialect` | `sqlite` | `sqlite`, `postgres`, `mysql` |
| `table` | | entity full name to physical table name |

`query_header` exists only for a drop-in: the two file groups came from two
different upstream generators, and a swap that changes line 1 of every file is
not a swap. Set only `header` if you are not replacing anything.

`dialect` is a capability question. MySQL has no partial index, so an entity that
erases softly cannot have a unique index covering only its live rows, and calque
will say so rather than create one that also covers the erased. Postgres caps a
compound index at 32 members.

## Optimistic locking

A field marked `version: {}` turns `Patch` into a compare-and-swap, and the
check folds into the same statement as the write:

```go
	is_force := req.GetDateUpdatedForce()
	if !req.HasDateUpdated() && !is_force {
		return nil, status.Errorf(codes.InvalidArgument, "version not given: %s", "date_updated")
	}
	...
	q := s.Db.Robot.Update().Where(p)
	if !is_force {
		q.Where(robot.DateUpdatedEQ(req.GetDateUpdated().AsTime()))
	}
```

so the read and the write are one round trip and nothing needs a transaction to
be atomic. Zero rows affected then means either the row is gone or someone else
got there first, and the two error codes distinguish them: `NotFound` under
`_force`, `FailedPrecondition` otherwise.

`<field>_force` drops the clause. If the caller also supplies a version, that
value is written rather than a fresh stamp — which is what makes restoring a
known state reproducible.

## Nullable props need two bits

proto cannot tell "leave it alone" from "set it to nothing", so every nullable
prop has a `<field>_null` companion in the patch request:

```go
	if req.GetProjectNull() {
		q.ClearProject()
	} else if req.HasProject() {
		...
	}
```

## Known gaps

- **`Erase` returns `(nil, nil)`** — a nil pointer where the signature promises
  a message. grpc marshals it as an empty message; a caller dereferencing it
  does not survive.
- **Soft delete is untested.** No entity in the measured schema carries
  `erased: {}`, so `Erase` is a hard delete everywhere it has been exercised.
- **A uuid field that is not the key is refused** in `Add` and `Patch`, because
  converting one needs a statement rather than an expression and nothing in the
  measured schema has one. It says so rather than emitting something untested.
