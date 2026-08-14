package ormcompat

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/HeoJeongBo/calque/ormopt"
	"github.com/HeoJeongBo/calque/schema"
)

// typeOf maps an annotation type to a schema type.
//
// Upstream's enum has aliases -- TYPE_INT and TYPE_INT64 are both 3 -- so two
// spellings arrive here as one value, which is why this switch is shorter than
// the enum looks.
//
// An unrecognised value is an error rather than a fallthrough. A newer
// buf.build/orm/orm that adds a type should stop calque, not have its new type
// silently stored as whatever the zero value happens to mean.
func typeOf(t ormopt.Type) (schema.Type, error) {
	switch t {
	case ormopt.Type_TYPE_BOOL:
		return schema.TypeBool, nil
	case ormopt.Type_TYPE_ENUM:
		return schema.TypeEnum, nil
	case ormopt.Type_TYPE_INT32:
		return schema.TypeInt32, nil
	case ormopt.Type_TYPE_SINT32:
		return schema.TypeSint32, nil
	case ormopt.Type_TYPE_UINT32:
		return schema.TypeUint32, nil
	case ormopt.Type_TYPE_INT64: // also TYPE_INT
		return schema.TypeInt64, nil
	case ormopt.Type_TYPE_SINT64:
		return schema.TypeSint64, nil
	case ormopt.Type_TYPE_UINT64: // also TYPE_UINT
		return schema.TypeUint64, nil
	case ormopt.Type_TYPE_SFIXED32:
		return schema.TypeSfixed32, nil
	case ormopt.Type_TYPE_FIXED32:
		return schema.TypeFixed32, nil
	case ormopt.Type_TYPE_SFIXED64:
		return schema.TypeSfixed64, nil
	case ormopt.Type_TYPE_FIXED64:
		return schema.TypeFixed64, nil
	case ormopt.Type_TYPE_FLOAT:
		return schema.TypeFloat, nil
	case ormopt.Type_TYPE_DOUBLE:
		return schema.TypeDouble, nil
	case ormopt.Type_TYPE_STRING:
		return schema.TypeString, nil
	case ormopt.Type_TYPE_BYTES:
		return schema.TypeBytes, nil
	case ormopt.Type_TYPE_MESSAGE, ormopt.Type_TYPE_GROUP:
		return schema.TypeMessage, nil
	case ormopt.Type_TYPE_UUID:
		return schema.TypeUUID, nil
	case ormopt.Type_TYPE_TIME:
		return schema.TypeTime, nil
	case ormopt.Type_TYPE_JSON:
		return schema.TypeJSON, nil
	default:
		return schema.TypeUnspecified, fmt.Errorf("unknown orm.Type %d", int32(t))
	}
}

// typeFromKind maps a proto kind to the type it is when nothing says otherwise.
func typeFromKind(k protoreflect.Kind) (schema.Type, error) {
	switch k {
	case protoreflect.BoolKind:
		return schema.TypeBool, nil
	case protoreflect.EnumKind:
		return schema.TypeEnum, nil
	case protoreflect.Int32Kind:
		return schema.TypeInt32, nil
	case protoreflect.Sint32Kind:
		return schema.TypeSint32, nil
	case protoreflect.Uint32Kind:
		return schema.TypeUint32, nil
	case protoreflect.Int64Kind:
		return schema.TypeInt64, nil
	case protoreflect.Sint64Kind:
		return schema.TypeSint64, nil
	case protoreflect.Uint64Kind:
		return schema.TypeUint64, nil
	case protoreflect.Sfixed32Kind:
		return schema.TypeSfixed32, nil
	case protoreflect.Fixed32Kind:
		return schema.TypeFixed32, nil
	case protoreflect.Sfixed64Kind:
		return schema.TypeSfixed64, nil
	case protoreflect.Fixed64Kind:
		return schema.TypeFixed64, nil
	case protoreflect.FloatKind:
		return schema.TypeFloat, nil
	case protoreflect.DoubleKind:
		return schema.TypeDouble, nil
	case protoreflect.StringKind:
		return schema.TypeString, nil
	case protoreflect.BytesKind:
		return schema.TypeBytes, nil
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return schema.TypeMessage, nil
	default:
		return schema.TypeUnspecified, fmt.Errorf("unknown proto kind %s", k)
	}
}

// Well-known messages that mean something other than "a document".
const (
	wktTimestamp = "google.protobuf.Timestamp"
	wktStruct    = "google.protobuf.Struct"
	wktValue     = "google.protobuf.Value"
)

// deduceType is the type a prop has when the annotation did not name one.
//
// A map is a document. A Timestamp is a time. Struct and Value are documents,
// and so is any other message -- a message field is stored, not related, and
// relating is what (orm.edge) is for.
//
// TYPE_UUID is never deduced. A bytes field is bytes until it says otherwise,
// because deciding from the length would make a field's meaning depend on its
// data.
func deduceType(fd protoreflect.FieldDescriptor) (schema.Type, error) {
	t, err := typeFromKind(fd.Kind())
	if err != nil {
		return schema.TypeUnspecified, err
	}
	if t != schema.TypeMessage {
		return t, nil
	}

	if fd.IsMap() {
		return schema.TypeJSON, nil
	}
	switch fd.Message().FullName() {
	case wktTimestamp:
		return schema.TypeTime, nil
	case wktStruct, wktValue:
		return schema.TypeJSON, nil
	default:
		return schema.TypeJSON, nil
	}
}
