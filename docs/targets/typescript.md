# The TypeScript target

Emits tables over [Dexie](https://dexie.org) and IndexedDB, typed against
[protobuf-es](https://github.com/bufbuild/protobuf-es) messages.

```yaml
targets:
  - {target: ts, backend: dexie, out: ts}
```

**Status: complete.** Against the real proto tree it replaces, it produces 28
of 28 files byte-identical to what `protoc-gen-orm-ts` produces.

## What it emits

One directory per proto package, with the package's dots becoming slashes.

| file | when |
|---|---|
| `client.g.ts` | always, even with no services |
| `db.g.ts` | when the package has at least one entity |
| `<entity>.db.g.ts` | per entity — the name is lower-cased whole, so `PosePreset` is `posepreset.db.g.ts` |

An entity gets a table only if a service named after it exists — `apptest.User`
needs `apptest.UserService`. calque does not generate that service; it is
another plugin's job. It reads it.

### `client.g.ts`

Types for the Connect clients, and the query descriptors that let a cache know
what a response contains.

```ts
export type UserServiceClient = C<typeof UserService>

export interface ServiceClient {
	readonly tenant: TenantServiceClient
	readonly user: UserServiceClient
}

export const queries = {
	["apptest.UserService"]: {
		pick: (v) => ({ case: "id", value: v.id }),
		refs: (v) => [...],
		rpc: { get: {...}, list: {...} },
	} satisfies QueryDescOf<typeof UserService>,
}
```

`pick` names a message's primary reference; `refs` names every way the row could
be looked up; `rpc[method].extract` pulls the entity out of that method's
response — `v` when the response *is* the entity, `v.items` when it wraps one.

Service properties are the service name minus `Service`, first letter
lower-cased. That is `LowerFirst`, not camelCase: `BTExecutorService` becomes
`bTExecutor`. It looks wrong and it is what the consuming code says.

### `db.g.ts`

The package's tables, collected.

```ts
export type Db = 
	& Tenant.Db
	& User.Db

export function schemas(){
	return {
		[Tenant.TableName]: Tenant.Schema,
		[User.TableName]: User.Schema,
	} as const
}
```

`schemas()` is what goes into Dexie's `version(n).stores(...)`.

### `<entity>.db.g.ts`

```ts
export const TableName = "apptest.User";
export const Schema = "&id,[alias+tenant.id]" as const;

export class TableService extends TableBase<Desc> implements Partial<UserServiceClient> {
	constructor(db: Db) { super(db, UserSchema); }

	_dehydrate(v: User): [Key, any] { ... }
	_hydrate(v: any): User { ... }

	get(req: MessageInitShape<typeof UserGetRequestSchema>): Promise<User> { ... }
}
```

`_dehydrate` and `_hydrate` convert the props that need converting — a uuid is
16 bytes on the wire and hyphenated text in the store, because IndexedDB cannot
index a byte array. They walk the entity's *lookups*, not all its props: a
conversion line exists for a prop that participates in an index.

`get` is a `switch` over the reference's `key` case, one arm per lookup. The
case labels are read out of the `<Entity>Ref` message's oneof by field number,
not computed — computing them is how a generator ends up querying a spelling
nothing declared.

## The Dexie schema string

Dexie describes a table with a mini-DSL, and this is the only place in calque
that knows its syntax.

| declaration | emitted |
|---|---|
| the key | `&id` |
| a unique field | `&alias` |
| a unique edge | `&tenant.id` |
| a unique index | `[alias+tenant.id]` — brackets even for one member |

Built from `Entity.Keys()`, which is every unique element. Two consequences:

- **A non-unique index is not emitted at all.** It is not a lookup, so it is not
  in `Keys()`.
- **A unique compound index is emitted without the `&`.** Dexie has no unique
  form of a compound index. The index is created and it is not unique.

That second one is conformance item 3, and it has already cost something in
production. calque will not do it silently: `dexie.compat: none` makes it an
error naming the index. See [Migrating](../migrating.md) for why the default is
still to reproduce it.

## `compat: orm-ts`

The default. It reproduces `protoc-gen-orm-ts`, bugs included, so that adopting
calque on an existing database is a swap with an empty diff.

What it reproduces:

- **an index component spelled with the proto name** where the row carries the
  JSON name. The store declares `&token_hash` and the query asks for
  `{tokenHash}`. IndexedDB does not error on an unresolvable key path — it
  declines to index the record — which is why it has been latent.
- **a unique compound index emitted without `&`**, as above.
- **`_dehydrate` writing an edge with no presence guard**, so an absent optional
  edge throws while dehydrating.

Each one warns on stderr every time it is generated, naming the entity and the
prop and what to do:

```
calque: apptest.User: index component "token_hash" is stored as "tokenHash"; the index is
declared under a name no row has, so it is never used
	compat: orm-ts is reproducing protoc-gen-orm-ts here. Set dexie.compat: none to fix
	it — that changes the stored schema and needs a Dexie version bump.
```

`compat: none` fixes all of them and makes the backend strict, so a capability
shortfall stops the build instead of warning.

## Runtime

Generated code imports one specifier, `@heojeongbo/calque-dexie` by default,
configurable with `ts.runtime`.

| package | |
|---|---|
| [`@heojeongbo/calque-runtime`](../../ts/packages/runtime) | types and codecs, no store dependency |
| [`@heojeongbo/calque-dexie`](../../ts/packages/dexie) | `TableBase` and the Dexie types; re-exports the runtime |

```sh
pnpm add @heojeongbo/calque-dexie dexie @bufbuild/protobuf @connectrpc/connect
```

`dexie`, `@bufbuild/protobuf` and `@connectrpc/connect` are peer dependencies —
your app already has them and the generated code has to agree with the copy it
has.

### `TableBase`

`_query`, `_insert`, `_reconcile`, `_compare`, `_clone`, plus the `_dehydrate`
and `_hydrate` the generated subclass overrides.

`_reconcile` reports whether it wrote. `false` means the stored value was newer
or the same and nothing happened, so a cache written like this behaves:

```ts
const ok = await table._reconcile(v)
if (!ok) continue      // taken, as of runtime v0.2.0
```

It did not until v0.2.0: the comparison's result was discarded and the method
always resolved `true`, which was conformance item 5. Upgrading a consumer that
already had that branch makes it live for the first time.

## Configuration

| key | default | |
|---|---|---|
| `runtime` | `@heojeongbo/calque-dexie` | the module generated code imports |
| `header` | `// Code generated by calque. DO NOT EDIT.` | first line of every file |
| `import_extension` | `""` | appended to relative imports — `.js` under NodeNext |

`import_extension` exists because the predecessor emitted `.ts` and the
consuming repository stripped it again with a regex over every generated file.
Emitting what is wanted removes the post-pass.

## Known gaps

Both are conformance items and both are open.

- **Only `get` is emitted**, even from `rpc: {crud: true}`. `add`, `patch` and
  `erase` are not. The class says `implements Partial<UserServiceClient>`, which
  is a way of saying "some methods are missing" that no compiler complains
  about.
- **There are no tests for the generated TypeScript.** No `fake-indexeddb`, no
  Dexie test, nothing exercising a table. The runtime packages have tests; what
  the generator emits is verified by byte-comparison against a generator that
  also has none.
