package tsw_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/HeoJeongBo/calque/internal/tsw"
)

func TestPWritesLinesVerbatim(t *testing.T) {
	f := tsw.New()
	f.P("export const TableName = ", `"apptest.User"`, ";")
	f.P()
	f.P("\tconstructor(db: Db) {")

	require.Equal(t,
		"export const TableName = \"apptest.User\";\n\n\tconstructor(db: Db) {\n",
		f.String())
}

// TestPPreservesTrailingSpace is the reason there is no formatter.
//
// db.g.ts's `export type Db = ` really does end in a space, and the diff that
// proves calque is a drop-in is byte-for-byte.
func TestPPreservesTrailingSpace(t *testing.T) {
	f := tsw.New()
	f.P("export type Db = ")
	require.Equal(t, "export type Db = \n", f.String())
}

func TestPKeepsTabs(t *testing.T) {
	f := tsw.New()
	f.P("\t\tconst k = uuid.u8_str(v.id)")
	require.Contains(t, f.String(), "\t\t")
	require.NotContains(t, f.String(), "    ", "tabs are not spaces")
}

// TestLowerFirstIsNotCamelCase pins the spelling the consuming application
// depends on.
//
// This is protoc-gen-orm-ts's `camel`, and it is wrong in a specific way that
// 265 call sites and a hand-written ServiceClient now rely on. Replacing it
// with a correct camelCase is a breaking change dressed as a cleanup.
func TestLowerFirstIsNotCamelCase(t *testing.T) {
	for in, want := range map[string]string{
		"BTExecutor":   "bTExecutor",
		"BTFrame":      "bTFrame",
		"StationState": "stationState",
		"PosePreset":   "posePreset",
		"Robot":        "robot",
		"":             "",
	} {
		require.Equal(t, want, tsw.LowerFirst(in), "LowerFirst(%q)", in)
	}

	require.NotEqual(t, "btExecutor", tsw.LowerFirst("BTExecutor"),
		"a correct camelCase here would break every call site")
}

func TestStr(t *testing.T) {
	for in, want := range map[string]string{
		`apptest.User`: `"apptest.User"`,
		`&id,[a+b]`:    `"&id,[a+b]"`,
		`say "hi"`:     `"say \"hi\""`,
		`back\slash`:   `"back\\slash"`,
	} {
		got, err := tsw.Str(in)
		require.NoError(t, err)
		require.Equal(t, want, got, "Str(%q)", in)
	}
}

// TestStrRefusesControlCharacters: a generator that silently mangles a string
// is how a name stops matching the thing it names.
func TestStrRefusesControlCharacters(t *testing.T) {
	_, err := tsw.Str("a\x00b")
	require.ErrorContains(t, err, "control character")
}

// TestOutputIsStable: the same calls produce the same bytes, which is what
// makes a golden test meaningful.
func TestOutputIsStable(t *testing.T) {
	build := func() string {
		f := tsw.New()
		for _, name := range []string{"Tenant", "User", "Robot"} {
			f.P("import * as ", name, ` from "./`, tsw.LowerFirst(name), `.db.g";`)
		}
		return f.String()
	}
	require.Equal(t, build(), build())
}
