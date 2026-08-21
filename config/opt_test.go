package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOverrideWinsOverTheFile(t *testing.T) {
	c := mustParse(t, `
version: 1
targets:
  - {target: ts, backend: dexie}

ts:
  runtime: "@heojeongbo/calque-dexie"
  import_extension: ""
`)
	require.NoError(t, c.Override("ts.import_extension", ".js"))

	var opts struct {
		Runtime         string `yaml:"runtime"`
		ImportExtension string `yaml:"import_extension"`
	}
	ok, err := c.Section("ts", &opts)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ".js", opts.ImportExtension, "the override is applied on top of the file")
	require.Equal(t, "@heojeongbo/calque-dexie", opts.Runtime, "and leaves everything else alone")
}

// TestOverrideWithoutASection: overriding a setting in a section the file does
// not have is the common case -- the file is minimal and the build varies one
// thing -- so it has to work without the section being written out first.
func TestOverrideWithoutASection(t *testing.T) {
	c := mustParse(t, minimal)
	require.NoError(t, c.Override("dexie.compat", "none"))

	var opts struct {
		Compat string `yaml:"compat"`
	}
	ok, err := c.Section("dexie", &opts)
	require.NoError(t, err)
	require.True(t, ok, "the section exists because the command line gave it one")
	require.Equal(t, "none", opts.Compat)
	require.Empty(t, c.UnclaimedSections())
}

// TestOverrideValueIsReadAsYaml: `x.n=2` and a config saying `n: 2` have to mean
// the same thing, or the override is a second way to spell a value.
func TestOverrideValueIsReadAsYaml(t *testing.T) {
	c := mustParse(t, minimal)
	require.NoError(t, c.Override("ts.depth", "2"))
	require.NoError(t, c.Override("ts.strict", "true"))
	// A package name starts with a character yaml reserves. It is plainly a
	// string, and treating it as one is not a fallback so much as the case that
	// actually comes up.
	require.NoError(t, c.Override("ts.runtime", "@scope/pkg"))

	var opts struct {
		Depth   int    `yaml:"depth"`
		Strict  bool   `yaml:"strict"`
		Runtime string `yaml:"runtime"`
	}
	ok, err := c.Section("ts", &opts)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2, opts.Depth)
	require.True(t, opts.Strict)
	require.Equal(t, "@scope/pkg", opts.Runtime)
}

// TestOverrideNestsOnDots: `entsql.table.apptest.User=users` reaches into a map,
// because a flat key would have no way to say which map it meant.
func TestOverrideNestsOnDots(t *testing.T) {
	c := mustParse(t, minimal)
	require.NoError(t, c.Override("entsql.table.apptest.User", "users"))

	var opts struct {
		Table map[string]map[string]string `yaml:"table"`
	}
	ok, err := c.Section("entsql", &opts)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "users", opts.Table["apptest"]["User"])
}

// TestOverrideTypoIsLoud: an override naming a key the section does not have has
// to fail the same way the file would, and name the option the user typed rather
// than a line in a document nobody wrote.
func TestOverrideTypoIsLoud(t *testing.T) {
	c := mustParse(t, minimal)
	require.NoError(t, c.Override("dexie.compaat", "none"))

	var opts struct {
		Compat string `yaml:"compat"`
	}
	_, err := c.Section("dexie", &opts)
	require.Error(t, err)
	require.ErrorContains(t, err, "dexie.compaat=none")
}

// TestOverrideOfAnUnknownSectionIsReported: the section name is the one typo
// strict decoding cannot catch, and an override must not be a way around that.
func TestOverrideOfAnUnknownSectionIsReported(t *testing.T) {
	c := mustParse(t, minimal)
	require.NoError(t, c.Override("dexei.compat", "none"))

	require.Equal(t, []string{"dexei"}, c.UnclaimedSections())
	err := c.CheckUnclaimed([]string{"ts", "dexie"})
	require.ErrorContains(t, err, `nothing understands "dexei"`)
}

func TestOverrideNeedsASectionAndAKey(t *testing.T) {
	c := mustParse(t, minimal)
	require.ErrorContains(t, c.Override("compat", "none"), "<section>.<key>=<value>")
	require.ErrorContains(t, c.Override("dexie.", "none"), "empty key")
}
