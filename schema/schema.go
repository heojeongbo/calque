// Package schema is calque's language-neutral model of an annotated proto
// schema: entities, their fields and edges and indexes, and what is unique.
//
// It knows nothing about proto options and nothing about any target language or
// storage engine. ormcompat turns annotations into this; targets and backends
// read this and nothing else. That boundary is the reason a second language and
// a second store are additions rather than rewrites.
package schema

import (
	"fmt"
	"sort"
)

// Schema is one generation's entities.
//
// Order is part of the contract -- golden output is compared byte for byte --
// so entities stay in the order Build was given them, which is file order and
// then declaration order.
type Schema struct {
	entities []*Entity
	byName   map[string]*Entity
}

// Entities is every entity, in stable order.
func (s *Schema) Entities() []*Entity { return s.entities }

// Sources is the entities whose file was in files-to-generate.
//
// An entity reachable only as an edge target is in Entities and not here: it
// has to be understood so the edge means something, and it must not be emitted
// or two runs would both claim its file.
func (s *Schema) Sources() []*Entity {
	var out []*Entity
	for _, e := range s.entities {
		if e.Generate() {
			out = append(out, e)
		}
	}
	return out
}

// Get finds an entity by full name.
func (s *Schema) Get(fullName string) (*Entity, bool) {
	e, ok := s.byName[fullName]
	return e, ok
}

// Build wires entities into a schema: it attaches every member to its entity,
// resolves index refs and edge targets, and finds the key, version and erased
// fields.
//
// It reports what is structurally impossible -- a ref naming a prop that is not
// there, an edge pointing at an entity nobody defined -- and leaves what is
// merely wrong to the compat layer's validation, which can say it better
// because it still knows which annotation said it.
func Build(entities []*Entity) (*Schema, error) {
	s := &Schema{
		entities: entities,
		byName:   make(map[string]*Entity, len(entities)),
	}

	var diags Diagnostics

	for _, e := range entities {
		if prev, dup := s.byName[e.FullName()]; dup {
			diags.Addf(e.File(), "%s is defined twice, in %s and %s",
				e.FullName(), prev.File(), e.File())
			continue
		}
		s.byName[e.FullName()] = e
	}

	for _, e := range entities {
		attach(e, &diags)
	}
	for _, e := range entities {
		resolveIndexes(e, &diags)
	}
	for _, e := range entities {
		resolveEdges(s, e, &diags)
	}
	for _, e := range entities {
		resolveBackReferences(e, &diags)
	}

	if err := diags.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// attach points every member at its entity and works out which fields are
// special.
func attach(e *Entity, diags *Diagnostics) {
	at := diags.At(e.FullName())

	e.props = nil
	e.elems = nil

	for _, f := range e.spec.Fields {
		f.parent = e
		e.props = append(e.props, f)
	}
	for _, ed := range e.spec.Edges {
		ed.parent = e
		e.props = append(e.props, ed)
	}

	// Declaration order across both kinds, so that a schema's shape does not
	// depend on which slice a member happened to land in.
	sort.SliceStable(e.props, func(i, j int) bool {
		return e.props[i].Number() < e.props[j].Number()
	})

	for _, p := range e.props {
		e.elems = append(e.elems, p)
	}
	for _, i := range e.spec.Indexes {
		i.parent = e
		e.elems = append(e.elems, i)
	}

	for _, f := range e.spec.Fields {
		switch {
		case f.IsVersion():
			if e.version != nil {
				at.Addf(string(f.Name()), "there can be only one version field; %s is already one", e.version.Name())
				continue
			}
			e.version = f
		case f.IsErased():
			if e.erased != nil {
				at.Addf(string(f.Name()), "there can be only one erased field; %s is already one", e.erased.Name())
				continue
			}
			e.erased = f
		}
	}
}

// SetKey names the entity's primary key. The compat layer calls it once it has
// checked that the field is allowed to be one.
func (e *Entity) SetKey(f *Field) { e.key = f }

func resolveIndexes(e *Entity, diags *Diagnostics) {
	at := diags.At(e.FullName())

	for _, idx := range e.spec.Indexes {
		where := fmt.Sprintf("{indexes}(%s)", idx.Name())
		if len(idx.spec.Refs) == 0 {
			at.Add(where, "an index must reference at least one prop")
			continue
		}

		idx.props = nil
		for n, ref := range idx.spec.Refs {
			p, ok := e.Prop(ref.Name)
			if !ok {
				at.Addf(fmt.Sprintf("%s.refs[%d]", where, n),
					"no prop named %s", ref.Name)
				continue
			}
			// Both halves have to agree. Checking only the name would let a
			// renumbered field keep an index that no longer describes it.
			if p.Number() != ref.Number {
				at.Hintf(fmt.Sprintf("%s.refs[%d]", where, n),
					fmt.Sprintf("%s is field %d, not %d", ref.Name, p.Number(), ref.Number),
					"a ref names a prop by name and number, so that renaming or renumbering one is loud rather than silent")
				continue
			}
			idx.props = append(idx.props, p)
		}
	}
}

func resolveEdges(s *Schema, e *Entity, diags *Diagnostics) {
	at := diags.At(e.FullName())

	for _, ed := range e.spec.Edges {
		target, ok := s.Get(ed.spec.TargetName)
		if !ok {
			at.Hintf(string(ed.Name()),
				fmt.Sprintf("%s is not an entity", ed.spec.TargetName),
				"an edge points at a message that carries option (orm.message); add it there, or make this a field")
			continue
		}
		ed.target = target
	}
}

// resolveBackReferences links the two halves of a relation.
//
// `from:` on one edge names the edge on the target that it is the reverse of.
// Both directions are recorded, because a target that has one half and needs
// the other should not have to search for it.
func resolveBackReferences(e *Entity, diags *Diagnostics) {
	at := diags.At(e.FullName())

	for _, ed := range e.spec.Edges {
		if ed.spec.FromName == "" || ed.target == nil {
			continue
		}

		where := string(ed.Name())
		p, ok := ed.target.Prop(ProtoName(ed.spec.FromName))
		if !ok {
			at.Addf(where, "back reference names %s, which %s does not have",
				ed.spec.FromName, ed.target.FullName())
			continue
		}
		if p.Number() != ed.spec.FromNumber {
			at.Addf(where, "back reference names %s as field %d, but it is field %d",
				ed.spec.FromName, ed.spec.FromNumber, p.Number())
			continue
		}
		other, ok := p.(*Edge)
		if !ok {
			at.Addf(where, "back reference names %s, which is a field rather than an edge",
				ed.spec.FromName)
			continue
		}

		// A unique other half means at most one row on that side, so this side
		// cannot be a list: the two statements contradict each other and only
		// one of them can be what was meant.
		if other.IsUnique() && ed.spec.List {
			at.Addf(where, "back reference %s is unique, so this edge cannot be repeated",
				ed.spec.FromName)
			continue
		}

		ed.inverse = other
		other.reverse = ed

		// Neither side repeated is a one-to-one relation, and both ends of one
		// are unique. It is derived rather than declared because it is not a
		// choice: saying `from:` on a singular edge to a singular edge has
		// already said it.
		if !ed.spec.List && !other.spec.List {
			ed.spec.Unique = true
			other.spec.Unique = true
		}
	}
}
