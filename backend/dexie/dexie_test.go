package dexie_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/backend/dexie"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	"github.com/heojeongbo/calque/schema"
)

func parse(t *testing.T, file string) *schema.Schema {
	t.Helper()

	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../../testdata/proto/valid", "../../testdata/proto/_upstream"},
		"", file)
	require.NoError(t, err)

	s, err := ormcompat.Parse(req)
	require.NoError(t, err)
	return s
}

func entity(t *testing.T, s *schema.Schema, name string) *schema.Entity {
	t.Helper()
	e, ok := s.Get(name)
	require.True(t, ok, "%s is missing", name)
	return e
}

func configured(t *testing.T, compat string) *dexie.Backend {
	t.Helper()
	b := dexie.New()
	doc := "version: 1\ntargets:\n  - {target: ts, backend: dexie}\n"
	if compat != "" {
		doc += "dexie:\n  compat: " + compat + "\n"
	}
	cfg, err := gen.ParseConfig([]byte(doc), "calque.yaml")
	require.NoError(t, err)
	require.NoError(t, b.Configure(cfg, "dexie"))
	return b
}

// TestSchemaStringGrammar reproduces the shapes the corpus contains.
//
// The grammar is Dexie's: "&" is unique, "[a+b]" is compound, "a.b" reaches
// into a nested object. A compound index gets no "&" because Dexie has no
// unique form of one -- which is exactly how the uniqueness disappears.
func TestSchemaStringGrammar(t *testing.T) {
	b := configured(t, dexie.CompatORMTS)

	for _, tc := range []struct {
		file, entity, want string
	}{
		// A unique field beside the key.
		{"apptest.proto", "apptest.Tenant", "&id,&alias"},
		// A compound index spanning a field and an edge: no "&", so the
		// uniqueness the schema asked for is not there.
		{"apptest.proto", "apptest.User", "&id,[alias+tenant.id]"},
		// A unique edge is a candidate key of its own.
		{"erased.proto", "erasedtest.Credential", "&id,&holder.id,&username"},
		// Nothing unique but the key.
		{"erased.proto", "erasedtest.Holder", "&id,&alias"},
	} {
		t.Run(tc.entity, func(t *testing.T) {
			got, err := b.SchemaString(entity(t, parse(t, tc.file), tc.entity))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestSingleMemberIndexKeepsItsBrackets: "[device_id]" and "device_id" are
// different index names to Dexie, and the corpus has the bracketed form.
func TestSingleMemberIndexKeepsItsBrackets(t *testing.T) {
	b := configured(t, dexie.CompatORMTS)
	got, err := b.SchemaString(entity(t, parse(t, "naming.proto"), "namingtest.Device"))
	require.NoError(t, err)
	require.Contains(t, got, "[device_id]")
	require.NotContains(t, got, ",device_id,")
}

// TestCompatSpellsIndexesWithTheProtoName is conformance item 4, isolated.
//
// The row carries `tokenHash`; compat mode declares the index as `token_hash`.
// That is the bug, reproduced on purpose so that swapping generators changes no
// bytes -- and the fix is one config value away.
func TestCompatSpellsIndexesWithTheProtoName(t *testing.T) {
	s := parse(t, "naming.proto")
	e := entity(t, s, "namingtest.Device")

	compat, err := configured(t, dexie.CompatORMTS).SchemaString(e)
	require.NoError(t, err)
	require.Equal(t, "&id,&token_hash,[device_id]", compat,
		"protoc-gen-orm-ts spells an index component with the proto name")

	fixed, err := configured(t, dexie.CompatNone).SchemaString(e)
	require.NoError(t, err)
	require.Equal(t, "&id,&tokenHash,&deviceId", fixed,
		"the row actually carries the JSON name, and a one-member index is a plain one")
}

// TestNonUniqueIndexIsNotEmitted records a gap rather than a design.
//
// The schema string is built from Entity.Keys(), which is the unique elements,
// so a `unique: false` index never reaches the store at all -- it is parsed,
// validated, and then silently dropped. The `pair` index in the fixture is one.
//
// That is what protoc-gen-orm-ts does, so calque reproduces it here. It is a
// conformance item of its own: a declared index that does not exist is a query
// plan that will table-scan, and nothing says so today.
func TestNonUniqueIndexIsNotEmitted(t *testing.T) {
	s := parse(t, "naming.proto")
	e := entity(t, s, "namingtest.Device")

	pair, ok := e.Index("pair")
	require.True(t, ok, "the schema has it")
	require.False(t, pair.IsUnique())

	got, err := configured(t, dexie.CompatORMTS).SchemaString(e)
	require.NoError(t, err)
	require.NotContains(t, got, "alias", "and the store never hears about it")
}

// TestMismatchesNamesWhatIsWrong: the bug is reproduced, and also reported.
func TestMismatchesNamesWhatIsWrong(t *testing.T) {
	s := parse(t, "naming.proto")
	e := entity(t, s, "namingtest.Device")

	got := configured(t, dexie.CompatORMTS).Mismatches(e)
	var names []schema.ProtoName
	for _, p := range got {
		names = append(names, p.Name())
	}
	require.Contains(t, names, schema.ProtoName("token_hash"))
	require.Contains(t, names, schema.ProtoName("device_id"))

	require.Empty(t, configured(t, dexie.CompatNone).Mismatches(e),
		"with the spelling fixed there is nothing to report")

	// Single-word keys are the same in both spellings, which is why the bug
	// stayed hidden for so long.
	require.Empty(t, configured(t, dexie.CompatORMTS).Mismatches(
		entity(t, parse(t, "apptest.proto"), "apptest.Tenant")))
}

// TestCapabilitiesStateFactsNotWishes: these are what IndexedDB can do, and
// they are what makes the refusal in gen/capability.go meaningful.
func TestCapabilities(t *testing.T) {
	caps := dexie.New().Capabilities()
	// This said false until it was measured. `&[a+b]` parses as unique and
	// compound, and IndexedDB enforces unique on an array key path; the
	// predecessor's `[a+b]` dropped the constraint, and "the store cannot hold
	// it" was an inference from that rather than a fact about the store. See the
	// runtime package's tests, which put two colliding rows in and get a
	// ConstraintError.
	require.True(t, caps.UniqueCompoundIndex, "Dexie holds `&[a+b]`")
	require.False(t, caps.PartialIndex, "IndexedDB has no partial index")
	require.False(t, caps.BinaryKey, "which is why a uuid is stored as text")
	require.True(t, caps.NestedKeyPath)
	require.True(t, caps.Supports(gen.CodecUUIDString))
}

// TestStrictOnlyOnceTheBugsAreFixed: refusing while reproducing an older
// generator would mean generating nothing for any schema that already has a
// unique compound index.
func TestStrictOnlyOnceTheBugsAreFixed(t *testing.T) {
	require.False(t, configured(t, dexie.CompatORMTS).Strict())
	require.True(t, configured(t, dexie.CompatNone).Strict())
}

func TestCompatValueIsChecked(t *testing.T) {
	b := dexie.New()
	cfg, err := gen.ParseConfig([]byte("version: 1\ntargets:\n  - {target: ts, backend: dexie}\ndexie:\n  compat: nope\n"), "calque.yaml")
	require.NoError(t, err)
	require.ErrorContains(t, b.Configure(cfg, "dexie"), `compat "nope"`)
}

// TestUUIDBecomesTextOnBothSides is conformance item 7 from the Dexie side.
func TestUUIDBecomesText(t *testing.T) {
	s := parse(t, "apptest.proto")
	e := entity(t, s, "apptest.User")
	b := configured(t, dexie.CompatORMTS)

	codec, err := b.Codec(e.Key())
	require.NoError(t, err)
	require.Equal(t, gen.CodecUUIDString, codec,
		"IndexedDB cannot index a byte array, and the Go side stores the same text")

	// An edge's codec is its target's key's, because that is what is stored.
	tenant, ok := e.Prop("tenant")
	require.True(t, ok)
	codec, err = b.Codec(tenant)
	require.NoError(t, err)
	require.Equal(t, gen.CodecUUIDString, codec)
}

func TestLowerCarriesTheSchemaString(t *testing.T) {
	s := parse(t, "apptest.proto")
	l, err := configured(t, dexie.CompatORMTS).Lower(s)
	require.NoError(t, err)

	table, err := l.Table(entity(t, s, "apptest.User"))
	require.NoError(t, err)
	require.Equal(t, "apptest.User", table.Name)
	require.Equal(t, "&id,[alias+tenant.id]", table.Extra["stores"])

	// An edge's stored path reaches into the nested ref.
	tenant, _ := entity(t, s, "apptest.User").Prop("tenant")
	require.Equal(t, schema.StorePath{"tenant", "id"}, table.Path[tenant])
}

// TestAcceptsIsPerKindOnceStrict: compat accepts everything by definition, and
// once the spelling is fixed only what the config named gets through.
func TestAcceptsIsPerKindOnceStrict(t *testing.T) {
	compat := configured(t, dexie.CompatORMTS)
	for _, k := range gen.AllShortfallKinds() {
		require.True(t, compat.Accepts(k),
			"reproducing a generator means not refusing what it produced: %s", k)
	}

	strict := configured(t, dexie.CompatNone)
	for _, k := range gen.AllShortfallKinds() {
		require.False(t, strict.Accepts(k), "nothing named, nothing accepted: %s", k)
	}
}

func TestAcceptNamesOneKind(t *testing.T) {
	b := dexie.New()
	cfg, err := gen.ParseConfig([]byte(`
version: 1
targets:
  - {target: ts, backend: dexie}
dexie:
  compat: none
  accept:
    - unique_compound_index
`), "calque.yaml")
	require.NoError(t, err)
	require.NoError(t, b.Configure(cfg, "dexie"))

	require.True(t, b.Accepts(gen.ShortfallUniqueCompoundIndex))
	require.False(t, b.Accepts(gen.ShortfallBinaryKey),
		"a list accepts what it lists; a boolean would have accepted this too")
}

// TestAcceptKindIsChecked: a misspelled kind would accept nothing while looking
// like it accepted something, which is the failure this option exists to avoid.
func TestAcceptKindIsChecked(t *testing.T) {
	b := dexie.New()
	cfg, err := gen.ParseConfig([]byte(`
version: 1
targets:
  - {target: ts, backend: dexie}
dexie:
  accept: [uniqe_compound_index]
`), "calque.yaml")
	require.NoError(t, err)

	err = b.Configure(cfg, "dexie")
	require.ErrorContains(t, err, `accept "uniqe_compound_index"`)
	require.ErrorContains(t, err, "calque knows:")
	require.ErrorContains(t, err, "unique_compound_index")
}

// TestEveryDeclaredIndexIsEnforced is the invariant the one-member bug slipped
// through, and it is one line of assertion because that is all it needed to be.
//
// Every part of a schema string comes from Entity.Keys(), which yields only
// unique elements. So every part must be marked unique -- if one is not, the
// generator is declaring an index the store will not enforce while the
// capability check reports nothing, because IsComposite() is len > 1 and a
// one-member index is not composite.
//
// `[deviceId]` fails this. So would any future shape that forgets the "&".
func TestEveryDeclaredIndexIsEnforced(t *testing.T) {
	b := configured(t, dexie.CompatNone)

	for _, file := range []string{"apptest.proto", "erased.proto", "naming.proto"} {
		s := parse(t, file)
		for _, e := range s.Entities() {
			str, err := b.SchemaString(e)
			require.NoError(t, err)

			for i, part := range strings.Split(str, ",") {
				require.True(t, strings.HasPrefix(part, "&"),
					"%s: part %d of %q is not unique, but every part comes from Keys()",
					e.FullName(), i, str)

				// And a one-member index must not be bracketed: "[a]" registers
				// under that name, and where({a}) looks for "a".
				inner := strings.TrimPrefix(part, "&")
				if strings.HasPrefix(inner, "[") {
					require.Contains(t, inner, "+",
						"%s: %q is a one-member index in compound syntax, which no query can find",
						e.FullName(), part)
				}
			}
		}
	}
}
