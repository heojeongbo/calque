Two variants of `valid/apptest_svc.proto` that differ only in the `Ref` message's
oneof, for the one rule both targets share about it: it is found by being the
message's only oneof, and its name is not read.

| file | difference | what it pins |
|---|---|---|
| `apptest_svc.proto` | `oneof key` renamed to `oneof lookup` | output is identical to `valid/` |
| `two_svc.proto` | `UserRef` carries two oneofs | the Go target refuses, the TypeScript target warns |

There are no entity protos here on purpose. The tests compile with this directory
ahead of `valid/` on the import path, so `import "apptest.proto"` still resolves
to the one fixture entity file and these copies cannot drift from it.

Only one of the two service files is ever compiled at a time -- they declare the
same messages in the same package.
