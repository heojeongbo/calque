package gotarget_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/backend/entsql"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	gotarget "github.com/heojeongbo/calque/target/gotarget"
)

// param is what the plugin is given in a real build, and the Go target reads it
// twice over: once for its own options and once through protogen, which is
// where module= and default_api_level= are parsed.
//
// API_OPAQUE is not a preference. The emitted code says SetX, which the open
// API does not generate, so the alternative is code that names a method nobody
// wrote.
const param = "module=example.com,default_api_level=API_OPAQUE"

func generate(t *testing.T, cfgDoc string, files ...string) *gen.Output {
	t.Helper()
	if len(files) == 0 {
		files = []string{"apptest.proto", "apptest_svc.proto"}
	}

	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../../testdata/proto/valid", "../../testdata/proto/_upstream"}, param,
		files...)
	require.NoError(t, err)

	s, reg, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)

	cfg, err := gen.ParseConfig([]byte(cfgDoc), "calque.yaml")
	require.NoError(t, err)

	r := gen.NewRegistry().Target(gotarget.New()).Backend(entsql.New())
	out, err := gen.Run(s, cfg, r, gen.WithDescriptors(req, reg))
	require.NoError(t, err)
	return out
}

const defaultConfig = "version: 1\ntargets:\n  - {target: go, backend: entsql}\n"

// TestGolden pins the emitted bytes.
//
// The Go target had no test that ran without an environment variable pointing
// at a private tree: the reference test skips by default, so `go test ./...`
// proved nothing about four thousand lines of emission. This is what a
// checkout can run.
//
// UPDATE_GOLDEN=1 rewrites them; read the diff before committing it, because
// the value of a golden is that it notices what you did not mean.
func TestGolden(t *testing.T) {
	out := generate(t, defaultConfig)
	dir := "testdata/golden"

	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.RemoveAll(dir))
		for _, name := range out.Names() {
			body, _ := out.Body(name)
			path := filepath.Join(dir, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, body, 0o644))
		}
		t.Log("goldens rewritten")
		return
	}

	for _, name := range out.Names() {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err, "no golden for %s; run with UPDATE_GOLDEN=1", name)

			body, _ := out.Body(name)
			require.Equal(t, string(want), string(body))
		})
	}

	var golden []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		golden = append(golden, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err)
	require.ElementsMatch(t, golden, out.Names(), "the set of emitted files changed")
}

// TestTwoNamingSystemsInOneLine is the property the whole target turns on, and
// the one a casing function would quietly get wrong.
//
// ent and protoc-gen-go disagree about a field with an initialism in it, so a
// single statement spells the same prop both ways. There is no schema in the
// fixtures with a multi-word key, so this asserts the pair that does occur: the
// key is ent's ID and protoc-gen-go's Id.
func TestTwoNamingSystemsInOneLine(t *testing.T) {
	out := generate(t, defaultConfig)

	body, ok := out.Body("apptest/oas/ent/apptest.g.go")
	require.True(t, ok, "names: %v", out.Names())
	require.Contains(t, string(body), "x.SetId(e.ID[:])",
		"ent calls the key ID and protoc-gen-go calls it Id; both appear here")
}

// TestOpaqueAPIIsRequired: the open API has no SetX, so emitting for it would
// produce code that fails to compile against a method that was never
// generated. Saying so names the flag instead of the symptom.
func TestOpaqueAPIIsRequired(t *testing.T) {
	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../../testdata/proto/valid", "../../testdata/proto/_upstream"},
		"module=example.com", // no default_api_level
		"apptest.proto", "apptest_svc.proto")
	require.NoError(t, err)

	s, reg, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)
	cfg, err := gen.ParseConfig([]byte(defaultConfig), "calque.yaml")
	require.NoError(t, err)

	r := gen.NewRegistry().Target(gotarget.New()).Backend(entsql.New())
	_, err = gen.Run(s, cfg, r, gen.WithDescriptors(req, reg))
	require.Error(t, err)
	require.ErrorContains(t, err, "default_api_level=API_OPAQUE")
}

// TestServersNeedAService: an entity nothing serves still gets a schema and a
// conversion, because an edge pointing at it has to mean something, but it has
// no methods to implement and therefore no server.
func TestServersNeedAService(t *testing.T) {
	out := generate(t, defaultConfig, "apptest.proto") // no _svc

	var bare []string
	for _, name := range out.Names() {
		if strings.Contains(name, "/server/bare/") {
			bare = append(bare, name)
		}
	}
	require.Empty(t, bare, "no service, no server")

	_, ok := out.Body("apptest/oas/schema/apptest.go")
	require.True(t, ok, "the schema is emitted regardless")
}

// TestDirectoriesAreConfigurable: where the three groups land is a setting, and
// a drop-in needs it to be, because the tree being replaced chose its own.
func TestDirectoriesAreConfigurable(t *testing.T) {
	out := generate(t, `
version: 1
targets:
  - {target: go, backend: entsql}
go:
  schema_dir: entschema
  ent_dir: db
  bare_dir: rpc
`)
	for _, want := range []string{
		"apptest/oas/entschema/apptest.go",
		"apptest/oas/db/apptest.g.go",
		"apptest/oas/rpc/apptest.g.go",
	} {
		_, ok := out.Body(want)
		require.True(t, ok, "expected %s in %v", want, out.Names())
	}
}

// TestQueryHeaderIsSeparate: the two file groups came from two different
// upstream generators, so a drop-in needs two headers. Everything else follows
// `header`.
func TestQueryHeaderIsSeparate(t *testing.T) {
	out := generate(t, `
version: 1
targets:
  - {target: go, backend: entsql}
go:
  header: "// schema header"
  query_header: "// query header"
`)
	for name, want := range map[string]string{
		"apptest/oas/schema/apptest.go":        "// schema header",
		"apptest/oas/ent/apptest.g.go":         "// schema header",
		"apptest/oas/server/bare/apptest.g.go": "// schema header",
		"apptest/oas/query.g.go":               "// query header",
		"apptest/oas/store.g.go":               "// query header",
	} {
		body, ok := out.Body(name)
		require.True(t, ok, "expected %s in %v", name, out.Names())
		require.True(t, strings.HasPrefix(string(body), want+"\n"),
			"%s should start with %q, got %q", name, want, firstLine(string(body)))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
