// Package tsw writes TypeScript, and does as little as possible while doing it.
//
// There is no import management, no sorting, no formatter, and no
// prettification. That is not an oversight — it is the requirement. calque's
// first job is to emit output byte-identical to what protoc-gen-orm-ts emits,
// so that swapping generators in an existing repository produces a diff a
// person can actually read, and every convenience a writer might offer is a way
// for the bytes to differ.
//
// The committed output this has to reproduce contains, among other things:
//
//   - a trailing space on `export type Db = ` in db.g.ts
//   - client.g.ts ending "}\n\n" while the other files end "}\n"
//   - the first four imports of a .db.g.ts ending in ";" and the last three not
//   - tabs, not spaces
//
// A formatter eats all four. So there is no formatter, and the emitter is
// responsible for its own bytes.
package tsw

import (
	"bytes"
	"fmt"
	"strings"
)

// File accumulates one emitted file.
type File struct {
	b bytes.Buffer
}

// New returns an empty file.
func New() *File { return &File{} }

// P writes one line: the arguments concatenated with no separator, then a
// newline.
//
// The shape is protogen's, so porting a line from the generator being replaced
// is mechanical rather than an act of interpretation.
func (f *File) P(args ...any) {
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

// Len is how many bytes have been written.
func (f *File) Len() int { return f.b.Len() }

// Bytes is the file.
func (f *File) Bytes() []byte { return f.b.Bytes() }

// String is the file, as a string.
func (f *File) String() string { return f.b.String() }

// LowerFirst lower-cases the first character and leaves the rest alone.
//
// This is not a casing function and must not be replaced by one. It is
// protoc-gen-orm-ts's `camel`:
//
//	func camel(v string) string { return strings.ToLower(v[:1]) + v[1:] }
//
// so "BTExecutor" becomes "bTExecutor" and not "btExecutor". That spelling is
// in 265 call sites in the application being migrated, and in a hand-written
// ServiceClient implementation. A correct camelCase would break all of them —
// the wrong-looking output is the contract.
func LowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// Str renders a TypeScript double-quoted string literal.
//
// Only the escapes the corpus can produce are handled, and anything else is an
// error rather than a guess: a generator that silently mangles a string is how
// a name stops matching the thing it names.
func Str(s string) (string, error) {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				return "", fmt.Errorf("tsw: cannot render control character %#U in a string literal", r)
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}
