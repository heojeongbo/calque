# Annotations

What you write in a `.proto` to describe a schema, what calque infers when you
write nothing, and what it refuses.

This is the vocabulary. For the shape — file layout, the message names calque
looks up, the patch companions — see [Proto conventions](conventions.md).

The vocabulary is [`buf.build/orm/orm`](https://buf.build/orm/orm)'s. calque
carries its own copy under `proto/calque/orm/`, in package `calque.orm`, with
the same extension numbers — and extensions resolve by number, so your files
still say `import "orm.proto"` and `(orm.field)`. A schema written against a
*newer* upstream is not silently accepted: an option carrying a field this
calque does not know is an error saying so.

```proto
extend google.protobuf.MessageOptions { MessageOptions message = 45001; }
extend google.protobuf.FieldOptions   { FieldOptions   field   = 45101;
                                        EdgeOptions    edge    = 45102; }
```

## An entity

A message is an entity when it carries `option (orm.message)`. A message that
does not is not an entity, and that is not an error — it is how a nested value
type is spelled.

```proto
message User {
  bytes id = 1 [(orm.field) = {type: TYPE_UUID key: true default: ""}];
  option (orm.message) = {rpc: {crud: true}};
}
```

Every entity needs exactly one key. A message with `(orm.message)` and no
`key: true` is refused.

## `(orm.field)`

| field | type | means |
|---|---|---|
| `disabled` | bool | not stored at all — a computed value that happens to be on the message |
| `type` | `Type` | the stored type. Unset means deduced from the proto kind |
| `key` | bool | the primary key. Exactly one per entity |
| `unique` | bool | unique, and therefore a way to look a row up |
| `nullable` | bool | may be null |
| `immutable` | bool | cannot change after insert, so it is not in a patch |
| `default` | string | has a default |
| `version` | `VersionOptions` | the optimistic-lock field. Written `version: {}` |
| `erased` | `ErasedOptions` | the soft-delete stamp. Written `erased: {}` |

Marking a field `key: true` also makes it unique, immutable and not nullable.
Saying the opposite of any of those in the same annotation is an error rather
than something quietly overridden.

### `version:` and `erased:` are messages, not booleans

They are empty messages, and presence is what turns them on:

```proto
google.protobuf.Timestamp date_updated = 14 [(orm.field) = {version: {}}];
```

`version: true` does not compile. This is inherited from the upstream
vocabulary, and it is the single most common thing to get wrong.

Both must be `time`. A version field cannot be the key, unique, nullable or
immutable. An erased field cannot be the key, unique or immutable, cannot also
be the version, and *is* nullable — being null is how it says the row is still
there. Writing `nullable: false` next to `erased: {}` is refused rather than
ignored.

There is at most one of each per entity.

### `default` is about presence, not value

```proto
string alias = 4 [(orm.field) = {default: ""}];
```

That field has a default. `default: ""` and no `default` at all are different
things, because the option's presence is the signal and the string is only the
value. It is why the annotation appears with an empty string all over real
schemas — it is saying "this has a default", not "the default is empty".

What a default lowers to depends on the backend. On ent it becomes
`.Optional()`, not `.Default(...)`; see [the Go target](targets/go.md).

## `(orm.edge)`

An edge is a reference to another entity, stored as that entity's key.

```proto
Tenant tenant = 2 [(orm.edge) = {}];
```

| field | type | means |
|---|---|---|
| `disabled` | bool | not stored |
| `bind` | `Ref` | reserved; parsed and carried, and no target reads it yet |
| `from` | `Ref` | this edge is the back reference of a named edge on the target |
| `unique` | bool | unique, and therefore a way to look a row up |
| `nullable` | bool | may be null |
| `immutable` | bool | cannot change after insert |
| `default` | string | has a default |

There is no `type`, no `key`, no `version` and no `erased` on an edge. Its
stored type is always the target's key type.

An edge must point at a message, that message must be an entity, and a map
cannot be an edge. A repeated edge cannot be unique.

### `from:` — back references

```proto
message Tenant {
  repeated User users = 3 [(orm.edge) = {from: {name: "tenant" number: 2}}];
}
```

The named edge must exist on the target, must be an edge rather than a field,
and must agree on both name and number. If neither side is repeated the relation
is one-to-one, and both sides become unique.

## `Ref` — naming by name *and* number

```proto
{name: "alias" number: 4}
```

Both have to match. That is the point: renaming a field, or renumbering it,
breaks the reference loudly instead of pointing at whatever now occupies the
number.

## `(orm.message)`

```proto
option (orm.message) = {
  rpc: {crud: true}
  indexes: [{name: "slug" refs: [...] unique: true}]
};
```

| field | type | means |
|---|---|---|
| `disabled` | bool | not an entity after all |
| `rpc` | `RpcOptions` | which operations exist |
| `indexes` | repeated `Index` | indexes over more than one prop |

### `rpc`

| field | means |
|---|---|
| `disabled` | no operations |
| `crud` | all four: add, get, patch, erase |
| `add` / `get` / `patch` / `erase` | one each, itself carrying `disabled` |

An operation is on when it is not disabled and either `crud` is set or that
operation was named. So `crud: true` plus `patch: {disabled: true}` is three
operations, and `get: {}` alone is one.

### `indexes`

| field | type | means |
|---|---|---|
| `disabled` | bool | skipped |
| `name` | string | required |
| `refs` | repeated `Ref` | the members, in index order |
| `unique` | bool | unique, and therefore a way to look a row up |
| `immutable` | bool | |
| `hidden` | bool | unique, but not offered as a lookup |
| `includes_erased` | bool | a unique index that covers erased rows too |

An index must be named and must have at least one ref. `includes_erased` on a
non-unique index is an error, because it says nothing about one.

Two things worth knowing before you rely on an index:

- **A non-unique index is not emitted by the Dexie backend at all.** Its schema
  string is built from the entity's lookups, and a non-unique index is not one.
  See [the TypeScript target](targets/typescript.md).
- **`hidden: true` means the store gets the index and the API does not.** It is
  a constraint you want enforced without offering a lookup by it.

## When you annotate nothing

A field with no annotation is still a field, with its type deduced. The rules,
in the order they are applied:

| the proto says | the schema gets |
|---|---|
| any non-message kind | the matching type — `string`, `bool`, `int32`, `bytes`, … |
| `map<K, V>` | `json` — a map is a document, not a relation |
| `google.protobuf.Timestamp` | `time` |
| `google.protobuf.Struct` or `Value` | `json` |
| any other message | `json` |

So `Profile profile = 9;` with no `(orm.edge)` is stored as a document, and
`map<string, string> labels = 7;` needs no annotation at all.

**`TYPE_UUID` is never deduced.** A `bytes` field is bytes until you say
otherwise, because deducing from length would make the field's meaning depend on
the data in it:

```proto
bytes id = 1 [(orm.field) = {type: TYPE_UUID key: true default: ""}];
```

### Nullability

For a **field**:

1. repeated is never nullable — proto cannot tell an empty list from an absent one
2. `nullable: true`, or the `optional` keyword, means nullable
3. otherwise, presence means nullable — **except for `time`**
4. otherwise not

The exception in (3) exists because every message field has presence, and
`google.protobuf.Timestamp` is a message. Without it every timestamp in every
schema would be nullable and no annotation could say otherwise.

For an **edge**, presence is *not* read as nullability — only `nullable: true`
or `optional` is. The rules differ on purpose: a message field's presence says
something about the value, an edge's says something about the reference.

## Types

Values under 64 are the proto kinds and carry their numbers. The three above are
storage concepts proto has no kind for.

`TYPE_DOUBLE` `TYPE_FLOAT` `TYPE_INT64` `TYPE_UINT64` `TYPE_INT32`
`TYPE_FIXED64` `TYPE_FIXED32` `TYPE_BOOL` `TYPE_STRING` `TYPE_MESSAGE`
`TYPE_BYTES` `TYPE_UINT32` `TYPE_ENUM` `TYPE_SFIXED32` `TYPE_SFIXED64`
`TYPE_SINT32` `TYPE_SINT64` — and `TYPE_INT` (an alias of `TYPE_INT64`),
`TYPE_UINT` (an alias of `TYPE_UINT64`).

**`TYPE_UUID` = 64**, **`TYPE_TIME` = 65**, **`TYPE_JSON` = 66**.

A field cannot be a message type. If you wrote `type: TYPE_MESSAGE`, you wanted
either `TYPE_JSON` or an `(orm.edge)`, and the error says so.

## Errors

Every error names a path and says what is wrong with the schema, not with
calque. They are collected rather than reported one at a time, so a file with
four problems tells you about four problems.

```
apptest.User.alias: no prop named nope
apptest.User.{indexes}(slug).refs[1]: alias is field 4, not 7
	a ref names a prop by name and number, so that renaming or renumbering one is loud rather than silent
```

Validation happens in two passes: everything about annotations first, and only
if that passes, everything about structure. So an error about a broken reference
is never noise from an annotation that was going to be rejected anyway.

| what you wrote | what it says |
|---|---|
| both `(orm.field)` and `(orm.edge)` | `only one of "orm.field" or "orm.edge" can be specified` |
| `type: TYPE_MESSAGE` | `a field cannot be a message type` |
| no `key: true` anywhere | `no key is defined` |
| two `key: true` | `there can be only one key, but id(1) and alias(2) are both marked` |
| `key: true unique: false` | `the key must be unique` |
| `key: true nullable: true` | `the key cannot be nullable` |
| `key: true immutable: false` | `the key must be immutable` |
| `(orm.edge)` on a scalar | `an edge must reference a message, but this is string` |
| `(orm.edge)` on a map | `a map cannot be an edge` |
| edge to a non-entity | `invalid.Plain is not an entity` |
| repeated edge with `unique: true` | `an edge with repeated cardinality cannot be unique` |
| index with no `refs` | `an index must reference at least one prop` |
| index with no `name` | `an index must be named` |
| `refs` naming nothing | `no prop named nope` |
| `refs` with the wrong number | `alias is field 4, not 7` |
| `includes_erased` without `unique` | `includes_erased says nothing about an index that is not unique` |
| `key: true version: {}` | `the version field cannot be the key` |
| `unique: true version: {}` | `the version field cannot be unique, nullable or immutable` |
| `version: {}` on a string | `only the time type supports versioning, but this is string` |
| two `version: {}` | `there can be only one version field; a is already one` |
| `key: true erased: {}` | `the erased field cannot be the key` |
| `unique: true erased: {}` | `the erased field cannot be unique or immutable` |
| `version: {} erased: {}` | `the erased field cannot also be the version field` |
| `erased: {}` on a string | `only the time type can say that a row was erased, but this is string` |
| `nullable: false erased: {}` | `the erased field is nullable, since being null is how it says the row is still there` |
| two `erased: {}` | `there can be only one erased field; a is already one` |
| `from:` naming nothing | `back reference names nope, which invalid.T does not have` |
| `from:` with the wrong number | `back reference names wrong as field 10, but it is field N` |
| `from:` naming a field | `back reference names alias, which is a field rather than an edge` |
| repeated edge whose `from:` is unique | `back reference parent is unique, so this edge cannot be repeated` |
| an option field this calque does not know | `(orm.field) carries a field this calque does not know` |

Each row above is a fixture under `testdata/proto/invalid/`, named after the
mistake, and a test asserts that the corpus and the table are the same set — so
this list cannot drift from what the code actually refuses.

## A complete example

This is `testdata/proto/valid/apptest.proto`, which every downstream test runs
against.

```proto
edition = "2023";
package apptest;

import "google/protobuf/timestamp.proto";
import "orm.proto";

option features.field_presence = IMPLICIT;
option go_package = "example.com/apptest/oas";

message Tenant {
  bytes id = 1 [(orm.field) = {type: TYPE_UUID key: true default: ""}];
  string alias = 4 [(orm.field) = {unique: true default: ""}];
  string name = 5 [(orm.field) = {default: ""}];
  google.protobuf.Timestamp date_created = 15 [(orm.field) = {immutable: true default: ""}];

  option (orm.message) = {rpc: {crud: true}};
}

message User {
  bytes id = 1 [(orm.field) = {type: TYPE_UUID key: true default: ""}];

  Tenant tenant = 2 [(orm.edge) = {}];

  string alias = 4 [(orm.field) = {default: ""}];
  string name = 5 [(orm.field) = {default: ""}];

  // No annotation at all: a map deduces to json.
  map<string, string> labels = 7;

  // Explicit presence, so this one really is nullable.
  string lock = 8 [features.field_presence = EXPLICIT, (orm.field) = {nullable: true}];

  // A message with no (orm.edge) is stored, not related: json.
  Profile profile = 9;

  google.protobuf.Timestamp date_updated = 14 [(orm.field) = {version: {}}];
  google.protobuf.Timestamp date_created = 15 [(orm.field) = {immutable: true default: ""}];

  // Opted out entirely: computed, never stored.
  string state = 16 [(orm.field) = {disabled: true}];

  option (orm.message) = {
    rpc: {crud: true}
    indexes: [{
      name: "slug"
      refs: [{name: "alias" number: 4}, {name: "tenant" number: 2}]
      unique: true
    }]
  };
}

// Not an entity: no (orm.message).
message Profile {
  string bio = 1;
}
```
