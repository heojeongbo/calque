package gen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/gen"
)

func parse(t *testing.T, doc string) (*gen.Config, error) {
	t.Helper()
	return gen.ParseConfig([]byte(doc), "calque.yaml")
}

func mustParse(t *testing.T, doc string) *gen.Config {
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

	require.Equal(t, gen.ConfigVersion, c.Version)
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
// backend decodes its own section, and a typo inside it is loud.
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
// one thing and the command line says another, and the command line is later.
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

// TestNamesAreChecked: the json schema declared this pattern from the start and
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

func TestCheckUnclaimedPassesWhenEverythingIsClaimed(t *testing.T) {
	c := mustParse(t, minimal)
	require.NoError(t, c.CheckUnclaimed([]string{"ts", "dexie"}))
}

func TestEmptyAndMalformedDocuments(t *testing.T) {
	_, err := parse(t, "")
	require.ErrorContains(t, err, "config is empty")

	_, err = parse(t, "- a\n- b\n")
	require.ErrorContains(t, err, "must be a mapping")
}
