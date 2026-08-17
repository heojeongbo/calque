package plugin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/plugin"
)

func TestParseParams(t *testing.T) {
	p, err := plugin.ParseParams("config=calque.yaml,manifest=calque.manifest,dexie.unique_compound=runtime")
	require.NoError(t, err)

	require.Equal(t, "calque.yaml", p.Config)
	require.Equal(t, "calque.manifest", p.Manifest)
	require.Equal(t, map[string]string{"dexie.unique_compound": "runtime"}, p.Overrides)
}

func TestParseParamsEmpty(t *testing.T) {
	p, err := plugin.ParseParams("")
	require.NoError(t, err)
	require.Empty(t, p.Config)
	require.Empty(t, p.Overrides)
}

// TestUnknownOptIsRefused is the inverse of the predecessor's failure. It wired
// a flag.FlagSet as ParamFunc and registered no flags, so *any* opt= was a hard
// error and its own buf.gen.yaml therefore passed none. Same root cause, other
// direction: nobody decided what opt means.
func TestUnknownOptIsRefused(t *testing.T) {
	_, err := plugin.ParseParams("target=ts")
	require.ErrorContains(t, err, `unknown opt "target"`)
	require.ErrorContains(t, err, "calque understands config=")

	_, err = plugin.ParseParams("justaword")
	require.ErrorContains(t, err, "is not key=value")
}

func TestConfigGivenTwiceIsRefused(t *testing.T) {
	_, err := plugin.ParseParams("config=a.yaml,config=b.yaml")
	require.ErrorContains(t, err, "more than once")
}

// TestDottedKeysAreOverrides: anything with a dot is a section override, which
// is how a backend option is set without editing the file.
func TestDottedKeysAreOverrides(t *testing.T) {
	p, err := plugin.ParseParams("ts.import_extension=.js,dexie.database_version=2")
	require.NoError(t, err)
	require.Equal(t, []string{"dexie.database_version", "ts.import_extension"}, p.OverrideKeys())
}

// TestMissingConfigNamesTheAbsolutePath: "no such file: calque.yaml" is useless
// when the plugin's working directory is not the one the user is thinking
// about, which for buf it usually is not.
func TestMissingConfigNamesTheAbsolutePath(t *testing.T) {
	p, err := plugin.ParseParams("config=nope/calque.yaml")
	require.NoError(t, err)

	_, err = p.ConfigPath()
	require.Error(t, err)
	require.Contains(t, err.Error(), "nope/calque.yaml")
	require.Contains(t, err.Error(), string(filepath.Separator),
		"the message has to carry the resolved absolute path")
}

func TestConfigPathFoundBesideWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "calque.yaml"), []byte("version: 1\n"), 0o644))

	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(wd) })
	require.NoError(t, os.Chdir(dir))

	p, err := plugin.ParseParams("")
	require.NoError(t, err)

	got, err := p.ConfigPath()
	require.NoError(t, err)
	require.Equal(t, "calque.yaml", filepath.Base(got))
}

func TestNoConfigAnywhereSaysWhatToDo(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(wd) })
	require.NoError(t, os.Chdir(dir))

	p, err := plugin.ParseParams("")
	require.NoError(t, err)

	_, err = p.ConfigPath()
	require.ErrorContains(t, err, "no calque.yaml in")
	require.ErrorContains(t, err, "opt: [config=")
	require.ErrorContains(t, err, "where buf runs")
}

// TestQuietIsBooleanish accepts the spellings a build file is likely to use.
func TestQuietIsBooleanish(t *testing.T) {
	for _, s := range []string{"quiet=true", "quiet=1", "quiet="} {
		p, err := plugin.ParseParams(s)
		require.NoError(t, err, s)
		require.True(t, p.Quiet, s)
	}
	for _, s := range []string{"quiet=false", "quiet=0"} {
		p, err := plugin.ParseParams(s)
		require.NoError(t, err, s)
		require.False(t, p.Quiet, s)
	}
	_, err := plugin.ParseParams("quiet=yes")
	require.ErrorContains(t, err, "is not a boolean")
}
