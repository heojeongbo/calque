package config

import (
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
)

// EnvPrefix is what every environment variable that says something about a
// calque config starts with.
//
// A name under it that nothing answers to is reported rather than ignored,
// because that is what a typo looks like. Which means anything else calque
// puts in this namespace has to be listed in reserved, or every build that
// exports one would report it.
const EnvPrefix = "CALQUE_"

// reserved are the CALQUE_ names calque defines somewhere other than a config
// section.
//
// The reference tests take their proto and output roots this way; see
// docs/development.md#reference-tests. Without this, a developer with those
// exported would be told five times per build that nothing reads them.
var reserved = []string{"CALQUE_REFERENCE_"}

// sectionPrefix is what a section's variables are called: CALQUE_, the section
// name, and then the path to the field. A dash in either is an underscore
// here, since a dash cannot be in an environment variable name.
//
// So `import_extension` of `ts` is CALQUE_TS_IMPORT_EXTENSION, derived from the
// yaml tag and written down nowhere.
//
// The mapping is not injective: a section `my-store` with a field `x` and a
// section `my` with a field `store_x` both answer to CALQUE_MY_STORE_X. No
// section name in this build has a dash, and documenting the ambiguity is
// cheaper than machinery for it -- `calque config` shows which section read
// which name if it ever comes up.
func sectionPrefix(section string) string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(section, "-", "_")) + "_"
}

// ReadEnv records what the environment says, for [Config.Section] to apply to
// each section as it is claimed.
//
// It records and does not apply, for the same reason [Config.Override] does:
// the file's own section is decoded first, an environment variable is meant to
// win over what the file said, and there is nothing to win over until a target
// or backend asks for the section.
//
// environ is a parameter -- what os.Environ returns -- and never read from the
// process here. That is what makes the golden tests hermetic by construction:
// they build a Config and never call this, so no ambient variable can reach
// the bytes they compare.
//
// Names matching a reserved prefix are dropped rather than tracked. `version:`
// and `targets:` take no environment at all: what to generate is what the file
// says, and letting a shell change it quietly is a reproducibility hole in a
// tool whose argument is that there are none.
func (c *Config) ReadEnv(environ []string) {
	for _, e := range environ {
		name, value, ok := strings.Cut(e, "=")
		if !ok || !strings.HasPrefix(name, EnvPrefix) {
			continue
		}
		if slices.ContainsFunc(reserved, func(p string) bool { return strings.HasPrefix(name, p) }) {
			continue
		}
		c.environ[name] = value
	}
}

