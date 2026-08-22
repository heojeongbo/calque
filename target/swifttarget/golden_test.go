package swifttarget_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/backend/grdb"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	"github.com/heojeongbo/calque/target/swifttarget"
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

	r := gen.NewRegistry().Target(swifttarget.New()).Backend(grdb.New())
	out, err := gen.Run(s, cfg, r, gen.WithDescriptors(req, files))
	require.NoError(t, err)
	return out
}

const defaultConfig = "version: 1\ntargets:\n  - {target: swift, backend: grdb}\n"

// TestGolden pins the emitted bytes.
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
}

// TestEdgeIsTargetKey checks that an edge types as the key it stores rather
// than as the entity it points at.
//
// A `Tenant tenant = 2 [(orm.edge) = {}]` is a `tenant_id` column holding a
// uuid, so the Swift property is a UUID -- not a Tenant. Emitting the entity
// type would compile and then fail to decode every row.
func TestEdgeIsTargetKey(t *testing.T) {
	out := generate(t, defaultConfig)
	body, ok := out.Body("User.g.swift")
	require.True(t, ok, "no User.g.swift")

	src := string(body)
	require.Contains(t, src, "var tenant: UUID",
		"an edge should type as its target's key")
	require.Contains(t, src, `case tenant = "tenant_id"`,
		"an edge's column is the edge name with _id")
}

// TestImmutableIsLet checks that a prop the store will not change after insert
// is a `let`.
//
// `date_created` is immutable in the fixture. A `var` would offer a write that
// the patch surface refuses, which is a compile-time promise worth keeping.
func TestImmutableIsLet(t *testing.T) {
	out := generate(t, defaultConfig)
	body, _ := out.Body("User.g.swift")
	require.Contains(t, string(body), "let dateCreated: Date")
	require.Contains(t, string(body), "var name: String")
}

// TestNullableIsOptional checks that only what may hold null is optional.
//
// The fixture's `lock` is explicitly present and nullable; `alias` is neither.
func TestNullableIsOptional(t *testing.T) {
	out := generate(t, defaultConfig)
	src := string(mustBody(t, out, "User.g.swift"))
	require.Contains(t, src, "var lock: String?")
	require.Contains(t, src, "var alias: String")
	require.NotContains(t, src, "var alias: String?")
}

// TestCodecsAreNamedPerColumn checks that a value needing a transform says so.
//
// SQLite has no uuid and no time, so both go through a codec. Identity is left
// out on purpose: naming the transform that is no transform would be noise in
// every entity.
func TestCodecsAreNamedPerColumn(t *testing.T) {
	out := generate(t, defaultConfig)
	src := string(mustBody(t, out, "User.g.swift"))
	require.Contains(t, src, `"id": .uuidString,`)
	require.Contains(t, src, `"date_created": .timeEpochMillis,`)
	require.NotContains(t, src, ".identity")
}

// TestTableNameIsSnakeCase checks the table an entity lives in.
func TestTableNameIsSnakeCase(t *testing.T) {
	out := generate(t, defaultConfig)
	src := string(mustBody(t, out, "User.g.swift"))
	require.Contains(t, src, `static let databaseTableName = "user"`)
}

// TestAccessLevelIsConfigurable checks that a generated declaration can be
// public when the code ships as its own module.
func TestAccessLevelIsConfigurable(t *testing.T) {
	out := generate(t, "version: 1\ntargets:\n  - {target: swift, backend: grdb}\nswift:\n  access_level: public\n")
	src := string(mustBody(t, out, "User.g.swift"))
	require.Contains(t, src, "public struct User")
	require.NotContains(t, src, "internal struct User")
}

// TestRejectsUnknownAccessLevel checks that a typo stops the run rather than
// silently emitting internal.
func TestRejectsUnknownAccessLevel(t *testing.T) {
	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../../testdata/proto/valid", "../../testdata/proto/_upstream"}, "",
		"apptest.proto", "apptest_svc.proto")
	require.NoError(t, err)
	s, files, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)

	cfg, err := gen.ParseConfig(
		[]byte("version: 1\ntargets:\n  - {target: swift, backend: grdb}\nswift:\n  access_level: fileprivate\n"),
		"calque.yaml")
	require.NoError(t, err)

	r := gen.NewRegistry().Target(swifttarget.New()).Backend(grdb.New())
	_, err = gen.Run(s, cfg, r, gen.WithDescriptors(req, files))
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_level")
}

// TestOneFilePerEntity checks the file layout: one entity, one file, named for
// the entity rather than for the proto file it came from.
func TestOneFilePerEntity(t *testing.T) {
	out := generate(t, defaultConfig)
	for _, name := range out.Names() {
		require.True(t, strings.HasSuffix(name, ".g.swift"), "unexpected file %s", name)
	}
	_, ok := out.Body("Tenant.g.swift")
	require.True(t, ok, "no Tenant.g.swift")
}

func mustBody(t *testing.T, out *gen.Output, name string) []byte {
	t.Helper()
	body, ok := out.Body(name)
	require.True(t, ok, "no %s", name)
	return body
}
