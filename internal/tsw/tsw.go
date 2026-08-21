// Package tsw writes TypeScript.
//
// The writing itself is [linew], which says why there is no formatter and what
// changing it costs. What is here is the two things that are about TypeScript
// rather than about lines: how a name is lower-cased, and how a string literal
// is spelled.
//
// The emitter never moves the depth. TypeScript output is reproduced from a
// generator that emitted tabs the caller decided on, so indentation is part of
// the line rather than a property of the writer.
package tsw

import (
	"fmt"
	"strings"

	"github.com/heojeongbo/calque/internal/linew"
)

// File accumulates one emitted file.
type File struct{ linew.File }

// New returns an empty file.
func New() *File { return &File{} }

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
