# The service target

Emits the `.proto` service contract the other two targets read. It is the odd one
out twice over: its output is another generator's input, and it describes what may
be *asked for* rather than where anything is kept — so it takes no backend.

```yaml
version: 1
targets:
  - target: service     # no `backend`
    out: proto.svc
```

Leaving `backend` out is not an omission to be forgiven; naming one is an error.
See [storeless targets](../configuration.md#storeless-targets).

## What it emits

One file per **source file**, named after it: `pose_preset.proto` produces
`pose_preset_svc.g.proto` beside it. Two entities in one proto produce one contract
carrying both, which is why the convention is one entity per file
([conventions](../conventions.md#file-layout)).

Per entity, a service and six messages, in this order:

```proto
service <E>Service {
	// Add creates a new <E>
	rpc Add(<E>AddRequest) returns (<E>);
	// Get retrieves a <E>
	rpc Get(<E>GetRequest) returns (<E>);
	// Patch updates an existing <E>
	rpc Patch(<E>PatchRequest) returns (<E>);
	// Erase deletes a <E>
	rpc Erase(<E>Ref) returns (google.protobuf.Empty);
}

message <E>AddRequest    { … }
message <E>RefBy<Index>  { … }   // one per unique index
message <E>Ref           { oneof key { … } }
message <E>Select        { … }
message <E>GetRequest    { <E>Ref ref = 1; <E>Select select = 2; }
message <E>PatchRequest  { … }
```

The wrappers precede the `Ref` that names them, which is the order the generator
this replaces completed them in.

### And what it does **not** emit

`<E>Filter`, `<E>ListRequest`, `<E>ListResponse`, `<E>WatchRequest`,
`<E>WatchResponse`, `<E>WatchItem`, `<E>Event`, `<E>ScrapeRequest`,
`<E>ScrapeResponse`.

None of them are in the annotation vocabulary: `orm.RpcOptions` knows `crud` and
`add`/`get`/`patch`/`erase`, and nothing else. A contract that carries a `List` or
a `Watch` got it **by hand**, and this target has to leave room for that rather
than own the file — see [Two passes](#two-passes-and-hand-written-extensions).

## Which declarations exist

| declaration | emitted when |
|---|---|
| `service <E>Service` | always, even with no operations — the TypeScript target reads it to decide there is a table |
| `<E>AddRequest` | `add` |
| `<E>Ref`, `<E>RefBy<Index>` | any of `get`, `patch`, `erase` |
| `<E>Select`, `<E>GetRequest` | `get` |
| `<E>PatchRequest` | `patch` |

`rpc: {crud: true}` turns on all four; `rpc: {get: {}}` turns on one.

## Which props appear, and where

| | in `AddRequest` | in `Select` | in `PatchRequest` |
|---|---|---|---|
| the key | yes | replaced by `bool all` | replaced by `ref` |
| the version field | no — the store sets it | yes, as `bool` | yes, plus `<v>_force` |
| an immutable prop or edge | yes | yes | no |
| `(orm.field) = {disabled: true}` | no | no | no |
| a nullable prop | yes | yes | yes, plus `<p>_null` |

An edge becomes `<Target>Ref` in `AddRequest` and `PatchRequest`, and
`<Target>Select` in `Select`. **A message-typed field with no `(orm.edge)` is not an
edge** — it keeps its own type, so one annotation is the difference between
`TenantRef tenant = 2` and `Tenant tenant = 2`.

## Field numbers

Not chosen so much as derived, and each derivation is a promise to code somebody
else wrote:

| message | numbering |
|---|---|
| `AddRequest`, `Select` | the entity's own field numbers |
| `GetRequest` | `ref = 1`, `select = 2` |
| `PatchRequest` | `2n-1` for the value, `2n` for its companion; `ref` at the key's number |
| `Ref` oneof member | the lookup's number — an index reports its **first member's** |

`ref` can take the key's number because the key is immutable by construction and
therefore never patched. The doubling is checked: a companion landing in proto's
reserved `19000`–`19999`, or past the maximum field number, is an error naming the
prop, because neither protoc nor a reader would explain it in terms of this target.

### The `Ref` oneof is in field-number order

`Entity.Keys()` is every unique prop and then every unique index; this oneof is
sorted by number. They coincide until an entity has a unique field numbered after
an index's first member, and then they do not.

**Both orders matter and neither may be changed to match the other.** This one is
the wire contract. `Keys()` order is what the generated query code and a Dexie
schema string use, and a stored Dexie schema cannot be reordered without a version
bump. See [conventions](../conventions.md#the-ref-oneof).

### `RefBy<Index>` members are in annotation order

Not number order. An index declared `refs: [alias(4), tenant(2)]` produces

```proto
message <E>RefBySlug {
	string alias = 4;
	TenantRef tenant = 2;
}
```

because the annotation is where a composite key gets its meaning. A one-member
index still gets a wrapper.

The message name is `RefBy` plus the index's name in PascalCase, with **no
initialism folding** — an index called `by_email` produces `RefByByEmail`. That is
not [`internal/entname`](../development.md)'s rule, and the two must not be
conflated: this one names a proto message that hand-written code refers to.

## Presence

The emitted file does **not** repeat the source's file-level
`features.field_presence = IMPLICIT`. Instead, a field in `AddRequest` that has no
presence carries it per-field:

```proto
	bytes trace_id = 8 [
		features.field_presence = IMPLICIT
	];
```

It is on a scalar that is not a list, not a map, and not optional — where optional
means nullable, or defaulted, or repeated. A message never carries it, because
presence is inherent to one. Getting this wrong does not fail: the field silently
gains presence it does not have, and an unset value stops being the default.

## Two passes, and hand-written extensions

This target's output is `protoc-gen-go`'s and `protoc-gen-es`'s input, so a build
runs twice. That is not new machinery to write — the pipeline in the tree this was
measured against already has the shape, and it is the shape to copy:

1. Generate contracts into a **staging** directory, not the tracked tree.
2. Merge each with its hand-written sibling — the `List`, `Watch` and `Scrape`
   declarations, which this target does not produce — into the tracked tree.
3. Run the language plugins over the result.

Step 2 is what makes it safe for this target not to own the whole file. Do not
point `out` at the tracked tree if anything in it was written by hand.

## Configuration

| | default | |
|---|---|---|
| `header` | `// Code generated by calque. DO NOT EDIT.` | first line of every file |
| `suffix` | `_svc.g.proto` | replaces the source's `.proto` |

`header` exists for the same reason the other targets' does: proving the swap is a
no-op means generating what the previous generator generated, and its first line
named itself. Set it to that plugin's line while you diff, then leave it alone.

`option go_package` is carried through from the source, read off the descriptor —
which matters in a buf build, because managed mode rewrites that option before a
plugin sees it. A source with none is **not** refused: a proto tree generated only
for TypeScript has no reason to carry a Go import path, and demanding one here
would impose [protogen's cost](../architecture.md#protogen-is-a-cost-the-go-target-pays-alone)
on a target that does not need it. The line is omitted and the omission is reported.

## Known gaps

- **Soft delete is passed through, not honoured.** An `erased: {}` field appears in
  `AddRequest` as the ordinary nullable timestamp it looks like, so a caller can
  create an already-erased row. That is what the generator being replaced does — it
  has no `erased` in its vocabulary at all — and nothing else in calque implements
  soft delete either, so leaving it is the choice that makes no claim. It will need
  one when something does.
- **The reserved-range check is on the companion only.** A prop numbered such that
  `2n-1` collides with another prop's `2n` is possible in principle and is not
  detected; protoc would refuse the result, naming the field but not the rule.
- **Two entities in one file share one output file**, which is then named after
  neither of them. This target does not warn about it; the convention that avoids it
  is [one entity per file](../conventions.md#file-layout).
- **A comment in the source is not carried over.** The generator being replaced does
  not either, so a contract has only the four rpc comments this target writes.
