package gen

import (
	"fmt"
	"strings"
)

// OneOf defaults an empty enum-shaped option and refuses anything unlisted.
//
// Three sections spelled one rule three ways -- dexie's compat, entsql's
// dialect, swift's access_level -- and three ways is three messages a person has
// to learn for one kind of mistake. The rule is one: the zero value means the
// default, and a value that is not on the list is a typo rather than something
// nobody has implemented yet.
//
// The claimant is named because a config has many sections and the message has
// to say which one was wrong, and the field is named because a section has many
// keys.
func OneOf[T ~string](claimant, field string, v, def T, allowed ...T) (T, error) {
	if v == "" {
		return def, nil
	}
	for _, a := range allowed {
		if v == a {
			return v, nil
		}
	}
	return "", fmt.Errorf("%s: %s %q is not %s", claimant, field, string(v), orList(allowed))
}

// orList renders "a", "a or b", "a, b or c" -- the way the message that has to
// list what it understands already read before there was one function writing
// it.
func orList[T ~string](values []T) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = string(v)
	}
	switch len(parts) {
	case 0:
		return "anything this build knows"
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1]
	}
}

// Claim decodes a claimant's own config section into o and applies its defaults.
//
// The same five lines opened Configure in every target and every backend:
// declare the zero value, decode strictly into it, default it, keep it. What
// differs is the defaults, which stay a method on the options type -- passed as
// a method expression, so it does not have to be exported to be reachable here.
//
// It takes a pointer rather than returning a value, and that is the whole reason
// it is worth having. Section remembers what it decoded into, and `calque
// config` prints the *effective* settings by reading that back -- so a default
// applied to a copy afterwards is a default the config command cannot see. This
// was written the other way round first, and cmd's test caught `compat: ""` in
// output that had been reporting `compat: orm-ts`.
//
// Not finding the section is not an error. It means nobody configured this
// claimant and its defaults stand, which is what lets a target be added to an
// existing calque.yaml without editing it.
func Claim[O any](cfg *Config, section string, o *O, defaults func(*O)) error {
	if _, err := cfg.Section(section, o); err != nil {
		return err
	}
	if defaults != nil {
		defaults(o)
	}
	return nil
}
