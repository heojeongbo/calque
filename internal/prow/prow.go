// Package prow writes .proto text.
//
// The writing itself is [linew], which says why there is no formatter and what
// changing it costs. What is here is the three things that are about proto
// rather than about lines: a string literal, a field line, and a comment.
//
// Unlike TypeScript, proto nests, so this one uses the depth. The output it has
// to reproduce is tab-indented, one tab per level, and carries blank lines in
// specific places: after the package, after the import block only if there was
// one, between declarations but not after the last. Those are the caller's
// business. The writer decides indentation and nothing else.
package prow

import (
	"strconv"
	"strings"

	"github.com/heojeongbo/calque/internal/linew"
)

// File accumulates one emitted file.
type File struct{ linew.File }

// New returns an empty file.
func New() *File { return &File{} }

// Quote renders a proto string literal the way protoc-gen-go's printer does,
// which is Go's %q.
//
// Only the option values a schema can produce go through this — a go_package, an
// import path — so Go's escaping and proto's agree on all of them.
func Quote(s string) string { return strconv.Quote(s) }

// Field renders one field line: label, type, name, number, and options.
//
// Options are rendered on their own lines inside brackets, which is what the
// generator being reproduced does even for a single option:
//
//	bytes id = 1 [
//		features.field_presence = IMPLICIT
//	];
//
// A field with no options is one line and no brackets.
//
// The separator is a trailing comma, where the printer being reproduced writes a
// leading one on continuation lines. That is a deliberate difference and it cannot
// show up in the output: that generator emits at most one option per field, so
// there is never a continuation line to disagree about. Reproducing the odd
// spelling would mean carrying it for a case neither generator reaches.
func (f *File) Field(label, typ, name string, number int32, opts ...string) {
	head := typ + " " + name + " = " + strconv.Itoa(int(number))
	if label != "" {
		head = label + " " + head
	}
	if len(opts) == 0 {
		f.P(head, ";")
		return
	}
	f.P(head, " [")
	f.In()
	for i, o := range opts {
		if i < len(opts)-1 {
			f.P(o, ",")
			continue
		}
		f.P(o)
	}
	f.Out()
	f.P("];")
}

// Comment writes `// ` lines, one per line of text.
//
// It does not wrap. A generated comment that rewraps when its subject is renamed
// produces a diff about nothing.
func (f *File) Comment(text string) {
	for line := range strings.SplitSeq(text, "\n") {
		if line == "" {
			f.P("//")
			continue
		}
		f.P("// ", line)
	}
}
