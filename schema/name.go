package schema

import "strings"

// A thing in a schema has more than one name, and which one is correct depends
// entirely on where it is about to be written.
//
// protoc-gen-orm-ts had one notion of "the name" and two functions that
// computed it -- strcase.ToCamel in the db app, a hand-rolled camel() in the
// client app -- and neither asked the descriptor. A field written `device_id`
// is `deviceId` on a protoc-gen-es message, so the generated code read
// `v.DeviceId`, wrote `w.device_id`, and indexed a third spelling. Nothing
// failed; the descriptor just quietly read undefined.
//
// The fix is not a better casing function. It is that these are different types,
// so putting one where another belongs does not compile.

// ProtoName is the name as written in the .proto file: "device_id".
//
// It is a prop's identity. It is the only name that is stable across target
// languages, so it is what diagnostics, config, and cross-references use -- and
// it is never what generated code that touches a value should say.
type ProtoName string

// ValueName is the property name on a decoded message value: "deviceId".
//
// It is protoc's JSON name, read off the descriptor. calque does not compute
// it: there is no casing function in this repository that returns a ValueName,
// because the compiler already decided and guessing is how the two spellings
// got out of step in the first place.
type ValueName string

// StoreName is a name inside a stored record.
//
// It belongs to the backend, not to the schema: whether an edge's foreign key
// is called "tenant" or "tenant_id" is a decision a store makes, which is why
// nothing on Prop returns one and gen.Backend.StorePath does.
type StoreName string

// StorePath is a StoreName sequence.
//
// {"tenant", "id"} is "tenant.id" in a store that indexes into nested objects
// and "tenant_id" in one that does not, so it stays a sequence until a backend
// renders it. protoc-gen-orm-ts joined it with a dot in the schema builder and
// again in the query builder, which is two of the three reasons a second
// backend was not possible.
type StorePath []StoreName

// String renders a StorePath the way a nesting store spells it. It is for
// diagnostics and for backends that happen to agree with it; a backend that
// spells paths differently renders them itself.
func (p StorePath) String() string {
	parts := make([]string, len(p))
	for i, n := range p {
		parts[i] = string(n)
	}
	return strings.Join(parts, ".")
}

// Names is every name a prop has.
//
// There is deliberately no Name() string on Prop. A caller has to say which
// world it is naming into, and the compiler checks the answer.
type Names struct {
	Proto ProtoName
	Value ValueName
}
