package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSectionClaimsAndDecodesStrictly(t *testing.T) {
	c := mustParse(t, `
version: 1
targets:
  - {target: ts, backend: dexie}

ts:
  runtime: "@heojeongbo/calque-runtime"
  import_extension: ".js"
`)

	require.Equal(t, []string{"ts"}, c.UnclaimedSections(),
		"before anything claims it, the section is unclaimed")

	var opts struct {
		Runtime         string `yaml:"runtime"`
		ImportExtension string `yaml:"import_extension"`
	}
	ok, err := c.Section("ts", &opts)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "@heojeongbo/calque-runtime", opts.Runtime)
	require.Equal(t, ".js", opts.ImportExtension)

	require.Empty(t, c.UnclaimedSections(), "claiming it removes it")

	// A section nobody wrote is not an error; it is simply absent.
	ok, err = c.Section("dexie", &opts)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSectionTypoIsLoud(t *testing.T) {
	c := mustParse(t, `
version: 1
targets:
  - {target: ts, backend: dexie}

ts:
  runtimee: "typo"
`)
	var opts struct {
		Runtime string `yaml:"runtime"`
	}
	_, err := c.Section("ts", &opts)
	require.Error(t, err, "an unknown key inside a claimed section must not be ignored")
	require.ErrorContains(t, err, "ts")
}

// TestUnclaimedSectionNamesTheAlternatives: a misspelled section name is the
// one typo strict decoding inside a section cannot catch, so it is caught here
// -- and the message says what this build does understand.
func TestUnclaimedSectionNamesTheAlternatives(t *testing.T) {
	c := mustParse(t, `
version: 1
targets:
  - {target: ts, backend: dexie}

dexei:
  database_version: 1
`)
	err := c.CheckUnclaimed([]string{"ts", "dexie", "memory"})
	require.ErrorContains(t, err, `nothing understands "dexei"`)
	require.ErrorContains(t, err, "this build knows:")
	require.ErrorContains(t, err, "dexie")
	require.ErrorContains(t, err, "targets")
}

// TestOverrideWinsOverTheFile is the whole point of an override: the file says

func TestCheckUnclaimedPassesWhenEverythingIsClaimed(t *testing.T) {
	c := mustParse(t, minimal)
	require.NoError(t, c.CheckUnclaimed([]string{"ts", "dexie"}))
}
