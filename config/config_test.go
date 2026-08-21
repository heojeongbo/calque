package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/config"
)

func parse(t *testing.T, doc string) (*config.Config, error) {
	t.Helper()
	return config.Parse([]byte(doc), "calque.yaml")
}

func mustParse(t *testing.T, doc string) *config.Config {
	t.Helper()
	c, err := parse(t, doc)
	require.NoError(t, err)
	return c
}

const minimal = `
version: 1
targets:
  - target: ts
    backend: dexie
`

func TestParseMinimal(t *testing.T) {
	c := mustParse(t, minimal)

	require.Equal(t, config.FormatVersion, c.Version)
	require.Len(t, c.Targets, 1)
	require.Equal(t, "ts", c.Targets[0].Target)
	require.Equal(t, "dexie", c.Targets[0].Backend)
	require.Equal(t, "ts", c.Targets[0].Label(), "Label defaults to the target name")
	require.Empty(t, c.UnclaimedSections())
}

// TestVersionIsRequired: a version change should say so, not produce a cascade
// of unknown-key errors about keys that were valid in the version the user
// actually wrote.
func TestVersionIsRequired(t *testing.T) {
	_, err := parse(t, "targets:\n  - {target: ts, backend: dexie}\n")
	require.ErrorContains(t, err, "`version` is required")
	require.ErrorContains(t, err, "version 1")
}

func TestFutureVersionIsRefusedClearly(t *testing.T) {
	_, err := parse(t, "version: 2\ntargets:\n  - {target: ts, backend: dexie}\n")
	require.ErrorContains(t, err, "config version 2")
	require.ErrorContains(t, err, "understands version 1")
}

func TestTargetsAreRequired(t *testing.T) {
	_, err := parse(t, "version: 1\n")
	require.ErrorContains(t, err, "`targets` is empty")

	_, err = parse(t, "version: 1\ntargets:\n  - backend: dexie\n")
	require.ErrorContains(t, err, "`target` is required")

	// A missing `backend` is checked in Run, not here: only Run knows whether
	// the target is storeless and therefore takes none. See run_test.go.
	_, err = parse(t, "version: 1\ntargets:\n  - target: ts\n")
	require.NoError(t, err)

	// The spelling is still checked here, because that needs no registry.
	_, err = parse(t, "version: 1\ntargets:\n  - {target: ts, backend: \"Not A Name\"}\n")
	require.ErrorContains(t, err, "backend:")
}

// TestOutStaysInsideTheOutputRoot: buf owns the root and hands it to the
// plugin. A generator writing outside it is not a path to clean up.
func TestOutStaysInsideTheOutputRoot(t *testing.T) {
	for _, tc := range []struct{ out, want string }{
		{"/etc", "absolute"},
		{"../elsewhere", "escapes the output root"},
		{"ts/../../up", "escapes the output root"},
	} {
		t.Run(tc.out, func(t *testing.T) {
			_, err := parse(t, "version: 1\ntargets:\n  - {target: ts, backend: dexie, out: "+tc.out+"}\n")
			require.ErrorContains(t, err, tc.want)
		})
	}

	// A plain subdirectory, and the root itself, are both fine.
	for _, out := range []string{"", "ts", "gen/ts"} {
		_, err := parse(t, "version: 1\ntargets:\n  - {target: ts, backend: dexie, out: \""+out+"\"}\n")
		require.NoError(t, err, "out: %q", out)
	}
}

// TestDuplicateTargetNamesAreRefused: two entries claiming one name would make
// per-instance option sections ambiguous, and would make "which one emitted
// this file" unanswerable.
func TestDuplicateTargetNamesAreRefused(t *testing.T) {
	_, err := parse(t, `
version: 1
targets:
  - {target: ts, backend: dexie}
  - {target: ts, backend: memory}
`)
	require.ErrorContains(t, err, `both named "ts"`)
	require.ErrorContains(t, err, "distinct `name`")

	// Naming one of them resolves it.
	c := mustParse(t, `
version: 1
targets:
  - {target: ts, backend: dexie}
  - {target: ts, backend: memory, name: ts-memory}
`)
	require.Equal(t, "ts", c.Targets[0].Label())
	require.Equal(t, "ts-memory", c.Targets[1].Label())
}

func TestCoreKeysAreStrict(t *testing.T) {
	_, err := parse(t, "version: 1\ntargets:\n  - {target: ts, backend: dexie, nope: 1}\n")
	require.Error(t, err, "an unknown key inside a core section must not be ignored")
	require.ErrorContains(t, err, "targets")
}

func TestDuplicateKeysAreRefused(t *testing.T) {
	_, err := parse(t, "version: 1\nversion: 1\ntargets:\n  - {target: ts, backend: dexie}\n")
	require.Error(t, err, "a duplicate key is a parse error, not a last-one-wins")
}

// TestSectionClaimsAndDecodesStrictly is the extension mechanism: a target or

// the code did not check it, so `target: TS` was a violation an editor flagged
// and a build did not.
func TestNamesAreChecked(t *testing.T) {
	_, err := parse(t, "version: 1\ntargets:\n  - {target: TS, backend: dexie}\n")
	require.ErrorContains(t, err, "not a name")

	_, err = parse(t, "version: 1\ntargets:\n  - {target: ts, backend: Dexie}\n")
	require.ErrorContains(t, err, "not a name")

	_, err = parse(t, "version: 1\ntargets:\n  - {target: ts, backend: dexie, name: TS2}\n")
	require.ErrorContains(t, err, "not a name")
}

func TestEmptyAndMalformedDocuments(t *testing.T) {
	_, err := parse(t, "")
	require.ErrorContains(t, err, "config is empty")

	_, err = parse(t, "- a\n- b\n")
	require.ErrorContains(t, err, "must be a mapping")
}
