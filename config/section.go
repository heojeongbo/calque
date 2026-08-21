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
