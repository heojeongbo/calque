package config

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

// override is one `<section>.<key>=<value>` from the plugin's option string.
type override struct {
	// path is the key inside the section, split on dots: `table.apptest.User`
	// is {"table", "apptest", "User"}.
	path []string
	// value is the text as written. It is read as a yaml scalar, so `2` is a
	// number and `true` is a boolean, and anything yaml cannot read is a
	// string -- which is what makes `ts.runtime=@scope/pkg` work.
	value string
	// opt is the whole thing as the user typed it, for diagnostics.
	opt string
}

// Override records a `<section>.<key>=<value>` setting from the command line,
// to be applied on top of the file when that section is claimed.
//
// It is applied at claim time rather than now because the file's own section is
// decoded first: an override is meant to win over what the file said, and there
// is nothing to win over until a target or backend asks for it. A section
// nobody claims is still reported, so `dexei.compat=none` names the mistake
// instead of doing nothing.
func (c *Config) Override(path, value string) error {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return fmt.Errorf("opt %s=%s: an override is <section>.<key>=<value>", path, value)
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("opt %s=%s: an override has an empty key", path, value)
		}
	}
	section := parts[0]
	c.overrides[section] = append(c.overrides[section], override{
		path:  parts[1:],
		value: value,
		opt:   path + "=" + value,
	})
	return nil
}

// document renders an override as the fragment of the section it stands for, so
// that applying it is the same decode the file gets rather than a second way to
// set a field.
func (o override) document() ([]byte, error) {
	var v any = scalar(o.value)
	for i := len(o.path) - 1; i >= 0; i-- {
		v = map[string]any{o.path[i]: v}
	}
	return yaml.Marshal(v)
}

// scalar reads an option's value the way the same text in the config file would
// be read, so `quiet: true` and `x.quiet=true` mean one thing.
//
// Text yaml cannot read is the text itself. That is not a fallback so much as
// the common case: `ts.runtime=@scope/pkg` starts with a character yaml
// reserves, and the user is plainly naming a package.
func scalar(raw string) any {
	var v any
	if err := yaml.Unmarshal([]byte(raw), &v); err == nil && v != nil {
		return v
	}
	return raw
}
