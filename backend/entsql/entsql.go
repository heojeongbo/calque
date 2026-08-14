// Package entsql is the ent-over-SQL backend.
//
// It is the other side of the comparison docs/conformance.md is about. Where
// the Dexie backend cannot hold a unique index over more than one property,
// this one can — ent writes index.Fields("alias").Edges("tenant").Unique() and
// the database holds it. Same schema, two backends, two honest answers, which
// is what Capabilities exists to say.
//
// Unlike Dexie, there is no runtime adapter to render the syntax: ent's API is
// per-entity generated types, so `predicate.Robot` and `predicate.Run` are
// distinct named types with no common method set and nothing can abstract over
// them. The Go target therefore renders ent calls directly, and this package
// contributes what a backend always contributes — capabilities, store paths,
// codecs — plus the ent-specific decisions the target asks it for.
package entsql

import (
	"fmt"
	"strings"

	"github.com/HeoJeongBo/calque/gen"
	"github.com/HeoJeongBo/calque/internal/entname"
	"github.com/HeoJeongBo/calque/schema"
)

// Dialect is the SQL a generated client speaks. It changes what the store can
// hold, so it is a capability question and not a formatting one.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
	DialectMySQL    Dialect = "mysql"
)

// Options is the `entsql:` config section.
type Options struct {
	// Dialect decides the capability set. MySQL has no partial index, so an
	// entity that erases softly cannot have a unique index that covers only the
	// live rows.
	Dialect Dialect `yaml:"dialect"`

	// Table renames a physical table. The default is the entity name
	// lower-cased whole, which is what pins it against ent's own default of a
	// snake-cased plural.
	Table map[string]string `yaml:"table"`
}

// Backend is the ent/SQL backend.
type Backend struct {
	opts Options
}

func New() *Backend { return &Backend{opts: Options{Dialect: DialectSQLite}} }

func (b *Backend) Name() string { return "entsql" }

func (b *Backend) Configure(cfg *gen.Config, section string) error {
	opts := Options{Dialect: DialectSQLite}
	if _, err := cfg.Section(section, &opts); err != nil {
		return err
	}
	switch opts.Dialect {
	case DialectSQLite, DialectPostgres, DialectMySQL:
	case "":
		opts.Dialect = DialectSQLite
	default:
		return fmt.Errorf("entsql: dialect %q is not sqlite, postgres or mysql", opts.Dialect)
	}
	b.opts = opts
	return nil
}

// Strict is true: a relational store is expected to hold what it says it holds,
// so a shortfall here is a schema to fix rather than a bug to reproduce.
func (b *Backend) Strict() bool { return true }

func (b *Backend) Capabilities() gen.Capabilities {
	caps := gen.Capabilities{
		// The load-bearing difference from Dexie.
		UniqueCompoundIndex: true,
		PartialIndex:        true,
		// An edge is a foreign-key column, not a path into a nested object.
		NestedKeyPath: false,
		BinaryKey:     true,
		Transactions:  true,
		Codecs: []gen.CodecName{
			gen.CodecIdentity,
			gen.CodecUUIDString,
			gen.CodecTimeNative,
			gen.CodecJSON,
		},
	}
	switch b.opts.Dialect {
	case DialectMySQL:
		// No partial indexes, so a unique index cannot be narrowed to the rows
		// that are still there.
		caps.PartialIndex = false
	case DialectPostgres:
		caps.MaxIndexArity = 32
	}
	return caps
}

// StorePath is the column a prop lives in.
//
// An edge becomes a single foreign-key column named after the entity and the
// edge — `robot_tenant` — which is ent's own convention and what the physical
// schema already has. It is one name, not a path, because SQL has no nested
// key path to reach through.
func (b *Backend) StorePath(p schema.Prop) (schema.StorePath, error) {
	return schema.VisitProp[schema.StorePath](p, storePathVisitor{})
}

type storePathVisitor struct{}

func (storePathVisitor) VisitField(f *schema.Field) (schema.StorePath, error) {
	return schema.StorePath{schema.StoreName(f.Name())}, nil
}

