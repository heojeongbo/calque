package ormcompat_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// invalidFixtures pins the message every rejected schema produces.
//
// The corpus is modelled on upstream's examples/graphtest/invalid, for the same
// reason it has one: a validation rule with no broken fixture is a rule nobody
// has watched fail, and the predecessor -- which had no Go tests at all --
// shipped two rules that crashed instead of complaining.
//
// The `want` strings are deliberately fragments of the user-facing text. If a
// message is reworded, this fails, which is the point: the message is the
// feature.
var invalidFixtures = map[string]string{
	"field_and_edge":     `only one of "orm.field" or "orm.edge"`,
	"field_type_message": "a field cannot be a message type",
	"no_key":             "no key is defined",
	"two_keys":           "there can be only one key",
	"key_not_unique":     "the key must be unique",
	"key_nullable":       "the key cannot be nullable",
	"key_not_immutable":  "the key must be immutable",

	"edge_repeated_unique":   "an edge with repeated cardinality cannot be unique",
	"edge_on_scalar":         "an edge must reference a message",
	"edge_target_not_entity": "is not an entity",
	"edge_map":               "a map cannot be an edge",

	"index_no_ref":                     "at least one prop",
	"index_ref_not_found":              "no prop named nope",
	"index_ref_name_mismatch":          "alias is field 4, not 7",
	"index_unnamed":                    "an index must be named",
	"index_includes_erased_non_unique": "includes_erased says nothing about an index that is not unique",

	"version_is_key":   "the version field cannot be the key",
	"version_unique":   "the version field cannot be unique, nullable or immutable",
	"version_not_time": "only the time type supports versioning",
	"version_two":      "there can be only one version field",

	"erased_is_key":       "the erased field cannot be the key",
	"erased_unique":       "the erased field cannot be unique or immutable",
	"erased_and_version":  "the erased field cannot also be the version field",
	"erased_not_time":     "only the time type can say that a row was erased",
	"erased_not_nullable": "being null is how it says the row is still there",
	"erased_two":          "there can be only one erased field",

	"back_ref_not_found":       "which invalid.T does not have",
	"back_ref_name_mismatch":   "back reference names wrong",
	"back_ref_not_edge":        "a field rather than an edge",
	"back_ref_unique_repeated": "is unique, so this edge cannot be repeated",
}

func TestInvalidSchemasAreRefused(t *testing.T) {
	for name, want := range invalidFixtures {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, name+".proto")
			require.Error(t, err, "this schema must not be accepted")
			require.Contains(t, err.Error(), want,
				"the message a user reads has changed")
		})
	}
}

// TestEveryInvalidFixtureIsClaimed keeps the corpus and the table in step.
//
// Without it, adding a fixture and forgetting the table entry leaves a proto in
// the tree that nothing ever compiles -- which reads as coverage and is not.
func TestEveryInvalidFixtureIsClaimed(t *testing.T) {
	entries, err := os.ReadDir("../testdata/proto/invalid")
	require.NoError(t, err)

	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".proto")
		if name == entry.Name() {
			continue
		}
		require.Contains(t, invalidFixtures, name,
			"%s is in the corpus but no test says what it should report",
			filepath.Join("testdata/proto/invalid", entry.Name()))
	}
	require.Len(t, invalidFixtures, len(entries),
		"the table names a fixture that is not in the corpus")
}
