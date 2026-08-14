// Package entname is ent's identifier naming, copied.
//
// ent turns a schema field name into a Go identifier with its own rules, and
// those rules disagree with protoc-gen-go's on exactly the initialisms:
// `device_id` is `DeviceID` to ent and `DeviceId` to protoc-gen-go. A generator
// emitting both has to know both, and getting one wrong is a compile error
// naming a symbol that looks almost right.
//
// The list is copied from entgo.io/ent's entc/gen/func.go rather than imported,
// because that package is internal to ent's codegen. TestAcronymsMatchEnt reads
// the pinned module in the build cache and fails when the two diverge, so an
// ent upgrade that adds an acronym is a named test failure rather than a
// mysterious undefined field.
package entname

import (
	"strings"
	"unicode"
)

// Acronyms is ent's initialism list, from entc/gen/func.go's ruleset().
var Acronyms = []string{
	"ACL", "API", "ASCII", "AWS", "CPU", "CSS", "DNS", "EOF", "GB", "GUID",
	"HCL", "HTML", "HTTP", "HTTPS", "ID", "IP", "JSON", "KB", "LHS", "MAC",
	"MB", "QPS", "RAM", "RHS", "RPC", "SLA", "SMTP", "SQL", "SSH", "SSO",
	"TCP", "TLS", "TTL", "UDP", "UI", "UID", "URI", "URL", "UTF8", "UUID",
	"VM", "XML", "XMPP", "XSRF", "XSS",
}

var acronyms = func() map[string]struct{} {
	m := make(map[string]struct{}, len(Acronyms))
	for _, a := range Acronyms {
		m[a] = struct{}{}
	}
	return m
}()

func isSeparator(r rune) bool {
	return r == '_' || r == '-' || unicode.IsSpace(r)
}

// Pascal converts a name to PascalCase the way ent does.
//
//	user_info  => UserInfo
//	user_id    => UserID
//	device_id  => DeviceID
func Pascal(s string) string {
	return pascalWords(strings.FieldsFunc(s, isSeparator))
}

// Camel converts a name to camelCase the way ent does.
//
//	user_info  => userInfo
//	user_id    => userID
func Camel(s string) string {
	words := strings.FieldsFunc(s, isSeparator)
	if len(words) == 0 {
		return ""
	}
	if len(words) == 1 {
		return strings.ToLower(words[0])
	}
	return strings.ToLower(words[0]) + pascalWords(words[1:])
}

func pascalWords(words []string) string {
	out := make([]string, len(words))
	for i, w := range words {
		upper := strings.ToUpper(w)
		if _, ok := acronyms[upper]; ok {
			out[i] = upper
			continue
		}
		out[i] = capitalize(w)
	}
	return strings.Join(out, "")
}

// capitalize upper-cases the first rune, matching inflect's Capitalize for the
// inputs a proto field name can have: it lower-cases nothing, so an already
// mixed-case word keeps its shape.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