func (storePathVisitor) VisitEdge(e *schema.Edge) (schema.StorePath, error) {
	if e.Entity() == nil {
		return nil, fmt.Errorf("entsql: %s is not attached to an entity", e.Name())
	}
	return schema.StorePath{schema.StoreName(
		strings.ToLower(e.Entity().Name()) + "_" + string(e.Name()),
	)}, nil
}

// Codec is what a value becomes on the way into a column.
//
// A uuid is canonical text, because google/uuid's driver.Valuer writes it that
// way — the same text the Dexie side stores. Conformance item 7 is that these
// two agree, and they agree here by naming the same codec rather than by
// coincidence.
func (b *Backend) Codec(p schema.Prop) (gen.CodecName, error) {
	switch p.Type() {
	case schema.TypeUUID:
		return gen.CodecUUIDString, nil
	case schema.TypeTime:
		return gen.CodecTimeNative, nil
	case schema.TypeJSON:
		return gen.CodecJSON, nil
	case schema.TypeMessage:
		if e, ok := p.(*schema.Edge); ok && e.Target() != nil && e.Target().Key() != nil {
			return b.Codec(e.Target().Key())
		}
		return gen.CodecJSON, nil
	default:
		return gen.CodecIdentity, nil
	}
}

// TableName is the physical table.
//
// The default is the entity name lower-cased whole, which is not ent's default:
// ent would say `pose_presets`. The generated schema pins it with an
// entsql.Annotation for exactly that reason, and changing it renames a table.
func (b *Backend) TableName(e *schema.Entity) string {
	if name, ok := b.opts.Table[e.FullName()]; ok {
		return name
	}
	return strings.ToLower(e.Name())
}

// EntIdent is the Go identifier ent gives a prop.
//
// It is not protoc-gen-go's: `device_id` is `DeviceID` here and `DeviceId`
// there. A generator emitting both has to spell it both ways.
func (b *Backend) EntIdent(p schema.Prop) string { return entname.Pascal(string(p.Name())) }

func (b *Backend) Lower(s *schema.Schema) (*gen.Lowered, error) {
	l := &gen.Lowered{Schema: s, Backend: b.Name(), Tables: map[*schema.Entity]*gen.Table{}}

	for _, e := range s.Entities() {
		if e.Key() == nil {
			return nil, fmt.Errorf("entsql: %s has no key", e.FullName())
		}

		t := &gen.Table{
			Entity: e,
			Name:   b.TableName(e),
			Path:   map[schema.Prop]schema.StorePath{},
			Codec:  map[schema.Prop]gen.CodecName{},
			Extra:  map[string]any{"dialect": string(b.opts.Dialect)},
		}

		for _, p := range e.Props() {
			path, err := b.StorePath(p)
			if err != nil {
				return nil, fmt.Errorf("entsql: %s.%s: %w", e.FullName(), p.Name(), err)
			}
			codec, err := b.Codec(p)
			if err != nil {
				return nil, fmt.Errorf("entsql: %s.%s: %w", e.FullName(), p.Name(), err)
			}
			t.Path[p] = path
			t.Codec[p] = codec
		}

		key := gen.StoredIndex{
			Name:        "PRIMARY",
			Members:     []schema.StorePath{t.Path[e.Key()]},
			Unique:      true,
			Enforcement: gen.EnforceStore,
			Source:      e.Key(),
		}
		t.PrimaryKey = key

		for _, idx := range e.Indexes() {
			members := make([]schema.StorePath, 0, len(idx.Props()))
			for _, p := range idx.Props() {
				members = append(members, t.Path[p])
			}
			t.Indexes = append(t.Indexes, gen.StoredIndex{
				Name:        string(idx.Name()),
				Members:     members,
				Unique:      idx.IsUnique(),
				Enforcement: gen.EnforceStore,
				Source:      idx,
			})
		}

		l.Tables[e] = t
	}
	return l, nil
}
