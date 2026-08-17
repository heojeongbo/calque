package gen_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/gen"
)

// TestProgressNeverWritesCarriageReturn is the one that matters.
//
// Under buf, a plugin's stderr is a pipe, not a terminal. A spinner that
// overwrites a line with \r leaves the build log full of control characters and
// unreadable, and CI logs are exactly where someone reads this. Whole lines
// only, with the percentage inside the line.
func TestProgressNeverWritesCarriageReturn(t *testing.T) {
	var out bytes.Buffer
	p := gen.NewProgress(&out)

	p.Start(13, 2)
	p.TargetStart("ts", "dexie")
	p.Step("ts", 7, 13, "Robot")
	p.TargetDone("ts", "dexie", 19)
	p.TargetStart("go", "entsql")
	p.TargetDone("go", "entsql", 42)
	p.Finish()

	require.NotContains(t, out.String(), "\r", "a build log is not a terminal")
	require.NotContains(t, out.String(), "\x1b", "no escape sequences either")

	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		require.True(t, strings.HasPrefix(line, "calque:"),
			"every line is attributable to calque, since it shares stderr with buf: %q", line)
	}
}

func TestProgressReportsSharesAndTotals(t *testing.T) {
	var out bytes.Buffer
	p := gen.NewProgress(&out)

	p.Start(13, 2)
	p.TargetStart("ts", "dexie")
	p.TargetDone("ts", "dexie", 19)
	p.TargetStart("go", "entsql")
	p.TargetDone("go", "entsql", 42)
	p.Finish()

	s := out.String()
	require.Contains(t, s, "13 entities, 2 targets")
	require.Contains(t, s, "[1/2]  50%  ts → dexie  19 files")
	require.Contains(t, s, "[2/2] 100%  go → entsql  42 files")
	require.Contains(t, s, "61 files", "the total is the sum, not the last target's")
}

func TestProgressPluralises(t *testing.T) {
	var out bytes.Buffer
	p := gen.NewProgress(&out)
	p.Start(1, 1)
	p.TargetStart("ts", "dexie")
	p.TargetDone("ts", "dexie", 1)
	p.Finish()

	s := out.String()
	require.Contains(t, s, "1 entity, 1 target")
	require.Contains(t, s, "1 file")
	require.NotContains(t, s, "1 entities")
	require.NotContains(t, s, "1 files")
}

// TestProgressToNilWriterIsSilent means a caller does not have to branch.
func TestProgressToNilWriterIsSilent(t *testing.T) {
	p := gen.NewProgress(nil)
	require.NotPanics(t, func() {
		p.Start(1, 1)
		p.TargetStart("ts", "dexie")
		p.Step("ts", 1, 1, "User")
		p.TargetDone("ts", "dexie", 1)
		p.Finish()
	})
}

// TestWarningsAreNotSilencedByQuiet is the reason progress and warnings are
// separate options.
//
// Progress is a convenience. A warning is a fact about the result -- here, that
// a unique constraint the schema asked for will not exist. Putting both behind
// one switch would mean `quiet` hid it.
func TestWarningsAreNotSilencedByQuiet(t *testing.T) {
	s := fixtureSchema(t)
	lenient := documentStore()
	lenient.lenient = true

	var warn, prog bytes.Buffer
	r := gen.NewRegistry().Target(&fakeTarget{name: "ts"}).Backend(lenient)

	_, err := gen.Run(s, config(t, "version: 1\ntargets:\n  - {target: ts, backend: doc}\n"), r,
		gen.WithWarnings(&warn),
		gen.WithProgress(gen.NewProgress(nil)), // quiet
	)
	require.NoError(t, err, "a lenient backend reports and continues")

	require.Empty(t, prog.String())
	require.Contains(t, warn.String(), "cannot enforce a unique index",
		"the constraint that will not exist is still said out loud")
}
