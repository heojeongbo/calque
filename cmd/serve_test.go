package cmd_test

import (
	"strings"
	"testing"

	"github.com/lesomnus/xli/xlitest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/heojeongbo/calque/cmd"
	"github.com/heojeongbo/calque/internal/protoc"
)

// The plugin boundary had no test at all before this file: plugin.Serve was
// named exactly once in its own package, in its declaration. Every golden test
// starts at gen.Run, which is one layer inside — so the option string, the
// config lookup, the manifest, `quiet=`, and the exit-code contract were
// covered by nothing but the fact that a real build kept working.
//
// The command *is* the IO in xli, so a whole run fits in memory: a marshalled
// CodeGeneratorRequest goes in as stdin and the CodeGeneratorResponse comes
// back out of stdout, with no pipe and no subprocess.

// request compiles fixtures into the request buf would send.
func request(t *testing.T, param string, files ...string) []byte {
	t.Helper()

	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../testdata/proto/valid", "../testdata/proto/_upstream"}, param, files...)
	require.NoError(t, err)

	b, err := proto.Marshal(req)
	require.NoError(t, err)
	return b
}

// run drives the whole plugin: request in, response out.
func run(t *testing.T, param string, files ...string) (*pluginpb.CodeGeneratorResponse, xlitest.Result) {
	t.Helper()

	if len(files) == 0 {
		files = []string{"apptest.proto", "apptest_svc.proto"}
	}
	got := xlitest.Harness{
		Cmd: cmd.NewCmdRoot(cmd.Registry()),
		// A []byte and a string hold the same bytes; the request is binary and
		// survives the round trip either way.
		Stdin: string(request(t, param, files...)),
	}.Run(t)

	res := &pluginpb.CodeGeneratorResponse{}
	require.NoError(t, proto.Unmarshal([]byte(got.Stdout), res),
		"stdout is the protocol, and it did not parse as a response")
	return res, got
}

func TestServeAnswersARequest(t *testing.T) {
	res, got := run(t, "config=testdata/calque.yaml")

	require.NoError(t, got.Err)
	require.Empty(t, res.GetError())

	var names []string
	for _, f := range res.GetFile() {
		names = append(names, f.GetName())
	}
	require.ElementsMatch(t, []string{
		"apptest/client.g.ts",
		"apptest/db.g.ts",
		"apptest/tenant.db.g.ts",
		"apptest/user.db.g.ts",
	}, names, "the same files target/ts/golden_test.go pins, reached through the plugin")

	// The editions range the response advertises is part of the contract with
	// protoc, and nothing else asserts it.
	require.NotNil(t, res.SupportedFeatures)
	require.NotNil(t, res.MinimumEdition)
	require.NotNil(t, res.MaximumEdition)
}

// TestServeReportsAConfigErrorWithoutFailing is the exit-code contract.
//
// A config the user got wrong is response.error and a nil return, so the
// process exits 0 and buf prints the message and fails the build itself.
// Exiting non-zero here would make buf say the plugin crashed, which is a
// worse account of a typo.
func TestServeReportsAConfigErrorWithoutFailing(t *testing.T) {
	res, got := run(t, "config=testdata/unknown-target.yaml")

	require.NoError(t, got.Err, "a bad config is the user's mistake, not a failure to run")
	require.Contains(t, res.GetError(), `no target named "nope"`)
	require.Empty(t, res.GetFile())
}

func TestServeSaysWhereItLookedForAConfig(t *testing.T) {
	res, got := run(t, "config=testdata/nope.yaml")

	require.NoError(t, got.Err)
	// An absolute path, because "no such file: calque.yaml" is unhelpful when
	// the plugin's working directory is not the one you had in mind.
	require.Contains(t, res.GetError(), "no config at /")
	require.Contains(t, res.GetError(), "testdata/nope.yaml")
}

func TestServeRefusesAnUnknownOpt(t *testing.T) {
	res, got := run(t, "config=testdata/calque.yaml,nonsense=1")

	require.NoError(t, got.Err)
	require.Contains(t, res.GetError(), `unknown opt "nonsense"`)
}

// TestManifestListsWhatWasEmitted: a plugin can only add files, so pruning is
// the build's job and this is what it prunes against.
func TestManifestListsWhatWasEmitted(t *testing.T) {
	res, got := run(t, "config=testdata/calque.yaml,manifest=calque.manifest")
	require.NoError(t, got.Err)

	var manifest string
	for _, f := range res.GetFile() {
		if f.GetName() == "calque.manifest" {
			manifest = f.GetContent()
		}
	}
	require.NotEmpty(t, manifest, "manifest= was given and no manifest came back")
	require.Contains(t, manifest, "apptest/db.g.ts")
	require.Contains(t, manifest, "Anything else under the output root is stale.")
}

// TestQuietSilencesProgress checks the half of `quiet=` that this corpus can
// show. That it does *not* silence a warning is gen.Run's contract and is
// tested there, against a backend built to fall short of the schema --
// TestAnAcceptedShortfallIsStillReported. No fixture here produces one, and
// inventing a schema so that an end-to-end test could re-assert a unit test is
// the wrong way round.
func TestQuietSilencesProgress(t *testing.T) {
	_, loud := run(t, "config=testdata/calque.yaml")
	require.Contains(t, loud.Stderr, "calque: [1/1]")
	require.Contains(t, loud.Stderr, "2 entities, 1 target")

	_, quiet := run(t, "config=testdata/calque.yaml,quiet=true")
	require.Empty(t, quiet.Stderr)
}

// TestProgressNeverWritesCarriageReturn, at the boundary rather than at the
// Progress type. buf pipes a plugin's stderr, so a spinner would leave the log
// full of \r; gen has a unit test for that and this is the end-to-end half.
func TestProgressNeverWritesCarriageReturn(t *testing.T) {
	_, got := run(t, "config=testdata/calque.yaml")
	require.NotContains(t, got.Stderr, "\r")
	for _, line := range strings.Split(strings.TrimSuffix(got.Stderr, "\n"), "\n") {
		require.True(t, strings.HasPrefix(line, "calque:"),
			"buf interleaves this with its own output, so every line says who wrote it: %q", line)
	}
}

// TestNoSubcommandWithNothingOnStdin: a person who typed `calque` gets a
// sentence, not a CodeGeneratorResponse rendered on their terminal.
func TestNoSubcommandWithNothingOnStdin(t *testing.T) {
	got := xlitest.Run(t, cmd.NewCmdRoot(cmd.Registry()))

	require.ErrorContains(t, got.Err, "nothing on stdin")
	require.ErrorContains(t, got.Err, "calque is a protoc plugin")
	require.Empty(t, got.Stdout, "stdout is the protocol; nothing else may go there")
}
