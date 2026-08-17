// Package prow writes .proto text, and does as little as possible while doing it.
//
// It is internal/tsw with one addition: a depth, because proto nests and
// TypeScript output did not need to. The reason for the rest of the austerity is
// the same — there is no formatter and no pretty-printer, because the first job
// of the target using this is to emit output byte-identical to what another
// generator emits, and every convenience a writer might offer is a way for the
// bytes to differ.
//
// The output it has to reproduce is tab-indented, one tab per level, and carries
// blank lines in specific places: after the package, after the import block only
// if there was one, between declarations but not after the last. Those are the
// caller's business. This decides indentation and nothing else.
package prow

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// File accumulates one emitted file.
type File struct {
	b     bytes.Buffer
	depth int
}

// New returns an empty file.
func New() *File { return &File{} }

// P writes one line at the current depth: the arguments concatenated with no
// separator, then a newline.
//
// A line with no arguments is a blank line and gets no indentation, because
// trailing tabs on an otherwise empty line are a diff nobody meant to write.
func (f *File) P(args ...any) {
	if len(args) > 0 {
		for range f.depth {
			f.b.WriteByte('\t')
		}
	}
	for _, a := range args {
		switch v := a.(type) {
		case string:
			f.b.WriteString(v)
		case fmt.Stringer:
			f.b.WriteString(v.String())
		default:
			fmt.Fprint(&f.b, v)
		}
	}
	f.b.WriteByte('\n')
}

// In and Out move one level. Out at depth zero stays at zero rather than
// panicking: an unbalanced emitter should produce visibly wrong output, not a
// crash in a plugin whose stdout is the protocol.
func (f *File) In() { f.depth++ }

func (f *File) Out() {
	if f.depth > 0 {
		f.depth--
	}
}

// Depth is the current level, for a caller that wants to assert its own balance.
func (f *File) Depth() int { return f.depth }

// Block writes `<head> {`, runs body one level in, then `}`.
//
// Every nesting in a proto file is this shape, and doing it by hand is how an
// emitter comes to leak a level after an early return.
func (f *File) Block(head string, body func()) {
	f.P(head, " {")
	f.In()
	body()
	f.Out()
	f.P("}")
}

// Len is how many bytes have been written.
func (f *File) Len() int { return f.b.Len() }

// Bytes is the file.
func (f *File) Bytes() []byte { return f.b.Bytes() }

// String is the file, as a string.
func (f *File) String() string { return f.b.String() }

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
