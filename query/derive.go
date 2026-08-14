package query

import (
	"fmt"

	"github.com/HeoJeongBo/calque/schema"
)

// Derive builds every plan an entity's declared operations imply.
//
// It lives in the core, not in a target, because which operations exist is a
// property of the schema. protoc-gen-orm-ts decided this per target and
// generated only `get`, leaving add, patch and erase declared in the service
// protos and absent from the output — licensed by `implements Partial<...>`,
// which is a type-level way of saying "some methods are missing" that no
// compiler ever objects to.
//
// Deriving centrally means a target cannot quietly emit fewer, and
// TestPlansCoverDeclaredRpcs can say so if one tries.
func Derive(e *schema.Entity) (Set, error) {
	if e.Key() == nil {
		return Set{}, fmt.Errorf("query: %s has no key", e.FullName())
	}

	set := Set{Entity: e}
	rpc := e.Rpc()

	if rpc.Has(schema.OpGet) {
		plans, err := readPlans(e)
		if err != nil {
			return Set{}, err
		}
		set.Plans = append(set.Plans, plans...)
	}
	if rpc.Has(schema.OpAdd) {
		set.Plans = append(set.Plans, insertPlan(e))
	}
	if rpc.Has(schema.OpPatch) {
		set.Plans = append(set.Plans, updatePlan(e))
	}
	if rpc.Has(schema.OpErase) {
		set.Plans = append(set.Plans, erasePlan(e))
	}
	return set, nil
}

