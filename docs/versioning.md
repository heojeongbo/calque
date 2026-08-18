# Versioning

Three numbers, and they are not the same number.

| | example | what it versions | who moves it |
|---|---|---|---|
| **the plugin** | `v0.6.0` — a git tag, and the Go module version | the thing that **generates** code | a calque release |
| **the runtime** | `@heojeongbo/calque-dexie@0.2.0` | what generated TypeScript **calls at run time** | a change to the runtime |
| | `@heojeongbo/calque-runtime@0.1.0` | the store-neutral half the above re-exports | " |
| **the config** | `version: 1` in `calque.yaml` | the **shape of the config file** | a breaking format change |

They are deliberately not kept in step. `v0.6.0` does not imply
`calque-dexie@0.6.0` and never will: the runtime moves when the runtime changes,
and most calque releases do not touch it. The plugin being on its sixth release
while the runtime is on its second is the evidence, not an oversight.

## One rule decides the rest

**Only the runtime can change behaviour without a regeneration.**

- **The plugin lives at build time only.** Upgrading it changes nothing until you
  run the generator again — and when you do, the change arrives as a diff in your
  tree, where you can read it. [The Go target](targets/go.md#runtime) has nothing
  else: its generated code imports no calque package at all, so the plugin
  version is the only calque version that exists on that side.
- **The runtime lives at run time.** Upgrading it changes the behaviour of code
  you did not regenerate and did not review. That is the whole reason
  `_reconcile` reporting its outcome was a **minor** and not a patch: the output
  did not move, and the behaviour did. See
  [the TypeScript target](targets/typescript.md#runtime) and
  [conformance item 5](conformance.md).
- **The config version refuses at parse time**, loudly and by name, rather than
  letting a newer document decompose into a pile of unknown-key errors
  (`gen/config.go:21-26`).

So the question to ask before upgrading anything is which of the three you are
touching. Upgrading the plugin is safe until you regenerate; upgrading the
runtime is the one that can surprise you.

## What is where, right now

```sh
git tag -n1                                  # the plugin, one line per release
npm view @heojeongbo/calque-dexie versions   # the runtime
grep '^version:' calque.yaml                 # the config format
```

In a consuming repository the same three appear as: the plugin in `go.mod`
(or the `buf.gen.yaml` plugin entry), the runtime in `package.json`, and
`version:` at the top of `calque.yaml`.

## Where the record of changes is

There is no CHANGELOG, and this is what stands in for one.

**For the plugin, the tags are annotated** — `git tag -n1` reads as a release
history, one line each, because every tag was written to be read that way.

**For the runtime there is no such record.** What changed in
`calque-dexie@0.2.0` is in its commit and in
[Migrating](migrating.md#stage-2-what-to-fix-and-in-what-order), which lists the
things that need a code change rather than a regeneration — but nothing collects
it per release. That is a gap; it is written down here rather than papered over,
because the runtime is the version that can change behaviour under you, and it is
the one whose history is hardest to see.
