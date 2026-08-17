package service_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	"github.com/heojeongbo/calque/target/service"
)

// TestReferenceOutput generates from a real proto tree and compares against what
// protoc-gen-orm-service committed for it.
//
// The diff is expected to be non-empty and to be *only comments*: the header names
// calque instead of that plugin, the source line drops a doubled extension, and
// "retrieves a Env" gains an n. Everything else has to match byte for byte,
// because everything else is a contract — field numbers a stored audit log and a
// browser cache depend on, and names hand-written extensions reference.
//
// Set CALQUE_REFERENCE_PROTO to the proto root and CALQUE_REFERENCE_SVC to the
// directory the previous generator wrote, and the test runs; unset, it skips, which
// keeps a private tree out of this repository while still gating on it.
//
// One line is exempt, and it is worth knowing why. `option go_package` comes from
// the descriptor, and buf rewrites it before a plugin ever sees it -- managed mode
// maps a whole module to one import path. This test compiles the tree with protoc
// directly, so it sees the raw option and a tree using that override disagrees on
// exactly that line. Verifying it needs the real pipeline, which is what the gate
// in docs/migrating.md does: generate through buf and diff the tracked output.
func TestReferenceOutput(t *testing.T) {
	root := os.Getenv("CALQUE_REFERENCE_PROTO")
	want := os.Getenv("CALQUE_REFERENCE_SVC")
	if root == "" || want == "" {
		t.Skip("set CALQUE_REFERENCE_PROTO and CALQUE_REFERENCE_SVC")
	}

	var files []string
	require.NoError(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".proto") {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	}))
	require.NotEmpty(t, files)

	upstream, _ := filepath.Abs("../../testdata/proto/_upstream")
	paths := []string{root, upstream}
	if extra := os.Getenv("CALQUE_REFERENCE_INCLUDE"); extra != "" {
		paths = append(paths, strings.Split(extra, ":")...)
	}

	req, err := protoc.CompileRequest(t.Context(), paths, "", files...)
	require.NoError(t, err)

	s, reg, err := ormcompat.ParseFiles(req)
	require.NoError(t, err)

	cfg, err := gen.ParseConfig([]byte("version: 1\ntargets:\n  - target: service\n"), "calque.yaml")
	require.NoError(t, err)

	out, err := gen.Run(s, cfg, gen.NewRegistry().Target(service.New()), gen.WithDescriptors(req, reg))
	require.NoError(t, err)
	require.NotEmpty(t, out.Names())

	for _, name := range out.Names() {
		t.Run(name, func(t *testing.T) {
			expected, err := os.ReadFile(filepath.Join(want, name))
			if os.IsNotExist(err) {
				t.Skipf("%s has no committed counterpart", name)
			}
			require.NoError(t, err)

			body, _ := out.Body(name)
			wl := strings.Split(string(expected), "\n")
			gl := strings.Split(string(body), "\n")
			require.Equal(t, len(wl), len(gl), "line count")

			for i := range wl {
				if wl[i] == gl[i] {
					continue
				}
				if strings.HasPrefix(strings.TrimSpace(gl[i]), "option go_package") {
					continue // see the note above
				}
				// A comment may differ; anything else may not.
				require.True(t, strings.HasPrefix(strings.TrimSpace(gl[i]), "//"),
					"line %d is not a comment and does not match:\n  want: %q\n   got: %q", i+1, wl[i], gl[i])
				require.True(t, strings.HasPrefix(strings.TrimSpace(wl[i]), "//"),
					"line %d replaced a declaration with a comment:\n  want: %q\n   got: %q", i+1, wl[i], gl[i])
			}
		})
	}
}
