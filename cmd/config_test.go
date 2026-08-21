package cmd_test

import (
	"testing"

	"github.com/lesomnus/xli/xlitest"
	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/cmd"
)

func config(t *testing.T, args ...string) xlitest.Result {
	t.Helper()
	return xlitest.Run(t, cmd.NewCmdRoot(cmd.Registry()), append([]string{"config"}, args...)...)
}

// TestConfigShowsTheEffectiveValues is the point of the command.
//
// The file says nothing about `ts`, and the output still reports the runtime
// and the header the target will use, because what is printed is the value the
// target decoded into after applying its own defaults -- not the file's half of
// it. A person asking what a build will do gets the answer rather than the
// input.
func TestConfigShowsTheEffectiveValues(t *testing.T) {
	got := config(t, "-c", "testdata/calque.yaml")

	require.NoError(t, got.Err)
	require.Contains(t, got.Stdout, "ts → dexie")
	require.Contains(t, got.Stdout, `runtime: "@heojeongbo/calque-dexie"`)
	require.Contains(t, got.Stdout, "compat: orm-ts")
	require.Contains(t, got.Stdout, "defaults only",
		"the file names no section, and saying where a value came from is half the answer")
}

func TestConfigAppliesOverrides(t *testing.T) {
	got := config(t, "-c", "testdata/calque.yaml", "-o", "dexie.compat=none", "-o", "ts.import_extension=.js")

	require.NoError(t, got.Err)
	require.Contains(t, got.Stdout, "compat: none")
	require.Contains(t, got.Stdout, "import_extension: .js")
	require.Contains(t, got.Stdout, "--opt only")
}

// TestConfigFailsTheWayABuildWould: this is gen.Resolve, so every error a build
// hits before it touches a proto is reachable here -- and reported as an error,
// because a person asked. The plugin puts the same message in response.error
// instead, since there buf is the one who has to print it.
func TestConfigFailsTheWayABuildWould(t *testing.T) {
	got := config(t, "-c", "testdata/unknown-target.yaml")

	require.ErrorContains(t, got.Err, `no target named "nope"`)
	require.ErrorContains(t, got.Err, "this build has go, service, ts")
}

func TestConfigRefusesASectionNobodyClaims(t *testing.T) {
	got := config(t, "-c", "testdata/calque.yaml", "-o", "dexei.compat=none")

	require.ErrorContains(t, got.Err, `nothing understands "dexei"`)
	require.ErrorContains(t, got.Err, "this build knows:")
}

func TestConfigSaysWhereItLooked(t *testing.T) {
	got := config(t, "-c", "testdata/nope.yaml")

	require.ErrorContains(t, got.Err, "no config at /")
	require.ErrorContains(t, got.Err, "testdata/nope.yaml")
}
