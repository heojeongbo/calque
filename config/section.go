package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Section strictly decodes the top-level extension section key into v and marks
// it claimed; ok is false when neither the config nor the command line said
// anything about it.
//
// A target or backend calls this for its own options. Decoding strictly is what
// makes a typo inside the section loud; UnclaimedSections is what makes a typo
// in the section *name* loud.
func (c *Config) Section(key string, v any) (ok bool, err error) {
	// Remembered before the early return, and remembered whether or not
	// anything was decoded into it.
	//
	// A section's Go type belongs to whoever claims it -- *ts.Options lives in
	// target/ts -- so this package cannot name one and could never render the
	// effective configuration by itself. It does not have to: the caller hands
	// it a pointer and then goes on mutating that same value, applying its own
	// defaults after this returns. Keeping the pointer is what lets
	// `calque config` print what the run will actually use, defaults included,
	// without any target having to say anything new.
	c.claimants[key] = v

	node, found := c.extra[key]
	overrides := c.overrides[key]
	if !found && len(overrides) == 0 {
		return false, nil
	}
	c.claimed[key] = true

	if found {
		if err := yaml.NodeToValue(node, v, yaml.Strict()); err != nil {
			return true, fmt.Errorf("%s: %s: %w", c.path, key, err)
		}
	}

	// One at a time, so a bad key is reported as the option the user typed
	// rather than as a line in a document they never wrote.
	for _, o := range overrides {
		doc, err := o.document()
		if err != nil {
			return true, fmt.Errorf("opt %s: %w", o.opt, err)
		}
		if err := yaml.UnmarshalWithOptions(doc, v, yaml.Strict()); err != nil {
			return true, fmt.Errorf("opt %s: %w", o.opt, err)
		}
	}
	return true, nil
}

// UnclaimedSections is the sections nothing claimed, from the file or from the
// command line, sorted.
func (c *Config) UnclaimedSections() []string {
	seen := map[string]bool{}
	var out []string
	for key := range c.extra {
		if !c.claimed[key] && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	for key := range c.overrides {
		if !c.claimed[key] && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// CheckUnclaimed turns leftover sections into an error naming what was
// understood, so a misspelled `dexei:` says what it should have been.
func (c *Config) CheckUnclaimed(registered []string) error {
	left := c.UnclaimedSections()
	if len(left) == 0 {
		return nil
	}
	known := append(KnownSections(), registered...)
	sort.Strings(known)
	return fmt.Errorf("%s: nothing understands %s\n\tthis build knows: %s",
		c.path, strings.Join(quoteAll(left), ", "), strings.Join(known, ", "))
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// Claimants is what every section that was offered decoded into, keyed by
// section name and sorted by it.
//
// The values are the pointers the claimants passed to Section and have gone on
// filling in since, so this is the effective configuration and not the file's
// half of it. It is for `calque config`, which is the only caller: a target
// asking what another target was configured with would be reaching across a
// seam that exists.
func (c *Config) Claimants() []Claimant {
	keys := make([]string, 0, len(c.claimants))
	for k := range c.claimants {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Claimant, 0, len(keys))
	for _, k := range keys {
		out = append(out, Claimant{
			Section: k,
			Options: c.claimants[k],
			FromFile: func() bool {
				_, ok := c.extra[k]
				return ok
			}(),
			Overridden: len(c.overrides[k]) > 0,
		})
	}
	return out
}

// Claimant is one section and what it was decoded into.
type Claimant struct {
	// Section is the top-level key, which is also the target's or backend's
	// own name.
	Section string
	// Options is the pointer the claimant passed to Section.
	Options any
	// FromFile reports whether the config file had this section at all.
	FromFile bool
	// Overridden reports whether an `opt=` named it.
	Overridden bool
}
