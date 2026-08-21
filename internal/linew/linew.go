// Package linew accumulates emitted text one line at a time, and does as
// little as possible while doing it.
//
// There is no import management, no sorting, no formatter and no
// prettification. That is not an oversight — it is the requirement. calque's
// first job is to emit output byte-identical to the generator it replaces, so
// that swapping generators in an existing repository produces a diff a person
// can actually read, and every convenience a writer might offer is a way for
// the bytes to differ. The committed TypeScript this has to reproduce contains,
// among other things, a trailing space on `export type Db = `, one file ending
// "}\n\n" while its siblings end "}\n", and four imports ending in ";" beside
// three that do not. A formatter eats all four.
//
// Two packages wrap this: internal/tsw for TypeScript and internal/prow for
// .proto. They were the same forty lines twice, which meant a fix to one was a
// fix to one. Now it is not — and the cost of that is the thing to know before
// changing anything here: **a change to this file changes what two generators
// emit.** Both golden suites are the gate, and both have to be run.
//
//	UPDATE_GOLDEN=1 go test ./target/... && git diff --exit-code
package linew

import (
	"bytes"
	"fmt"
)

// File accumulates one emitted file.
type File struct {
	b     bytes.Buffer
	depth int
}

// P writes one line at the current depth: the arguments concatenated with no
// separator, then a newline.
//
// The shape is protogen's, so porting a line from the generator being replaced
// is mechanical rather than an act of interpretation.
//
// A line with no arguments is a blank line and gets no indentation, because
// trailing tabs on an otherwise empty line are a diff nobody meant to write.
// An emitter that never moves the depth therefore gets exactly what an
// unindented writer would give it, which is what let the TypeScript one adopt
// this without moving a byte.
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