// EnvApplied is every environment variable that changed a setting, sorted,
// with the section it reached.
//
// A build reports these on stderr, unsilenceably. An environment variable can
// move emitted bytes and leaves no trace in the tree, so if it is not in the
// build log then two machines produce two trees and nothing says why.
func (c *Config) EnvApplied() []EnvSetting {
	out := make([]EnvSetting, 0, len(c.envRead))
	for name, section := range c.envRead {
		out = append(out, EnvSetting{Name: name, Section: section})
	}
	slices.SortFunc(out, func(a, b EnvSetting) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// EnvSetting is one environment variable that was read, and by what.
type EnvSetting struct {
	Name    string
	Section string
}

// UnreadEnv is every CALQUE_ name nothing answered to, sorted.
//
// It is deliberately not part of [Config.CheckUnclaimed], which is an error. A
// section in the file is in the repository and has an owner; an environment
// variable is ambient and may belong to something else on the machine
// entirely. Refusing to generate over one would make calque unusable on a
// developer's shell, so it is said and not enforced.
func (c *Config) UnreadEnv() []string {
	var out []string
	for name := range c.environ {
		if _, read := c.envRead[name]; !read {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// EnvNames is every name a section answers to, in the order its fields appear.
//
// It is recorded when the section is claimed, so a name that nearly matched can
// be reported against the real thing it missed.
func (c *Config) EnvNames(section string) []string { return c.envNames[section] }

// recordEnvNames walks a section's options and records every name it answers
// to, reading nothing.
//
// It runs on every claim, whether or not the environment says anything, so
// that a near miss can be reported against the real thing it missed even for a
// section nobody configured.
func (c *Config) recordEnvNames(section string, v any) {
	rv, ok := envRoot(v)
	if !ok {
		// A section whose options are not a struct answers to no name. That is
		// not an error: it is what "nothing to read into" looks like.
		c.envNames[section] = nil
		return
	}

	var names []string
	// The visitor never reads, so the walk allocates no missing struct and the
	// error is always nil.
	_, _ = walkEnv(sectionPrefix(section), rv, nil, func(name string, _ reflect.Value) (bool, error) {
		names = append(names, name)
		return false, nil
	})
	c.envNames[section] = names
}

// envMatches is the recorded variables a section answers to.
func (c *Config) envMatches(section string) []string {
	var out []string
	for _, name := range c.envNames[section] {
		if _, ok := c.environ[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// applyEnv reads the recorded environment into a section's options and reports
// which names it used.
func (c *Config) applyEnv(section string, v any) ([]string, error) {
	rv, ok := envRoot(v)
	if !ok {
		return nil, nil
	}

	var used []string
	_, err := walkEnv(sectionPrefix(section), rv, nil, func(name string, field reflect.Value) (bool, error) {
		value, ok := c.environ[name]
		if !ok {
			return false, nil
		}
		if err := setEnv(field, value); err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
		used = append(used, name)
		return true, nil
	})
	return used, err
}

// envRoot is what a walk starts at: something that is a struct and can be
// written to.
func envRoot(v any) (reflect.Value, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Type().Elem().Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return rv, true
}

// envVisitor is called with the environment variable name of a field a value
// can be read into. It reports whether it read one.
type envVisitor func(name string, field reflect.Value) (bool, error)

// walkEnv visits every field of the struct v, which may be a pointer to one,
// naming it after the path to it. It reports whether anything was read.
func walkEnv(prefix string, v reflect.Value, path []string, visit envVisitor) (bool, error) {
	// A struct that is not there yet is made only if something is read into
	// it, so that looking at the environment does not fill a configuration
	// with what nothing was said about.
	var missing reflect.Value
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			if !v.CanSet() {
				return false, nil
			}
			missing = v
			v = reflect.New(v.Type().Elem())
		}
		v = v.Elem()
	}

	read := false
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, inline, ok := envField(f)
		if !ok {
			continue
		}

		var (
			fv  = v.Field(i)
			err error
			one bool
		)
		switch {
		case inline:
			// An inlined struct is part of the one that holds it, so it adds
			// nothing to the path.
			one, err = walkEnv(prefix, fv, path, visit)
		case envLeaf(f.Type):
			one, err = visit(envKey(prefix, append(path, name)), fv)
		default:
			one, err = walkEnv(prefix, fv, append(path, name), visit)
		}
		if err != nil {
			return false, err
		}
		read = read || one
	}

	if read && missing.IsValid() {
		missing.Set(v.Addr())
	}
	return read, nil
}

// envField reads how a struct field is spelled in YAML, which is what it is
// named after here as well.
//
// The yaml tag is the single source of truth for both: `yaml:"-"` excludes a
// field from the file and from the environment, and a field added to a
// section's options answers to a name without anything being written anywhere.
func envField(f reflect.StructField) (name string, inline bool, ok bool) {
	tag, rest, _ := strings.Cut(f.Tag.Get("yaml"), ",")
	if tag == "-" {
		return "", false, false
	}
	name = tag
	if name == "" {
		// What the YAML decoder falls back to.
		name = strings.ToLower(f.Name)
	}
	return name, slices.Contains(strings.Split(rest, ","), "inline"), true
}

// envKey is the environment variable a field at the given path is read from.
func envKey(prefix string, path []string) string {
	name := strings.Join(path, "_")
	name = strings.ReplaceAll(name, "-", "_")
	return prefix + strings.ToUpper(name)
}

var (
	yamlBytes     = reflect.TypeFor[yaml.BytesUnmarshaler]()
	yamlInterface = reflect.TypeFor[yaml.InterfaceUnmarshaler]()
	textUnmarshal = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// envLeaf reports whether a value of this type is read as a whole rather than
// walked into: anything that is not a struct, and a struct that knows how to
// read itself.
func envLeaf(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return true
	}
	p := reflect.PointerTo(t)
	return p.Implements(yamlBytes) || p.Implements(yamlInterface) || p.Implements(textUnmarshal)
}

// setEnv reads a value into a field.
func setEnv(field reflect.Value, v string) error {
	// A value that is not there yet is made and read into. The decoder is of
	// no help here: handed a pointer to a pointer it leaves it nil and reports
	// nothing.
	if field.Kind() == reflect.Pointer {
		p := reflect.New(field.Type().Elem())
		if err := setEnv(p.Elem(), v); err != nil {
			return err
		}
		field.Set(p)
		return nil
	}

	// A string is taken as it is. Reading it as YAML would give a meaning to
	// the punctuation a module path or a header comment is full of --
	// "// Code generated by calque. DO NOT EDIT." is not a mapping.
	if field.Kind() == reflect.String {
		field.SetString(v)
		return nil
	}

	// Anything else is read by the decoder that reads the file, so a number, a
	// switch or a list means here what it means there.
	return yaml.Unmarshal([]byte(v), field.Addr().Interface())
}