// readPlans is one plan per candidate key: the primary key, plus every unique
// field, edge and index the entity exposes as a lookup.
func readPlans(e *schema.Entity) ([]*Plan, error) {
	var out []*Plan
	for _, key := range e.Keys() {
		p, err := readPlan(e, key)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// readPlan builds the lookup for one candidate key.
//
// It dispatches through schema.VisitElem, so adding an Elem variant is a
// compile error here. This is one of the three sites where the predecessor
// wrote `default: panic("unimplemented: key type not Field")`, and the one it
// never fixed: a unique edge used as a lookup key still crashes its generator.
func readPlan(e *schema.Entity, key schema.Elem) (*Plan, error) {
	return schema.VisitElem[*Plan](key, &readVisitor{entity: e})
}

type readVisitor struct{ entity *schema.Entity }

func (v *readVisitor) VisitField(f *schema.Field) (*Plan, error) {
	primary := f == v.entity.Key()

	id := "getBy" + pascal(string(f.Name()))
	kind := LookupIndex
	index := string(f.Name())
	if primary {
		id, kind, index = "get", LookupPrimary, ""
	}

	arg := Arg{
		Name: string(f.Names().Value),
		Path: refPath(primary, f),
		Prop: f,
	}
	return &Plan{
		ID:       id,
		Op:       OpGet,
		Entity:   v.entity,
		By:       &Lookup{Kind: kind, Index: index, Args: []int{0}},
		Args:     []Arg{arg},
		LiveOnly: v.entity.ErasesSoftly(),
	}, nil
}

// VisitEdge handles a unique edge used as a lookup key.
//
// The value supplied is the target's key, not the target itself: an edge is
// stored as a reference, so looking one up means naming the row it points at.
func (v *readVisitor) VisitEdge(e *schema.Edge) (*Plan, error) {
	if e.Target() == nil || e.Target().Key() == nil {
		return nil, fmt.Errorf("query: %s.%s points at an entity with no key",
			v.entity.FullName(), e.Name())
	}
	arg := Arg{
		Name: string(e.Names().Value),
		Path: append(refPath(false, e), e.Target().Key().Names().Value),
		Prop: e,
	}
	return &Plan{
		ID:       "getBy" + pascal(string(e.Name())),
		Op:       OpGet,
		Entity:   v.entity,
		By:       &Lookup{Kind: LookupIndex, Index: string(e.Name()), Args: []int{0}},
		Args:     []Arg{arg},
		LiveOnly: v.entity.ErasesSoftly(),
	}, nil
}

// VisitIndex handles a composite lookup: one argument per member, in index
// order, and an edge member contributes its target's key.
func (v *readVisitor) VisitIndex(idx *schema.Index) (*Plan, error) {
	args := make([]Arg, 0, len(idx.Props()))
	slots := make([]int, 0, len(idx.Props()))

	for i, p := range idx.Props() {
		path := refPath(false, p)
		if edge, isEdge := p.(*schema.Edge); isEdge {
			if edge.Target() == nil || edge.Target().Key() == nil {
				return nil, fmt.Errorf("query: %s.{indexes}(%s) member %s points at an entity with no key",
					v.entity.FullName(), idx.Name(), p.Name())
			}
			path = append(path, edge.Target().Key().Names().Value)
		}
		args = append(args, Arg{Name: string(p.Names().Value), Path: path, Prop: p})
		slots = append(slots, i)
	}

	return &Plan{
		ID:       "getBy" + pascal(string(idx.Name())),
		Op:       OpGet,
		Entity:   v.entity,
		By:       &Lookup{Kind: LookupIndex, Index: string(idx.Name()), Args: slots},
		Args:     args,
		LiveOnly: v.entity.ErasesSoftly(),
	}, nil
}

// refPath is where a lookup value lives in the request message.
//
// The primary key is the ref oneof's direct arm; every other candidate key
// wraps its members in a message of its own.
func refPath(primary bool, p schema.Prop) []schema.ValueName {
	if primary {
		return []schema.ValueName{"ref", "key", "value"}
	}
	return []schema.ValueName{"ref", "key", "value", p.Names().Value}
}

func insertPlan(e *schema.Entity) *Plan {
	p := &Plan{ID: "add", Op: OpInsert, Entity: e}

	for _, prop := range e.Props() {
		// The erased stamp is never written by a caller: a row is erased by
		// asking to erase it, not by assigning a date to it.
		if f, isField := prop.(*schema.Field); isField && f.IsErased() {
			continue
		}
		// The version is the server's to stamp.
		if f, isField := prop.(*schema.Field); isField && f.IsVersion() {
			p.Set = append(p.Set, Assign{Prop: prop, Now: true})
			continue
		}

		idx := len(p.Args)
		p.Args = append(p.Args, Arg{
			Name:     string(prop.Names().Value),
			Path:     []schema.ValueName{prop.Names().Value},
			Optional: prop.IsOptional(),
			Prop:     prop,
		})
		slot := idx
		p.Set = append(p.Set, Assign{Prop: prop, Arg: &slot})
	}
	return p
}

func updatePlan(e *schema.Entity) *Plan {
	p := &Plan{
		ID:       "patch",
		Op:       OpUpdate,
		Entity:   e,
		By:       &Lookup{Kind: LookupPrimary, Args: []int{0}},
		LiveOnly: e.ErasesSoftly(),
	}
	p.Args = append(p.Args, Arg{Name: "target", Path: []schema.ValueName{"target"}})

	for _, prop := range e.Props() {
		// The key names the row; changing it is not a patch.
		if prop == schema.Prop(e.Key()) {
			continue
		}
		// Immutable props are absent from the patch request entirely, so a
		// target that emitted a read for one would not compile.
		if prop.IsImmutable() {
			continue
		}
		if f, isField := prop.(*schema.Field); isField && (f.IsErased() || f.IsVersion()) {
			continue
		}

		idx := len(p.Args)
		p.Args = append(p.Args, Arg{
			Name:     string(prop.Names().Value),
			Path:     []schema.ValueName{prop.Names().Value},
			Optional: true,
			Prop:     prop,
		})
		slot := idx
		p.Set = append(p.Set, Assign{Prop: prop, Arg: &slot})

		// A nullable prop gets a companion that says "write null", because
		// proto's implicit presence cannot tell an absent value from a zero
		// one, and clearing is not the same as setting to zero.
		if prop.IsNullable() {
			null := len(p.Args)
			p.Args = append(p.Args, Arg{
				Name:     string(prop.Names().Value) + "Null",
				Path:     []schema.ValueName{prop.Names().Value + "Null"},
				Optional: true,
			})
			nullSlot := null
			p.Set = append(p.Set, Assign{Prop: prop, Arg: &nullSlot, Clear: true})
		}
	}

	if v := e.Version(); v != nil {
		guardArg := len(p.Args)
		p.Args = append(p.Args, Arg{
			Name: string(v.Names().Value),
			Path: []schema.ValueName{v.Names().Value},
			Prop: v,
		})
		forceArg := len(p.Args)
		p.Args = append(p.Args, Arg{
			Name:     string(v.Names().Value) + "Force",
			Path:     []schema.ValueName{v.Names().Value + "Force"},
			Optional: true,
		})
		p.Guard = &Guard{Prop: v, Arg: guardArg, Force: &forceArg}
		p.Set = append(p.Set, Assign{Prop: v, Now: true})
	}
	return p
}

// erasePlan stamps when the entity erases softly and removes when it does not.
func erasePlan(e *schema.Entity) *Plan {
	p := &Plan{
		ID:     "erase",
		Op:     OpDelete,
		Entity: e,
		By:     &Lookup{Kind: LookupPrimary, Args: []int{0}},
		Args:   []Arg{{Name: "target", Path: []schema.ValueName{"ref"}}},
	}
	if erased := e.Erased(); erased != nil {
		p.Op = OpErase
		p.LiveOnly = true
		p.Set = []Assign{{Prop: erased, Now: true}}
	}
	return p
}

// pascal upper-cases the first letter of each underscore-separated word.
//
// It is used only for a plan id, which is calque's own name for a plan and not
// a name in anybody's language. Identifiers in emitted code come from the
// descriptor or from a target's naming policy — never from here.
func pascal(s string) string {
	var out []rune
	upper := true
	for _, r := range s {
		if r == '_' || r == '-' {
			upper = true
			continue
		}
		if upper && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		upper = false
		out = append(out, r)
	}
	return string(out)
}
