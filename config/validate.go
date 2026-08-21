package config

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

func (c *Config) validate() error {
	switch {
	case c.Version == 0:
		return fmt.Errorf("%s: `version` is required; this calque understands version %d", c.path, FormatVersion)
	case c.Version != FormatVersion:
		return fmt.Errorf("%s: config version %d, but this calque understands version %d", c.path, c.Version, FormatVersion)
	}

	if len(c.Targets) == 0 {
		return fmt.Errorf("%s: `targets` is empty; list at least one, e.g. `- {target: ts, backend: dexie}`", c.path)
	}

	seen := map[string]int{}
	for i, t := range c.Targets {
		where := fmt.Sprintf("%s: targets[%d]", c.path, i)

		if t.Target == "" {
			return fmt.Errorf("%s: `target` is required (a registered target name)", where)
		}
		if err := checkName(t.Target); err != nil {
			return fmt.Errorf("%s: target: %w", where, err)
		}
		// `backend` is checked for spelling here and for presence in Run, which
		// is the only place that knows whether the target is storeless -- a
		// service contract describes no store and takes no backend at all.
		if t.Backend != "" {
			if err := checkName(t.Backend); err != nil {
				return fmt.Errorf("%s(%s): backend: %w", where, t.Target, err)
			}
		}
		if t.Name != "" {
			if err := checkName(t.Name); err != nil {
				return fmt.Errorf("%s(%s): name: %w", where, t.Target, err)
			}
		}
		if err := checkOut(t.Out); err != nil {
			return fmt.Errorf("%s(%s): out: %w", where, t.Label(), err)
		}

		label := t.Label()
		if prev, dup := seen[label]; dup {
			return fmt.Errorf("%s: targets[%d] and targets[%d] are both named %q; give one a distinct `name`",
				c.path, prev, i, label)
		}
		seen[label] = i
	}
	return nil
}

// nameRE is the spelling a target, backend or label may have.
//
// It is here so that the json schema and this validator agree. The schema
// declared the pattern from the start and the code did not check it, which meant
// `target: TS` was a schema violation an editor flagged and a build did not --
// it failed later with "no target named" and never said the name was the
// problem.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func checkName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("%q is not a name; it must be lowercase, start with a letter, and hold only letters, digits and dashes", name)
	}
	return nil
}

// checkOut refuses an `out` that is not a relative subdirectory.
//
// buf owns the output root and hands it to the plugin; a path that escapes it
// is not a path to normalize, it is a generator writing somewhere nobody asked
// for.
func checkOut(out string) error {
	if out == "" {
		return nil // means the output root itself
	}
	if path.IsAbs(out) || strings.HasPrefix(out, "/") {
		return fmt.Errorf("%q is absolute; it must be a subdirectory of the output root buf gives the plugin", out)
	}
	for _, seg := range strings.Split(path.Clean(out), "/") {
		if seg == ".." {
			return fmt.Errorf("%q escapes the output root", out)
		}
	}
	return nil
}
