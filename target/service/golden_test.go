package service_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	"github.com/heojeongbo/calque/target/service"
)

const defaultConfig = "version: 1\ntargets:\n  - target: service\n"

// generate runs the target over the fixture the way the plugin does.
//
// Only the entity protos go in. The corpus also carries a hand-written
// apptest_svc.proto, which is what the *other* targets read; compiling it here
// would put two declarations of every request message in one package.
func generate(t *testing.T, cfgDoc string, files ...string) *gen.Output {
	t.Helper()
	if len(files) == 0 {
		files = []string{"apptest.proto", "naming.proto", "erased.proto"}
	}

	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../../testdata/proto/valid", "../../testdata/proto/_upstream"}, "",
		files...)
	require.NoError(t, err)

	s, reg, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)

	cfg, err := gen.ParseConfig([]byte(cfgDoc), "calque.yaml")
	require.NoError(t, err)

	out, err := gen.Run(s, cfg, gen.NewRegistry().Target(service.New()), gen.WithDescriptors(req, reg))
	require.NoError(t, err)
	return out
}

// TestGolden pins the emitted bytes.
//
// This output is a contract twice over: it is compiled by protoc-gen-go and
// protoc-gen-es into code an application calls, and it is merged with hand-written
// extensions that reference its message names. A change in whitespace is a change
// in behaviour as far as this test is concerned.
//
// UPDATE_GOLDEN=1 rewrites them; CI never sets it.
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

	// A golden with no emitted file is a file that stopped being produced.
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

// TestHeaderIsConfigurable: the first line is the one thing meant to differ from
// the generator being replaced, which is how the reference test proves the rest is
// byte-identical while a real build still says who wrote the file.
func TestHeaderIsConfigurable(t *testing.T) {
	out := generate(t, defaultConfig+"service:\n  header: \"// mine\"\n")

	body, ok := out.Body("apptest_svc.g.proto")
	require.True(t, ok)
	require.Equal(t, "// mine\n", string(body[:len("// mine\n")]))
}

// TestSuffixIsConfigurable: the output name follows the input's, and what is
// appended is a setting rather than a constant nobody can move.
func TestSuffixIsConfigurable(t *testing.T) {
	out := generate(t, defaultConfig+"service:\n  suffix: \"_service.proto\"\n")
	require.Contains(t, out.Names(), "apptest_service.proto")
}

// TestNoBackendIsAccepted: the target declares gen.Storeless, so a config entry
// without a `backend` is what a correct one looks like. Naming one is the error.
func TestNoBackendIsAccepted(t *testing.T) {
	require.NotEmpty(t, generate(t, defaultConfig).Names())
}
