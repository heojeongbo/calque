package entname_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/HeoJeongBo/calque/internal/entname"
)

func TestPascal(t *testing.T) {
	for in, want := range map[string]string{
		"user_info":    "UserInfo",
		"full_name":    "FullName",
		"user_id":      "UserID",
		"device_id":    "DeviceID",
		"token_hash":   "TokenHash",
		"id":           "ID",
		"alias":        "Alias",
		"date_updated": "DateUpdated",
		"full-admin":   "FullAdmin",
		"work_window":  "WorkWindow",
		"utc_offset":   "UtcOffset",
		"":             "",
	} {
		require.Equal(t, want, entname.Pascal(in), "Pascal(%q)", in)
	}
}

func TestCamel(t *testing.T) {
	for in, want := range map[string]string{
		"user_info": "userInfo",
		"user_id":   "userID",
		"device_id": "deviceID",
		"alias":     "alias",
		"":          "",
	} {
		require.Equal(t, want, entname.Camel(in), "Camel(%q)", in)
	}
}

// TestEntAndProtocDisagree is the whole reason this package exists.
//
// A generator emitting both ent code and protobuf code has to spell the same
// field two ways, and they differ exactly where an initialism is involved.
func TestEntAndProtocDisagree(t *testing.T) {
	// ent says DeviceID; protoc-gen-go says DeviceId.
	require.Equal(t, "DeviceID", entname.Pascal("device_id"))
	require.NotEqual(t, "DeviceId", entname.Pascal("device_id"))

	// And they agree where no initialism is involved, which is what makes the
	// disagreement easy to miss.
	require.Equal(t, "TokenHash", entname.Pascal("token_hash"))
}

// TestAcronymsMatchEnt reads ent's own list out of the module cache and
// compares it.
//
// Without this, an ent release that adds an acronym silently renames a column
// setter -- SetUtcOffset becomes SetUTCOffset -- and the generated code stops
// compiling with an error about a field nobody removed. With it, the failure
// says "ent's acronym list changed", which is the actual problem.
func TestAcronymsMatchEnt(t *testing.T) {
	path := entFuncsPath(t)
	if path == "" {
		t.Skip("ent is not in the module cache")
	}

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	require.NoError(t, err)

	var got []string
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "ruleset" {
			return true
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			arr, ok := lit.Type.(*ast.ArrayType)
			if !ok {
				return true
			}
			if ident, ok := arr.Elt.(*ast.Ident); !ok || ident.Name != "string" {
				return true
			}
			for _, elt := range lit.Elts {
				bl, ok := elt.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(bl.Value)
				require.NoError(t, err)
				got = append(got, s)
			}
			return false
		})
		return false
	})

	require.NotEmpty(t, got, "could not find ent's acronym list in %s", path)
	require.Equal(t, got, entname.Acronyms,
		"ent's acronym list changed; copy it into entname.Acronyms — "+
			"until then every field whose name contains the new initialism is spelled wrong")
}

// entFuncsPath locates ent's entc/gen/func.go for the version go.mod pins.
func entFuncsPath(t *testing.T) string {
	t.Helper()

	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return ""
	}
	cache := strings.TrimSpace(string(out))

	matches, err := filepath.Glob(filepath.Join(cache, "entgo.io", "ent@*", "entc", "gen", "func.go"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	// Any pinned copy will do: the list is what is being compared, and a
	// mismatch between two cached ent versions is itself worth knowing.
	return matches[len(matches)-1]
}
