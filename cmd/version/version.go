// Package version answers which calque is running.
//
// It reads runtime/debug.ReadBuildInfo rather than a generated file, and that
// is a deliberate difference from the template this repository's layout comes
// from. docs/versioning.md defines the plugin version as "a git tag, and the Go
// module version", and for the documented invocation --
//
//	local: [go, run, github.com/heojeongbo/calque@v0.6.0]
//
// -- that is exactly what Main.Version holds, resolved by the module proxy.
// Writing a number into the source at release time would add a fourth number to
// a document titled "Three numbers, and they are not the same number", and it
// would be the one that can disagree with the tag buf actually ran. For a
// generator whose output is supposed to be reproducible from a version, that is
// the worst property available.
//
// A local `go build` has no module version, so it reports the VCS revision the
// toolchain stamps instead, and whether the tree was dirty.
package version

import "runtime/debug"

// Info is which calque this is.
type Info struct {
	// Version is the module version -- a tag, or "(devel)" for a local build.
	Version string
	// Revision is the VCS revision, when the toolchain stamped one.
	Revision string
	// Dirty reports whether the tree had uncommitted changes when built.
	Dirty bool
}

// Get reads the build info compiled into this binary.
//
// Everything is best-effort: a binary built in ways that carry no build info at
// all reports "unknown" rather than failing, because being unable to say which
// version you are is not a reason to refuse to generate.
func Get() Info {
	v := Info{Version: "unknown"}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	if bi.Main.Version != "" {
		v.Version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			v.Revision = s.Value
		case "vcs.modified":
			v.Dirty = s.Value == "true"
		}
	}
	return v
}
