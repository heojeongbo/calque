# Proto conventions

[Annotations](annotations.md) is the vocabulary — what `(orm.field)` may say.
This is the shape: how to lay a `.proto` tree out so calque can read it.

Some of what follows is a hard requirement that fails loudly, some is a
requirement that fails *quietly*, and some is only a habit that has worked. They
are marked, because the difference matters more than the advice.

## The contract calque looks up by name

calque does not compute the names of the request and reference messages. It
reads them off the descriptors, which is the whole reason a prop's proto name and
its JSON name cannot get mixed up (see [conformance item 4](conformance.md)). The
cost of that choice is a naming contract: **these names are load-bearing.**

| calque looks for | if it is missing |
|---|---|
| `<Entity>Ref` | error — `no <E>Ref message; calque reads the lookup names off it rather than computing them` |
| `<Entity>GetRequest` | error — `no <E>GetRequest message` |
| a field on `<Entity>GetRequest` whose type is `<Entity>Ref` | error — `<E>GetRequest has no <E>Ref field` |
| a field on `<Entity>GetRequest` whose type is `<Entity>Select` | **silent** — the select helpers are skipped |
| `<Entity>Service` | **silent** — no server and no table are emitted for that entity |
| `<version>_force` on the patch request | error — `has a version field but <X>PatchRequest has no <v>/<v>_force` |
| `<field>_null` on the patch request, per nullable prop | **silent** — the clear branch is skipped |
| methods named `Add`, `Get`, `Patch`, `Erase` | **silent** — that operation is not emitted |

The silent ones are the ones to watch. An entity whose service is named anything
other than `<Entity>Service` is modelled, validated, and then simply not served —
no error, no output, nothing to notice.

### The `Ref` oneof

calque does not look this oneof up by name. It takes the message's **only** oneof,
and a `Ref` carrying two is refused by the Go target and warned about by the
TypeScript one, because which of them holds the lookups would be a guess.

What the name *does* do is appear in the generated code, in both languages:

```go
switch x.WhichKey() {        // oneof key   → WhichKey
switch x.WhichLookup() {     // oneof lookup → WhichLookup
```

```ts
if(req.ref?.key === undefined) …      // oneof key
if(req.ref?.lookup === undefined) …   // oneof lookup
```

So it is free to choose and not free to change: renaming it regenerates every
place that dereferences it, which is correct and is a diff. Both targets derive it
from the descriptor, so they cannot disagree — `testdata/proto/refoneof` is a copy
of the fixture contract with the oneof renamed, and the tests assert exactly that.

`key` is what the deployment this was measured against uses, and what calque falls
back to when there is no `Ref` message to read.

### And what is *not* name-bound

Read off the descriptor by type or position, so you may name these whatever you
like:

- the wrapper message for a non-field key (`<Entity>RefBySlug` and friends) — it
  is found as the message type of the oneof member
- the `ref` and `select` field names on the get request — found by type

## The patch companions, and why they exist

proto cannot tell "leave this alone" from "set this to nothing". Under implicit
presence an absent field and a field set to its zero value are the same bytes. So
a patch request needs a second bit per nullable prop:

```proto
message RobotPatchRequest {
  RobotRef ref = 1;
  optional string name = 5;
  bool name_null = 105;          // "set it to nothing", vs. name being absent
}
```

The version field needs its own companion for a different reason — `_force`
drops the compare-and-swap clause, so a caller who means to overwrite regardless
can say so:

```proto
  google.protobuf.Timestamp date_updated = 14;
  bool date_updated_force = 114;
```

Both are looked up by name, `<field>_null` and `<version>_force`.

## File layout

**One entity per file, named after the entity in snake_case.** Output is per
*proto file*, not per entity: `pose_preset.proto` produces `pose_preset.go` even
though the entity is `PosePreset`.

Two entities in one file is allowed and produces one output file containing both.
That works, and it means the output file is named after neither of them — which
is why one-per-file is the convention.

The service contract belongs in a sibling file, and calque can write it: the
[service target](targets/service.md) emits `<Entity>Ref`, `<Entity>Service` and the
four request messages from the same annotations. The other two targets read that
file rather than assuming it, so a hand-written one works just as well — which is
what the fixtures in `testdata/proto/valid` are.

## File preamble

```proto
edition = "2023";

package apptest;

import "google/protobuf/timestamp.proto";
import "orm.proto";

option features.field_presence = IMPLICIT;
option go_package = "example.com/apptest/oas";
```

- **`import "orm.proto"`** — the vocabulary. calque carries its own copy under
  package `calque.orm` with upstream's extension numbers, and extensions resolve
  by number, so this line does not change whichever copy is on the path.
- **`option go_package`** — required by the Go target, on *every* file in the
  closure. That is protogen's rule; see [the Go target](targets/go.md).
- **`features.field_presence = IMPLICIT`** — not cosmetic. Presence decides
  nullability, so this is a schema decision, not a style one.

## Presence is load-bearing

Under `IMPLICIT`, a scalar has no presence and is therefore not nullable. To make
one genuinely nullable, say so:

```proto
  string lock = 8 [
    features.field_presence = EXPLICIT,
    (orm.field) = {nullable: true}
  ];
```

Two rules follow from this and are worth keeping in mind while writing:

- **A timestamp is never nullable from presence alone.** Every message field has
  presence and `google.protobuf.Timestamp` is a message, so without an exception
  every timestamp in every schema would be nullable and no annotation could say
  otherwise. Mark it `nullable: true` if you mean it.
- **An edge ignores presence entirely.** Only `(orm.edge) = {nullable: true}` or
  the `optional` keyword makes one nullable. The rules differ on purpose: a
  message field's presence says something about the value, an edge's says
  something about the reference.

## Field numbering — a habit, not a rule

calque enforces nothing here. This is what the deployment it was measured against
does, and it has held up:

| range | what |
|---|---|
| `1` | the key |
| `2`–`3` | edges |
| `4`–`9` | scalars, then a `labels` map |
| `10`–`13` | domain-specific messages and nullable timestamps |
| `14` | the version field |
| `15` | the created-at timestamp |

Keeping the version and created-at fields at the top of the range means adding a
column never disturbs them, and a reader can find them in any entity without
looking. It also keeps everything inside the one-byte tag range.

## Two orderings, and they are not the same

This one has cost time, so it is written down rather than left to be rediscovered.

An entity's lookups have two orders:

- **`Entity.Keys()`** — every unique prop first, in field-number order, then every
  unique index in declaration order.
- **the `<Entity>Ref` oneof** — by field number, counting an index as its first
  member's number.

They coincide until an entity has a unique field numbered *after* an index's
first member. Then they diverge, and each governs different output: the generated
`Ref()`, `Picks()` and constructors follow `Keys()`, while the oneof case labels
necessarily follow the message.

A schema that keeps its unique fields numbered before its indexed edges never
sees the difference. One that does not will still generate correctly — calque
reads the order from each source rather than assuming they agree — but the two
orders will visibly differ in the output, and that is expected rather than a bug.

## A complete file

`testdata/proto/valid/apptest.proto` is the corpus every downstream test runs
against, and it is deliberately shaped like a real schema: a uuid key, a required
edge, a nullable edge, a map, a version field, and a unique index spanning a
field and an edge. `naming.proto` beside it covers the case where a prop's proto
name and JSON name differ; `erased.proto` covers soft delete and a unique edge
used as a lookup key.

Reading those three is faster than reading this page, and they cannot go stale —
the tests fail if they do.
