package schema

import "fmt"

// Type is what a prop's value is, in storage terms.
//
// It is deliberately not ormopt.Type. The compat layer maps one to the other
// and refuses an ormopt.Type it does not know, so a newer buf.build/orm/orm
// fails loudly here instead of falling through a switch somewhere downstream.
//
// Upstream's enum carries aliases -- TYPE_INT and TYPE_INT64 are both 3 -- so
// two spellings arrive as one value. This one has no aliases: there is exactly
// one way to be a 64-bit signed integer, and a switch over Type is therefore
// answerable.
type Type int

// The types a prop can have. TypeMessage is only ever an Edge's.
const (
	TypeUnspecified Type = iota
	TypeBool
	TypeEnum
	TypeInt32
	TypeSint32
	TypeUint32
	TypeInt64
	TypeSint64
	TypeUint64
	TypeSfixed32
	TypeFixed32
	TypeSfixed64
	TypeFixed64
	TypeFloat
	TypeDouble
	TypeString
	TypeBytes
	TypeUUID
	TypeTime
	TypeJSON
	TypeMessage
)

// AllTypes is every type, and tests walk it.
//
// A backend that has no answer for one of these has a hole, and the way to find
// out is a test that asks about all of them rather than about the ones a
// fixture happens to use. protoc-gen-orm-ts read exactly one type -- it checked
// TYPE_UUID and passed everything else through untransformed -- and nothing
// said so.
func AllTypes() []Type {
	return []Type{
		TypeBool,
		TypeEnum,
		TypeInt32,
		TypeSint32,
		TypeUint32,
		TypeInt64,
		TypeSint64,
		TypeUint64,
		TypeSfixed32,
		TypeFixed32,
		TypeSfixed64,
		TypeFixed64,
		TypeFloat,
		TypeDouble,
		TypeString,
		TypeBytes,
		TypeUUID,
		TypeTime,
		TypeJSON,
		TypeMessage,
	}
}

var typeNames = map[Type]string{
	TypeUnspecified: "unspecified",
	TypeBool:        "bool",
	TypeEnum:        "enum",
	TypeInt32:       "int32",
	TypeSint32:      "sint32",
	TypeUint32:      "uint32",
	TypeInt64:       "int64",
	TypeSint64:      "sint64",
	TypeUint64:      "uint64",
	TypeSfixed32:    "sfixed32",
	TypeFixed32:     "fixed32",
	TypeSfixed64:    "sfixed64",
	TypeFixed64:     "fixed64",
	TypeFloat:       "float",
	TypeDouble:      "double",
	TypeString:      "string",
	TypeBytes:       "bytes",
	TypeUUID:        "uuid",
	TypeTime:        "time",
	TypeJSON:        "json",
	TypeMessage:     "message",
}

func (t Type) String() string {
	if s, ok := typeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("Type(%d)", int(t))
}

// IsOrderable reports whether a range over this type means anything, which is
// what an index member has to be.
//
// JSON is not: two documents have no order a store agrees on. A message is not,
// for the same reason -- and an edge is indexed by its target's key, not by
// itself, so this is never asked about one.
func (t Type) IsOrderable() bool {
	switch t {
	case TypeJSON, TypeMessage, TypeUnspecified:
		return false
	default:
		return true
	}
}

// IsInteger reports whether the type is one of the integer kinds, which is the
// question most backends actually want when they are choosing a column type.
func (t Type) IsInteger() bool {
	switch t {
	case TypeInt32, TypeSint32, TypeUint32, TypeInt64, TypeSint64, TypeUint64,
		TypeSfixed32, TypeFixed32, TypeSfixed64, TypeFixed64:
		return true
	default:
		return false
	}
}
