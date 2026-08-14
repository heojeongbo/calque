package schema

import (
	"fmt"
	"strings"
)

// Diagnostic is one problem with one thing, named by the path to it.
type Diagnostic struct {
	// Path names what is wrong:
	//
	//	apptest.User.alias
	//	apptest.User.{indexes}(slug).refs[1]
	Path string

	// Msg says what is wrong: lower case, no trailing punctuation, and about
	// the schema rather than about calque.
	Msg string

	// Hint, when set, says what to do about it, on its own line. It is for the
	// cases where knowing the rule is the hard part.
	Hint string
}

func (d Diagnostic) String() string {
	s := d.Path + ": " + d.Msg
	if d.Hint != "" {
		s += "\n\t" + d.Hint
	}
	return s
}

// Diagnostics is every problem found, in the order they were found.
//
// Collecting rather than returning the first is the point. A schema with six
// bad annotations should take one run to fix, not six -- and a generator that
// stops at the first is a generator people run six times.
//
// The zero value is ready to use.
type Diagnostics struct {
	// found is shared with every Diagnostics that At produced, so a problem
	// recorded deep in a walk lands in the set the caller holds.
	found *[]Diagnostic
	// prefix is what At has accumulated.
	prefix string
}

func (d *Diagnostics) store() *[]Diagnostic {
	if d.found == nil {
		d.found = &[]Diagnostic{}
	}
	return d.found
}

// At returns a Diagnostics that prefixes every path it is given, so a walk into
// an entity does not carry the path by hand and cannot forget it.
//
// It writes through to the Diagnostics it came from.
func (d *Diagnostics) At(prefix string) *Diagnostics {
	return &Diagnostics{found: d.store(), prefix: d.join(prefix)}
}

func (d *Diagnostics) join(path string) string {
	switch {
	case d.prefix == "":
		return path
	case path == "":
		return d.prefix
	default:
		return d.prefix + "." + path
	}
}

// Add records a problem.
func (d *Diagnostics) Add(path, msg string) {
	s := d.store()
	*s = append(*s, Diagnostic{Path: d.join(path), Msg: msg})
}

// Addf records a problem with a formatted message.
func (d *Diagnostics) Addf(path, format string, a ...any) {
	d.Add(path, fmt.Sprintf(format, a...))
}

// Hintf records a problem and what to do about it.
func (d *Diagnostics) Hintf(path, msg, hint string) {
	s := d.store()
	*s = append(*s, Diagnostic{Path: d.join(path), Msg: msg, Hint: hint})
}

// Len is how many problems were found.
func (d *Diagnostics) Len() int {
	if d.found == nil {
		return 0
	}
	return len(*d.found)
}

// List is every problem found.
func (d *Diagnostics) List() []Diagnostic {
	if d.found == nil {
		return nil
	}
	return *d.found
}

// Err returns d when it holds anything and nil otherwise, so a caller can end
// with `return s, diags.Err()`.
func (d *Diagnostics) Err() error {
	if d.Len() == 0 {
		return nil
	}
	return d
}

// Error renders one diagnostic per line.
//
// The whole set goes into CodeGeneratorResponse.error, which buf prints
// verbatim, so this is what a user actually reads.
func (d *Diagnostics) Error() string {
	list := d.List()
	parts := make([]string, len(list))
	for i, diag := range list {
		parts[i] = diag.String()
	}
	return strings.Join(parts, "\n")
}
