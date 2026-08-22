package dexie_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/backend/dexie"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/gentest"
	"github.com/heojeongbo/calque/schema"
)

func configured(t *testing.T, compat string) *dexie.Backend {
	t.Helper()
	b := dexie.New()
	section := ""
	if compat != "" {
		section = "dexie:\n  compat: " + compat + "\n"
	}
	gentest.Configure(t, b, section)
	return b
}

// TestContract runs the backend contract in both spellings, because compat is
// not a formatting option: it decides which name a row is indexed under, so a
// backend that holds in one mode and not the other holds in neither.
//
// The two Paths blocks below are conformance item 4 in the smallest form it has:
// the same prop, the same store, two names, and only one of them is what a
// protoc-gen-es message actually carries.
func TestContract(t *testing.T) {
	t.Run("compat", func(t *testing.T) {
		gentest.Run(t, gentest.Case{
			Backend: dexie.New(),
			Section: "dexie:\n  compat: orm-ts\n",
			Paths: map[string]schema.StorePath{
				// An edge is a nested key path, which is the load-bearing
				// difference from both SQL backends.
				"apptest.User.tenant": {"tenant", "id"},
				// The proto name -- and no row carries it.
				"namingtest.Device.token_hash": {"token_hash"},
			},
			Extra: map[string]map[string]any{
				"apptest.User": {"stores": "&id,[alias+tenant.id]"},
			},
		})
	})

	t.Run("none", func(t *testing.T) {
		gentest.Run(t, gentest.Case{
			Backend: dexie.New(),
			Section: "dexie:\n  compat: none\n  accept: [unique_compound_index, partial_index]\n",
			Paths: map[string]schema.StorePath{
				"apptest.User.tenant": {"tenant", "id"},
				// The name the row has.
				"namingtest.Device.token_hash": {"tokenHash"},
			},
			Codecs: map[string]gen.CodecName{
				"apptest.User.id": gen.CodecUUIDString,
				// IndexedDB holds a Date, so a timestamp is stored as it
				// arrives -- there is no time codec here and identity is the
				// honest answer.
				"apptest.User.date_updated": gen.CodecIdentity,
				"apptest.User.labels":       gen.CodecJSON,
				"apptest.User.profile":      gen.CodecJSON,
				"apptest.User.tenant":       gen.CodecUUIDString,
			},
		})
	})
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
			got, err := b.SchemaString(gentest.Entity(t, gentest.Schema(t, tc.file), tc.entity))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestSingleMemberIndexKeepsItsBrackets: "[device_id]" and "device_id" are
// different index names to Dexie, and the corpus has the bracketed form.
func TestSingleMemberIndexKeepsItsBrackets(t *testing.T) {
	b := configured(t, dexie.CompatORMTS)
	got, err := b.SchemaString(gentest.Entity(t, gentest.Schema(t, "naming.proto"), "namingtest.Device"))
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
	s := gentest.Schema(t, "naming.proto")
	e := gentest.Entity(t, s, "namingtest.Device")

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
	s := gentest.Schema(t, "naming.proto")
	e := gentest.Entity(t, s, "namingtest.Device")

	pair, ok := e.Index("pair")
	require.True(t, ok, "the schema has it")
	require.False(t, pair.IsUnique())

	got, err := configured(t, dexie.CompatORMTS).SchemaString(e)
	require.NoError(t, err)
	require.NotContains(t, got, "alias", "and the store never hears about it")
}

// TestMismatchesNamesWhatIsWrong: the bug is reproduced, and also reported.
func TestMismatchesNamesWhatIsWrong(t *testing.T) {
	s := gentest.Schema(t, "naming.proto")
	e := gentest.Entity(t, s, "namingtest.Device")

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
		gentest.Entity(t, gentest.Schema(t, "apptest.proto"), "apptest.Tenant")))
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
	s := gentest.Schema(t, "apptest.proto")
	e := gentest.Entity(t, s, "apptest.User")
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
	s := gentest.Schema(t, "apptest.proto")
	l, err := configured(t, dexie.CompatORMTS).Lower(s)
	require.NoError(t, err)

	table, err := l.Table(gentest.Entity(t, s, "apptest.User"))
	require.NoError(t, err)
	require.Equal(t, "&id,[alias+tenant.id]", table.Extra["stores"])
}

// TestEdgeIsAPathNotAColumn is the other half of entsql's
// TestEdgeIsAColumnNotAPath: the same edge, and the whole reason StorePath
// returns a sequence rather than a string.
//
// It is a direct call because Lower no longer makes one. It used to, to fill a
// per-prop path map that nothing read; asking the backend here is what keeps a
// StorePath bug from having to be found in a golden diff.
func TestEdgeIsAPathNotAColumn(t *testing.T) {
	s := gentest.Schema(t, "apptest.proto")
	tenant, ok := gentest.Entity(t, s, "apptest.User").Prop("tenant")
	require.True(t, ok)

	path, err := configured(t, dexie.CompatORMTS).StorePath(tenant)
	require.NoError(t, err)
	require.Equal(t, schema.StorePath{"tenant", "id"}, path)
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
		s := gentest.Schema(t, file)
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
