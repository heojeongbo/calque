// Package grdb is the GRDB/SQLite backend.
//
// It is what the Swift target stores into, and it sits in the same place Dexie
// does for TypeScript: a local store on the device, fed by a server that holds
// the record. The difference from Dexie is that this one is relational, so the
// constraints a schema states are constraints the store actually keeps —
// `UniqueCompoundIndex` and `PartialIndex` are both true here and both false
// there, and that is the whole reason a second local backend was worth having.
//
// Like every backend this contributes storage policy only: what the store can
// hold, which column a value lives in, and what transform it goes through. The
// GRDB calls themselves are the Swift runtime adapter's, not this file's.
package grdb

import (
	"fmt"
	"slices"
	"strings"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/schema"
)

// Options is the `grdb:` config section.
type Options struct {
	// Accept is the capability shortfalls to warn about rather than refuse,
	// named one at a time.
	//
	// SQLite can hold everything the vocabulary can state, so this should stay
	// empty. It exists because a schema can still out-run a *specific* build of
	// SQLite -- an old system library without partial indexes, say -- and the
	// answer to that is naming the one shortfall, not turning strictness off.
	Accept []string `yaml:"accept"`
}

// Backend is the GRDB backend.
type Backend struct {
	opts Options
}

// New returns the backend.
func New() *Backend { return &Backend{} }

func (b *Backend) Name() string { return "grdb" }

func (b *Backend) Configure(cfg *gen.Config, section string) error {
	var opts Options
	if _, err := cfg.Section(section, &opts); err != nil {
		return err
	}

	// A misspelled kind would accept nothing while looking like it accepted
	// something, which is the failure this option exists to avoid.
	known := map[string]bool{}
	for _, k := range gen.AllShortfallKinds() {
		known[string(k)] = true
	}
	for _, name := range opts.Accept {
		if !known[name] {
			return fmt.Errorf("grdb: accept %q is not a shortfall kind\n\tcalque knows: %s",
				name, strings.Join(gen.ShortfallKindNames(), ", "))
		}
	}

	b.opts = opts
	return nil
}

// Strict is true. SQLite can hold what the vocabulary states, so a shortfall
// here is a real one and stopping is the right answer.
func (b *Backend) Strict() bool { return true }

// Accepts refines Strict per kind, for the build of SQLite that cannot.
func (b *Backend) Accepts(kind gen.ShortfallKind) bool {
	return slices.Contains(b.opts.Accept, string(kind))
}

// Capabilities states what SQLite holds.
func (b *Backend) Capabilities() gen.Capabilities {
	return gen.Capabilities{
		// `UNIQUE (a, b)` and `CREATE UNIQUE INDEX ... ON t (a, b)`.
		UniqueCompoundIndex: true,
		// `CREATE UNIQUE INDEX ... WHERE date_erased IS NULL` -- which is what a
		// unique index on a softly-erasing entity needs, and what a document
		// store cannot express.
		PartialIndex: true,
		// A column is flat. An edge is a foreign-key column, not a path.
		NestedKeyPath: false,
		// A BLOB can be a key and an index member.
		BinaryKey:    true,
		Transactions: true,
		Codecs: []gen.CodecName{
			gen.CodecIdentity,
			gen.CodecUUIDString,
			gen.CodecTimeEpochMillis,
			gen.CodecJSON,
		},
	}
}

// StorePath is the column a prop lives in.
//
// A field is its own column. An edge is the target's key in a column named for
// the edge with `_id`, which is the same shape entsql chooses -- so a row here
// and a row there line up column for column, and a person reading both is not
// translating between two spellings.
func (b *Backend) StorePath(p schema.Prop) (schema.StorePath, error) {
	return schema.VisitProp[schema.StorePath](p, storePathVisitor{})
}

type storePathVisitor struct{}

func (storePathVisitor) VisitField(f *schema.Field) (schema.StorePath, error) {
	return schema.StorePath{schema.StoreName(f.Names().Proto)}, nil
}

func (storePathVisitor) VisitEdge(e *schema.Edge) (schema.StorePath, error) {
	if e.Target() == nil || e.Target().Key() == nil {
		return nil, fmt.Errorf("grdb: %s points at an entity with no key", e.Name())
	}
	return schema.StorePath{schema.StoreName(string(e.Names().Proto) + "_id")}, nil
}

// Codec is how a value is written.
//
// A uuid becomes canonical text rather than sixteen bytes, even though SQLite
// would hold the bytes. The reason is not storage: it is that the server stores
// the same text (entsql chooses `uuid-string` too), so a value copied from one
// side to the other is the same string, and a person reading the local database
// with the sqlite3 CLI sees the id they can search the server logs for.
//
// A time becomes epoch milliseconds because SQLite has no time type. Leaving it
// to the runtime would mean GRDB's default -- a "YYYY-MM-DD HH:MM:SS.SSS"
// string in an unstated zone -- and an unstated zone is how two devices disagree
// about which of two writes is newer.
func (b *Backend) Codec(p schema.Prop) (gen.CodecName, error) {
	switch p.Type() {
	case schema.TypeUUID:
		return gen.CodecUUIDString, nil
	case schema.TypeTime:
		return gen.CodecTimeEpochMillis, nil
	case schema.TypeJSON:
		return gen.CodecJSON, nil
	case schema.TypeMessage:
		// An edge is stored as its target's key, so its codec is the target's.
		if e, ok := p.(*schema.Edge); ok && e.Target() != nil && e.Target().Key() != nil {
			return b.Codec(e.Target().Key())
		}
		return gen.CodecJSON, nil
	default:
		return gen.CodecIdentity, nil
	}
}

func (b *Backend) Lower(s *schema.Schema) (*gen.Lowered, error) {
	l := &gen.Lowered{Schema: s, Backend: b.Name(), Tables: map[*schema.Entity]*gen.Table{}}

	for _, e := range s.Entities() {
		if e.Key() == nil {
			return nil, fmt.Errorf("grdb: %s has no key", e.FullName())
		}

		t := &gen.Table{
			Entity: e,
			Codec:  map[schema.Prop]gen.CodecName{},
			Extra:  map[string]any{"table": TableName(e)},
		}
		for _, p := range e.Props() {
			codec, err := b.Codec(p)
			if err != nil {
				return nil, fmt.Errorf("grdb: %s.%s: %w", e.FullName(), p.Name(), err)
			}
			t.Codec[p] = codec
		}
		l.Tables[e] = t
	}
	return l, nil
}

// TableName is the table an entity lives in: snake_case, unpluralised.
//
// Unpluralised because pluralising is a language question with no answer a
// generator should be guessing at, and because the entity name is right there
// in the proto for anyone matching the two up.
func TableName(e *schema.Entity) string { return snake(string(e.Name())) }

// Column renders a StorePath the way SQL spells one: joined, because there is
// no nesting to preserve.
func (b *Backend) Column(p schema.Prop) (string, error) {
	path, err := b.StorePath(p)
	if err != nil {
		return "", err
	}
	parts := make([]string, len(path))
	for i, n := range path {
		parts[i] = snake(string(n))
	}
	return strings.Join(parts, "_"), nil
}

// snake turns "BoxFolder" and "dateCreated" into "box_folder" and "date_created".
func snake(s string) string {
	var sb strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				sb.WriteByte('_')
			}
			sb.WriteRune(r - 'A' + 'a')
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
