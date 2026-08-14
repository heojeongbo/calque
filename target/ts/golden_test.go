package ts_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/HeoJeongBo/calque/backend/dexie"
	"github.com/HeoJeongBo/calque/gen"
	"github.com/HeoJeongBo/calque/internal/protoc"
	"github.com/HeoJeongBo/calque/ormcompat"
	"github.com/HeoJeongBo/calque/target/ts"
)

// generate runs the target over the fixture the way the plugin does.
func generate(t *testing.T, cfgDoc string) *gen.Output {
	t.Helper()

	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../../testdata/proto/valid", "../../testdata/proto/_upstream"}, "",
		"apptest.proto", "apptest_svc.proto")
	require.NoError(t, err)

	s, files, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)

	cfg, err := gen.ParseConfig([]byte(cfgDoc), "calque.yaml")
	require.NoError(t, err)

	r := gen.NewRegistry().Target(ts.New()).Backend(dexie.New())
	out, err := gen.Run(s, cfg, r, gen.WithDescriptors(req, files))
	require.NoError(t, err)
	return out
}

const defaultConfig = "version: 1\ntargets:\n  - {target: ts, backend: dexie}\n"

// TestGolden pins the emitted bytes.
//
// Byte fidelity is the whole point of this target's first version: the diff
// against the generator being replaced is the review, so a change in whitespace
// is a change in behaviour as far as this test is concerned.
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

// TestRuntimeSpecifierIsConfigurable: the import the generated code carries is
// a setting, not a constant. It is the only line that is meant to differ from
// the predecessor's output.
func TestRuntimeSpecifierIsConfigurable(t *testing.T) {
	out := generate(t, `
version: 1
targets:
  - {target: ts, backend: dexie}
ts:
  runtime: "@protobuf-orm/runtime"
`)
	body, ok := out.Body("apptest/user.db.g.ts")
	require.True(t, ok)
	require.Contains(t, string(body), `from "@protobuf-orm/runtime";`)
	require.NotContains(t, string(body), "@heojeongbo/calque-dexie")
}

// TestTablesNeedAService: an entity with no service is modelled and not stored,
// which is how the predecessor decides and therefore how the file set is
// decided.
func TestTablesNeedAService(t *testing.T) {
	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../../testdata/proto/valid", "../../testdata/proto/_upstream"}, "",
		"apptest.proto") // no _svc
	require.NoError(t, err)

	s, files, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)
	cfg, err := gen.ParseConfig([]byte(defaultConfig), "calque.yaml")
	require.NoError(t, err)

	r := gen.NewRegistry().Target(ts.New()).Backend(dexie.New())
	out, err := gen.Run(s, cfg, r, gen.WithDescriptors(req, files))
	require.NoError(t, err)

	require.Equal(t, []string{"apptest/client.g.ts"}, out.Names(),
		"client.g.ts is emitted even when empty; db.g.ts and the tables are not")
}
