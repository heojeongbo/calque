package ts_test

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/backend/dexie"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	"github.com/heojeongbo/calque/target/ts"
)

// TestRefOneofNameIsEmitted pins that the emitted code names the oneof the schema
// actually declares.
//
// The generated table dereferences that oneof -- `req.ref?.key` -- so the name is
// load-bearing in the output, not free. It used to be the literal `key` in a dozen
// places, which is right for every schema that calls it that and silently wrong
// for one that does not: protobuf-es would have generated a `lookup` property and
// the table would read `.key`, undefined at run time. The Go target has always got
// this right, because protogen hands it `WhichLookup()` and there is nothing to
// hardcode.
//
// The fixture is valid/apptest_svc.proto with `oneof key` spelled `oneof lookup`
// and nothing else touched, so what follows the rename is the whole difference.
func TestRefOneofNameIsEmitted(t *testing.T) {
	want := emitWith(t, "apptest_svc.proto", "../../testdata/proto/valid")
	got := emitWith(t, "apptest_svc.proto", "../../testdata/proto/refoneof", "../../testdata/proto/valid")

	require.Equal(t, keysOf(want), keysOf(got), "the same files must be emitted")

	table := string(got["apptest/user.db.g.ts"])
	require.Contains(t, table, "req.ref?.lookup === undefined")
	require.Contains(t, table, "const { lookup: key } = req.ref")
	require.NotContains(t, table, "req.ref?.key")

	// The client builds Refs rather than reading them, and it names the oneof too.
	client := string(got["apptest/client.g.ts"])
	require.Contains(t, client, "{lookup:{case:")
	require.NotContains(t, client, "{key:{case:")

	// And nothing moved that does not name the oneof. That is what makes this a
	// rename rather than a change in behaviour.
	for _, name := range keysOf(want) {
		wl := strings.Split(string(want[name]), "\n")
		gl := strings.Split(string(got[name]), "\n")
		require.Equal(t, len(wl), len(gl), "%s: line count", name)
		for i := range wl {
			if wl[i] == gl[i] {
				continue
			}
			require.Contains(t, gl[i], "lookup", "%s line %d changed without naming the oneof", name, i+1)
		}
	}
}

// TestRefWithTwoOneofsWarns: which oneof holds the lookups is not guessed.
//
// This target used to ask for one named `key` and so would have found this one,
// silently, on a message where nothing says it is the right one. It now says what
// it cannot tell -- and carries on, because a case label it cannot read is one it
// can compute, and refusing would drop an entity's whole table over a label that
// is very likely right.
func TestRefWithTwoOneofsWarns(t *testing.T) {
	var warnings bytes.Buffer

	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../../testdata/proto/refoneof", "../../testdata/proto/valid", "../../testdata/proto/_upstream"},
		"", "apptest.proto", "two_svc.proto")
	require.NoError(t, err)

	s, files, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)

	cfg, err := gen.ParseConfig([]byte(defaultConfig), "calque.yaml")
	require.NoError(t, err)

	r := gen.NewRegistry().Target(ts.New()).Backend(dexie.New())
	_, err = gen.Run(s, cfg, r, gen.WithDescriptors(req, files), gen.WithWarnings(&warnings))

	require.NoError(t, err)
	require.Contains(t, warnings.String(), "UserRef has 2 oneofs")
	require.Contains(t, warnings.String(), "needs exactly one oneof; its name does not matter")
}

// emitWith generates from apptest with the given import paths, first one first.
//
// The refoneof tree carries only service files, so putting it ahead of valid swaps
// that one file and leaves the entity protos shared.
func emitWith(t *testing.T, svc string, roots ...string) map[string][]byte {
	t.Helper()

	paths := append(append([]string{}, roots...), "../../testdata/proto/_upstream")
	req, err := protoc.CompileRequest(t.Context(), paths, "", "apptest.proto", svc)
	require.NoError(t, err)

	s, files, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)

	cfg, err := gen.ParseConfig([]byte(defaultConfig), "calque.yaml")
	require.NoError(t, err)

	r := gen.NewRegistry().Target(ts.New()).Backend(dexie.New())
	out, err := gen.Run(s, cfg, r, gen.WithDescriptors(req, files))
	require.NoError(t, err)

	got := map[string][]byte{}
	for _, name := range out.Names() {
		body, _ := out.Body(name)
		got[name] = body
	}
	return got
}

// keysOf is the file names, sorted -- map order is not the thing under test.
func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
