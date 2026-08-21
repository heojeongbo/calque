package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSourcesAreTheDirectory pins the list above to what is actually vendored.
//
// The list is written out three times -- here, and twice in ormopt/drift_test.go
// -- and the copies cannot be collapsed without the tool importing the package
// it generates, which is a poor thing to depend on when the reason you are
// running it is that that package is wrong.
//
// So this pins the one that matters. Adding a seventh .proto and forgetting the
// tool's list means the file is never generated and nothing else notices: the
// drift tests compare the shapes of what they were told about. Once the tool's
// list is right, TestGeneratedMatchesProto sees a linked file its own list does
// not mention and fails in turn, so the three converge from here.
//
// It is the same check TestEveryInvalidFixtureIsClaimed makes about the invalid
// corpus, for the same reason: a table and a directory that are meant to be the
// same set should not be able to stop being one.
func TestSourcesAreTheDirectory(t *testing.T) {
	const dir = "../../proto/calque/orm"

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var onDisk []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".proto") {
			continue
		}
		onDisk = append(onDisk, filepath.ToSlash(filepath.Join("calque/orm", e.Name())))
	}

	require.ElementsMatch(t, onDisk, sources,
		"the vendored vocabulary and the list gen-ormopt generates from have diverged;\n"+
			"a .proto that is not in the list produces no Go, silently")
}
