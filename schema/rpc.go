package schema

import "sort"

// Op is one of the operations an entity can declare.
type Op string

const (
	OpAdd   Op = "add"
	OpGet   Op = "get"
	OpPatch Op = "patch"
	OpErase Op = "erase"
)

// AllOps is every operation, in the order they are generated.
func AllOps() []Op { return []Op{OpAdd, OpGet, OpPatch, OpErase} }

// RpcSet is the operations an entity declares.
//
// It is a set rather than four bools so that "which operations exist" has one
// answer that a test can walk. protoc-gen-orm-ts derived this per target and
// generated only get, leaving add, patch and erase declared in the service
// protos and absent from the output -- licensed by `implements Partial<...>`,
// which is a type-level way of saying nothing is missing when something is.
type RpcSet map[Op]bool

// Has reports whether op is declared.
func (r RpcSet) Has(op Op) bool { return r[op] }

// Ops is the declared operations, in AllOps order.
func (r RpcSet) Ops() []Op {
	var out []Op
	for _, op := range AllOps() {
		if r[op] {
			out = append(out, op)
		}
	}
	return out
}

// Empty reports whether nothing is declared, which means the entity is modelled
// but has no service surface.
func (r RpcSet) Empty() bool { return len(r.Ops()) == 0 }

// Names returns the declared operations as strings, sorted, for diagnostics.
func (r RpcSet) Names() []string {
	out := make([]string, 0, len(r))
	for op := range r {
		if r[op] {
			out = append(out, string(op))
		}
	}
	sort.Strings(out)
	return out
}
