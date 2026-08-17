package service

import (
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/heojeongbo/calque/schema"
)

// imports is the set of files one output file has to import, collected while its
// declarations are written and sorted before they are printed.
//
// Collected rather than computed, because what a contract references depends on
// which messages it emitted, which depends on which rpcs the entity declared. An
// entity with only `get` never mentions google.protobuf.Empty and must not import
// it: an unused import is a lint failure in the tree this has to keep green.
type imports struct {
	set map[string]bool
}

func newImports() *imports { return &imports{set: map[string]bool{}} }

func (i *imports) add(path string) {
	if path != "" {
		i.set[path] = true
	}
}

// sorted is the import paths, ascending -- byte-for-byte the order the printer
// being replaced emits, which sorts them with a plain string compare.
func (i *imports) sorted() []string {
	out := make([]string, 0, len(i.set))
	for p := range i.set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// need registers whatever declares a prop's type.
//
// A field's type may live anywhere -- google/protobuf/timestamp.proto, a sibling
// proto, the entity's own file -- and the descriptor knows which. An edge's type
// is the *generated* contract of its target, because TenantRef and TenantSelect
// are declared there and not in tenant.proto: that is why one output file imports
// another's.
func (i *imports) need(p schema.Prop, out func(*schema.Entity) string) {
	if edge, ok := p.(*schema.Edge); ok {
		i.add(out(edge.Target()))
		return
	}
	i.needType(descriptorOf(p))
}

func (i *imports) needType(fd protoreflect.FieldDescriptor) {
	if fd.IsMap() {
		i.needType(fd.MapValue())
		return
	}
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		i.add(fd.Message().ParentFile().Path())
	case protoreflect.EnumKind:
		i.add(fd.Enum().ParentFile().Path())
	}
}
