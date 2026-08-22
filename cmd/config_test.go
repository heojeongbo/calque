package cmd_test

import (
	"strings"
	"testing"

	"github.com/lesomnus/xli/xlitest"
	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/cmd"
)

func config(t *testing.T, args ...string) xlitest.Result {
	t.Helper()
	return configIn(t, nil, args...)
}

// configIn runs `calque config` in an environment the test states. It is a
// parameter all the way down from NewCmdRoot, so these assertions hold whatever
// the machine running them happens to export.
func configIn(t *testing.T, environ []string, args ...string) xlitest.Result {
	t.Helper()
	return xlitest.Run(t, cmd.NewCmdRoot(cmd.Registry(), environ), append([]string{"config"}, args...)...)
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
	require.Contains(t, got.Stdout, "(--opt)")
}

// TestConfigFailsTheWayABuildWould: this is gen.Resolve, so every error a build
// hits before it touches a proto is reachable here -- and reported as an error,
// because a person asked. The plugin puts the same message in response.error
// instead, since there buf is the one who has to print it.
func TestConfigFailsTheWayABuildWould(t *testing.T) {
	got := config(t, "-c", "testdata/unknown-target.yaml")

	require.ErrorContains(t, got.Err, `no target named "nope"`)
	// Read the list off the registry rather than spelling it: a hardcoded list
	// makes registering a target a failing test in a package that has nothing to
	// do with it, which is what happened when swift was added.
	require.ErrorContains(t, got.Err, "this build has "+strings.Join(cmd.Registry().TargetNames(), ", "))
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

// TestTheSectionsOfThisBuildAreRead is the one thing these tests say about
// calque rather than about the reading: the walker is wired to the options the
// real targets and backends declare, and to nothing they keep to themselves.
//
// config/env_test.go covers every shape a section can have, against a struct of
// its own that does not change when a target does. This is the wire.
func TestTheSectionsOfThisBuildAreRead(t *testing.T) {
	got := configIn(t, []string{
		"CALQUE_TS_IMPORT_EXTENSION=.js",
		"CALQUE_DEXIE_COMPAT=none",
	}, "-c", "testdata/calque.yaml")
	require.NoError(t, got.Err)

	require.Contains(t, got.Stdout, "import_extension: .js")
	require.Contains(t, got.Stdout, "compat: none")
	require.Contains(t, got.Stdout, "CALQUE_TS_IMPORT_EXTENSION",
		"the command says which variable reached which section")
}

// TestANameNothingAnswersToIsShown: an environment variable is ambient and may
// belong to something else on the machine, so it is reported and not refused.
func TestANameNothingAnswersToIsShown(t *testing.T) {
	got := configIn(t, []string{"CALQUE_DEXEI_COMPAT=none"}, "-c", "testdata/calque.yaml")
	require.NoError(t, got.Err, "a stray variable does not stop a build")
	require.Contains(t, got.Stdout, "CALQUE_DEXEI_COMPAT")
	require.Contains(t, got.Stdout, "nothing answers to this")
}
