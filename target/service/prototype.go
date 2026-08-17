package service

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/heojeongbo/calque/schema"
)

// protoType is how a prop's type is written in a .proto file.
//
// It comes off the descriptor rather than schema.Type, and it has to: schema.Type
// is the annotation vocabulary's idea of a type -- TYPE_UUID, TYPE_TIME, TYPE_JSON
// -- which is what calque reasons about and not what a proto file says. `bytes`,
// `google.protobuf.Timestamp` and a domain message can all be the same schema.Type
// and only the descriptor knows which is which.
func protoType(fd protoreflect.FieldDescriptor, pkg protoreflect.FullName) string {
	if fd.IsMap() {
		return fmt.Sprintf("map<%s, %s>",
			fd.MapKey().Kind().String(), protoType(fd.MapValue(), pkg))
	}

	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return strip(fd.Message().FullName(), pkg)
	case protoreflect.EnumKind:
		return strip(fd.Enum().FullName(), pkg)
	default:
		return fd.Kind().String()
	}
}

// strip removes the emitting file's own package, which is how a proto file names a
// type it declares itself.
//
// The printer being reproduced does this by prefix-matching the whole type string,
// so a map never matched -- `map<string, apptest.Level>` came out fully qualified
// inside package apptest. Recursing instead of prefix-matching is that bug fixed;
// see docs/targets/service.md.
func strip(name, pkg protoreflect.FullName) string {
	if s, ok := strings.CutPrefix(string(name), string(pkg)+"."); ok {
		return s
	}
	return string(name)
}

// source is the descriptor behind a prop.
//
// A visitor rather than a type switch so that a new Elem variant is a compile
// error here, which is the rule the whole schema package is shaped around.
type source struct{}

func (source) VisitField(f *schema.Field) (protoreflect.FieldDescriptor, error) {
	return f.Source(), nil
}

func (source) VisitEdge(e *schema.Edge) (protoreflect.FieldDescriptor, error) {
	return e.Source(), nil
}

func descriptorOf(p schema.Prop) protoreflect.FieldDescriptor {
	fd, _ := schema.VisitProp[protoreflect.FieldDescriptor](p, source{})
	return fd
}

// label is the `repeated` keyword, or nothing.
//
// A map is repeated on the wire and is not written that way, which is why this
// asks the descriptor rather than schema.Prop.IsList.
func label(fd protoreflect.FieldDescriptor) string {
	if fd.IsList() {
		return "repeated"
	}
	return ""
}
