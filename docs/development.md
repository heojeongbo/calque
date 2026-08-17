# Development

```sh
go test ./...
test -z "$(gofmt -l .)" && go vet ./...

cd ts && pnpm -r typecheck && pnpm -r test && pnpm -r build
```

That is the whole suite. There is no protoc in the test path and no buf:
`internal/protoc` compiles `.proto` source in process with
[`bufbuild/protocompile`](https://github.com/bufbuild/protocompile), so a
checkout with a Go toolchain can run everything.

One thing in there looks unnecessary and is not. `protoc.Request` marshals the
request and unmarshals it again before handing it over. In process, an extension
resolved against a compiled-in type arrives as a typed value; over the wire from
real protoc it arrives as `dynamicpb`. Round-tripping makes the test path
identical to production — and without it a path that worked in tests panicked in
the field, once.

## What the tests are for

### Fixtures

`testdata/proto/valid/` is the corpus everything downstream runs against.
`apptest.proto` is deliberately shaped like a real schema: a uuid key, a required
edge, a nullable edge, a map, a version field, and a unique index spanning a
field and an edge. `apptest_svc.proto` is the service contract another plugin
would generate — calque does not emit it, but the TypeScript target reads it.
`erased.proto` and `naming.proto` cover soft delete and the case where a proto
name and a JSON name differ.

`testdata/proto/invalid/` is thirty files, each named after the mistake it makes.
A test asserts that the corpus and the table of expected messages are the same
set, so neither can drift: adding a fixture without an expectation fails, and so
does the reverse. The table is reproduced in
[docs/annotations.md](annotations.md).

### Golden output

`target/ts/golden_test.go` and `target/gotarget/golden_test.go` compile the valid
corpus and compare byte for byte against `testdata/golden/` beside each.

```sh
UPDATE_GOLDEN=1 go test ./target/...
```

Read the diff before committing it. The whole value of a golden test is that it
notices things you did not mean — the Go one found two bugs the first time it
ran, both conditions that happened to be equivalent on the private tree the
reference test reads.

### Policy tests

`internal/policy` checks the shape of the repository rather than the behaviour of
any one part of it.

**`TestNoPanicInGenerationPaths`** walks the AST of every package outside
`ormopt/` (generated) and `tools/` (developer commands) and fails on a call to
`panic`. The point is not that panics are ugly: a type switch with a panicking
default keeps compiling when a variant is added, which is when it stops being
right. `VisitElem` and `ElemVisitor` exist so that situation is a build failure;
this test is what stops someone reaching for the panic anyway.

It has already caught a real one, in `gen/registry.go`, and the fix was to the
code rather than to the exemption list.

**`TestGeneratedCodeIsMarked`** checks that every committed `.pb.go` says it is
generated, since the point of committing generated code is that a reader can
tell.

### Drift tests

`internal/entname` holds a copy of ent's acronym list — the thing that makes
`device_id` into `DeviceID` rather than `DeviceId`. A test parses ent's own
`func.go` out of the module cache and fails if the two have diverged, so the copy
cannot quietly go stale under a dependency bump.

### Reference tests

The gate that matters most cannot live in the repository, because it is a
private proto tree and a private output tree. So it is environment-gated and
skips when unset:

```sh
CALQUE_REFERENCE_PROTO=~/app/proto \
CALQUE_REFERENCE_OUTPUT=~/app/src \
go test ./target/ts/ -run Reference -v
```

| variable | |
|---|---|
| `CALQUE_REFERENCE_PROTO` | the proto root to compile |
| `CALQUE_REFERENCE_OUTPUT` | the committed TypeScript to compare against |
| `CALQUE_REFERENCE_GO` | the committed Go to compare against |
| `CALQUE_REFERENCE_INCLUDE` | extra import paths, colon-separated |
| `CALQUE_REFERENCE_PARAM` | the plugin parameter string, for the Go target's protogen flags |

Nothing is copied in. `ormcompat/reference_test.go` checks that the annotations
parse; `target/ts/reference_test.go` and `target/gotarget/reference_test.go`
check that the output is byte-identical.

This is where several of the interesting bugs came from. A fixture cannot tell
you that services are ordered by dependency rather than by path, that `queries`
is in service order while `db.g.ts` is in entity order, that an `rpc` entry is
four lines rather than one, or that Go output is per proto file rather than per
entity — all four were found by pointing the generator at a tree that had 13
entities in 13 files and comparing.

The goldens catch the opposite kind. A real tree is uniform in ways a fixture
does not have to be, and a condition that is equivalent on it stays wrong
quietly: every entity there has a service and every edge target declares
`patch`, so two conditions that are not the same condition looked like they
were. Run both.

## Layout

| | |
|---|---|
| `proto/calque/orm/` | the vendored annotation vocabulary |
| `ormopt/` | its generated Go, committed |
| `ormcompat/` | annotations to schema specs |
| `schema/` | the neutral IR: entities, props, indexes, names, diagnostics |
| `query/` | the plan AST and `Derive`. Not yet called by any target |
| `gen/` | the generator core: config, `Target`, `Backend`, capabilities, output |
| `backend/dexie/`, `backend/entsql/` | storage decisions |
| `target/ts/`, `target/gotarget/` | emission |
| `internal/tsw/` | a forty-line line writer |
| `internal/entname/` | ent's casing, copied and pinned |
| `internal/protoc/` | in-process proto compilation, for tests |
| `plugin/` | the protoc-plugin boundary |
| `ts/` | the two npm runtime packages |

## Conventions

**No formatter in the emission path.** The TypeScript target writes strings,
because reproducing an existing generator byte for byte means reproducing the
trailing space in `export type Db = ` and the three consecutive blank lines a
real committed file has. A formatter would normalise those away, and then you
could no longer prove a swap is a no-op. The Go target uses
`protogen.GeneratedFile`, where gofmt output *is* the target bytes.

**Diagnostics are about the schema.** Lower case, no trailing punctuation, and
they talk about what the user wrote rather than about calque. A hint, when there
is one, says what to do.

**Commit messages say why.** The diff already says what.

## Releasing

The npm packages are published from `ts/`, with `publishConfig.registry` pinned
to `registry.npmjs.org` so a publish cannot silently land in a private registry
a developer happens to be logged into:

```sh
cd ts
pnpm -r build
pnpm release:runtime:dry     # then without :dry
pnpm release:dexie:dry
```

The plugin itself is a Go module; a release is a tag.
