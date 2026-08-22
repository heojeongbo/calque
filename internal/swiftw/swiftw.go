// Package swiftw writes Swift.
//
// The writing itself is [linew], which says why there is no formatter and what
// changing it costs. What is here is the two things that are about Swift rather
// than about lines: how deep one level is, and how a string literal is spelled.
//
// The Swift target did neither through a writer at first -- it built a
// strings.Builder with "\n" and four-space literals in the format strings, which
// is what internal/tsw and internal/prow were before they were one package. The
// cost of that shape is not the duplication; it is that a depth kept in the
// format string cannot be got wrong visibly, so an emitter leaks a level and the
// output is merely oddly indented.
package swiftw

import (
	"strconv"

	"github.com/heojeongbo/calque/internal/linew"
)

// Indent is one level, and it is spaces because Swift's own tooling emits
// spaces and this target's committed output already has them.
const Indent = "    "

// File accumulates one emitted file.
type File struct{ linew.File }

// New returns an empty file indented the way Swift is.
func New() *File {
	f := &File{}
	f.SetIndent(Indent)
	return f
}

// Str renders a Swift string literal, which is Go's %q.
//
// It is the same claim prow.Quote makes and it holds for the same reason: the
// only strings that reach it are identifiers, column names and table names, and
// Go's escaping and Swift's agree on every character those can contain. A string
// that could contain more would need this to be an error rather than a guess.
func Str(s string) string { return strconv.Quote(s) }
