package prow_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/internal/prow"
)

func TestBlockIndents(t *testing.T) {
	f := prow.New()
	f.Block("service UserService", func() {
		f.P("rpc Get(UserGetRequest) returns (User);")
	})

	require.Equal(t, "service UserService {\n\trpc Get(UserGetRequest) returns (User);\n}\n", f.String())
	require.Zero(t, f.Depth(), "Block must not leak a level")
}

func TestNestedBlocks(t *testing.T) {
	f := prow.New()
	f.Block("message UserRef", func() {
		f.Block("oneof key", func() {
			f.Field("", "bytes", "id", 1)
		})
	})

	require.Equal(t, "message UserRef {\n\toneof key {\n\t\tbytes id = 1;\n\t}\n}\n", f.String())
}

// TestBlankLinesAreNotIndented: a blank line inside a block is empty, not a tab.
//
// Trailing whitespace on an otherwise empty line is invisible in review and shows
// up in a byte comparison, which is the only comparison that matters here.
func TestBlankLinesAreNotIndented(t *testing.T) {
	f := prow.New()
	f.Block("message M", func() {
		f.P("bytes id = 1;")
		f.P()
		f.P("string alias = 2;")
	})

	require.Equal(t, "message M {\n\tbytes id = 1;\n\n\tstring alias = 2;\n}\n", f.String())
}

func TestFieldOptions(t *testing.T) {
	f := prow.New()
	f.In()
	f.Field("", "bytes", "id", 1, "features.field_presence = IMPLICIT")

	require.Equal(t, "\tbytes id = 1 [\n\t\tfeatures.field_presence = IMPLICIT\n\t];\n", f.String())
}

// TestFieldOptionsCommas: every option but the last takes a trailing comma.
func TestFieldOptionsCommas(t *testing.T) {
	f := prow.New()
	f.Field("", "string", "lock", 8, "features.field_presence = EXPLICIT", "deprecated = true")

	require.Equal(t,
		"string lock = 8 [\n\tfeatures.field_presence = EXPLICIT,\n\tdeprecated = true\n];\n",
		f.String())
}

func TestFieldLabel(t *testing.T) {
	f := prow.New()
	f.Field("repeated", "Ref", "refs", 1)
	f.Field("", "map<string, string>", "labels", 7)

	require.Equal(t, "repeated Ref refs = 1;\nmap<string, string> labels = 7;\n", f.String())
}

// TestOutAtZeroDoesNotPanic: an unbalanced emitter is a visible bug in the output,
// not a crash in a plugin whose stdout is the protocol.
func TestOutAtZeroDoesNotPanic(t *testing.T) {
	f := prow.New()
	f.Out()
	f.P("x")

	require.Equal(t, "x\n", f.String())
	require.Zero(t, f.Depth())
}

func TestComment(t *testing.T) {
	f := prow.New()
	f.In()
	f.Comment("Add creates a new User")

	require.Equal(t, "\t// Add creates a new User\n", f.String())
}

func TestCommentBlankLine(t *testing.T) {
	f := prow.New()
	f.Comment("one\n\ntwo")

	require.Equal(t, "// one\n//\n// two\n", f.String())
}

func TestQuote(t *testing.T) {
	require.Equal(t, `"hday.io/oasys/oas"`, prow.Quote("hday.io/oasys/oas"))
	require.Equal(t, `"a\"b"`, prow.Quote(`a"b`))
}
