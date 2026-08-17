package gotarget_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/backend/entsql"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	gotarget "github.com/heojeongbo/calque/target/gotarget"
)

// TestRefOneofNameIsEmitted pins that the emitted code names the oneof the schema
// actually declares.
//
// protoc-gen-go names the accessor after the oneof, so `oneof lookup` produces
// WhichLookup() and there is nothing to hardcode -- this target reads the name
// through protogen and always has. The assertion is here anyway, because it is the
// half of the pair that was already right: the TypeScript target used to write the
// literal `key` and would have disagreed with this file.
//
// See testdata/proto/refoneof/README.md.
func TestRefOneofNameIsEmitted(t *testing.T) {
	want := goEmit(t, "apptest_svc.proto", "../../testdata/proto/valid")
	got := goEmit(t, "apptest_svc.proto", "../../testdata/proto/refoneof", "../../testdata/proto/valid")

	require.Equal(t, want.Names(), got.Names(), "the same files must be emitted")

	query, ok := got.Body("apptest/oas/query.g.go")
	require.True(t, ok)
	require.Contains(t, string(query), "x.WhichLookup()")
	require.NotContains(t, string(query), "x.WhichKey()")

	// And nothing moved that does not name the oneof.
	for _, name := range want.Names() {
		w, _ := want.Body(name)
		g, _ := got.Body(name)
		wl := strings.Split(string(w), "\n")
		gl := strings.Split(string(g), "\n")
		require.Equal(t, len(wl), len(gl), "%s: line count", name)
		for i := range wl {
			if wl[i] == gl[i] {
				continue
			}
			require.Contains(t, gl[i], "Lookup", "%s line %d changed without naming the oneof", name, i+1)
		}
	}
}

// TestRefWithTwoOneofsIsRefused: which oneof holds the lookups is not guessed.
//
// Taking Oneofs[0] is what this did, and it would have produced a file that
// compiles from a message where nothing says the first oneof is the right one.
// Unlike the TypeScript target, which can compute a case label and warn, there is
// nothing to fall back to here -- every lookup is read off this oneof -- so it is
// an error naming the count.
func TestRefWithTwoOneofsIsRefused(t *testing.T) {
	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../../testdata/proto/refoneof", "../../testdata/proto/valid", "../../testdata/proto/_upstream"},
		param, "apptest.proto", "two_svc.proto")
	require.NoError(t, err)

	s, reg, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)

	cfg, err := gen.ParseConfig([]byte(defaultConfig), "calque.yaml")
	require.NoError(t, err)

	r := gen.NewRegistry().Target(gotarget.New()).Backend(entsql.New())
	_, err = gen.Run(s, cfg, r, gen.WithDescriptors(req, reg))

	require.ErrorContains(t, err, "UserRef has 2 oneofs")
	require.ErrorContains(t, err, "it needs exactly one (its name does not matter)")
}

// goEmit is generate() with the import paths under the caller's control, so a
// fixture tree can be layered ahead of valid/.
func goEmit(t *testing.T, svc string, roots ...string) *gen.Output {
	t.Helper()

	paths := append(append([]string{}, roots...), "../../testdata/proto/_upstream")
	req, err := protoc.CompileRequest(t.Context(), paths, param, "apptest.proto", svc)
	require.NoError(t, err)

	s, reg, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)

	cfg, err := gen.ParseConfig([]byte(defaultConfig), "calque.yaml")
	require.NoError(t, err)

	r := gen.NewRegistry().Target(gotarget.New()).Backend(entsql.New())
	out, err := gen.Run(s, cfg, r, gen.WithDescriptors(req, reg))
	require.NoError(t, err)
	return out
}
