package ormopt_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/heojeongbo/calque/internal/protoc"
)

// TestVendoredMatchesUpstream compares proto/calque/orm against the real
// upstream files in testdata/proto/_upstream, message by message and field by
// field.
//
// calque owns a copy of a contract it does not control, and a copy is only
// worth having if something says when it stops matching. Refreshing the
// vocabulary is: replace testdata/proto/_upstream, run this test, and fix what
// it names.
//
// Names are compared without their package, since differing there is the whole
// point of the copy. Numbers and types are compared exactly, since agreeing
// there is the whole point of the copy.
func TestVendoredMatchesUpstream(t *testing.T) {
	vendored := shapeOf(t, []string{"../proto"},
		"calque/orm/orm.proto",
		"calque/orm/options.proto",
		"calque/orm/index.proto",
		"calque/orm/ref.proto",
		"calque/orm/rpc.proto",
		"calque/orm/type.proto",
	)
	upstream := shapeOf(t, []string{"../testdata/proto/_upstream"},
		"orm.proto",
		"orm/options.proto",
		"orm/index.proto",
		"orm/ref.proto",
		"orm/rpc.proto",
		"orm/type.proto",
	)

	assertSameShape(t, upstream, vendored,
		"the vendored vocabulary has drifted from upstream; a difference here means a\n"+
			"proto annotated with (orm.*) decodes into something calque does not mean")
}

// TestGeneratedMatchesProto pins the committed ormopt/*.pb.go to the committed
// proto/calque/orm/*.proto.
//
// Editing a .proto and forgetting to run `go run ./tools/gen-ormopt` leaves Go
// that compiles perfectly and reads the wrong field, which is the exact failure
// calque exists to produce in other people's code.
func TestGeneratedMatchesProto(t *testing.T) {
	source := shapeOf(t, []string{"../proto"},
		"calque/orm/orm.proto",
		"calque/orm/options.proto",
		"calque/orm/index.proto",
		"calque/orm/ref.proto",
		"calque/orm/rpc.proto",
		"calque/orm/type.proto",
	)

	var linked []protoreflect.FileDescriptor
	protoregistry.GlobalFiles.RangeFilesByPackage("calque.orm", func(fd protoreflect.FileDescriptor) bool {
		linked = append(linked, fd)
		return true
	})
	require.NotEmpty(t, linked, "no calque.orm files are registered; is ormopt linked?")

	assertSameShape(t, source, shapeOfFiles(linked),
		"ormopt is stale; run `go run ./tools/gen-ormopt` and commit the result")
}

// TestGoPackageMatchesModule pins every vendored proto's go_package to this
// module's path.
//
// It is not a style check. protoc-gen-go is told `module=<this module>` and
// refuses any file whose go_package does not sit under that prefix, so a
// mismatch does not produce wrong Go -- it produces no Go at all, and only when
// someone next runs `go run ./tools/gen-ormopt`, which may be months later.
// Renaming the module and forgetting these six lines is how that happens; it
// already did once, and the module path is case-sensitive, so a rename that
// only changes capitalisation counts.
func TestGoPackageMatchesModule(t *testing.T) {
	mod, err := os.ReadFile("../go.mod")
	require.NoError(t, err)

	var module string
	for line := range strings.Lines(string(mod)) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			module = strings.TrimSpace(rest)
			break
		}
	}
	require.NotEmpty(t, module, "no module line in go.mod")

	want := module + "/ormopt"

	files, err := filepath.Glob("../proto/calque/orm/*.proto")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, path := range files {
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Contains(t, string(src), `option go_package = "`+want+`";`,
			"%s: go_package must be %q, or gen-ormopt cannot run", path, want)
	}
}

// assertSameShape compares two vocabularies key by key, so a failure names the
// message that differs instead of printing both vocabularies in full.
func assertSameShape(t *testing.T, want, got shape, why string) {
	t.Helper()

	for name, wantFields := range want.Messages {
		gotFields, ok := got.Messages[name]
		require.True(t, ok, "%s\nmessage %s is missing", why, name)
		require.Equal(t, wantFields, gotFields, "%s\nmessage %s", why, name)
	}
	for name := range got.Messages {
		require.Contains(t, want.Messages, name, "%s\nmessage %s is unexpected", why, name)
	}

	for name, wantValues := range want.Enums {
		gotValues, ok := got.Enums[name]
		require.True(t, ok, "%s\nenum %s is missing", why, name)
		require.Equal(t, wantValues, gotValues, "%s\nenum %s", why, name)
	}
	for name := range got.Enums {
		require.Contains(t, want.Enums, name, "%s\nenum %s is unexpected", why, name)
	}

	require.Equal(t, want.Extensions, got.Extensions, "%s\nextensions", why)
}

// shape is a vocabulary reduced to what has to agree: every message's fields by
// number, every enum's values by number, and every extension by number.
type shape struct {
	Messages   map[string][]string
	Enums      map[string][]string
	Extensions []string
}

func shapeOf(t *testing.T, importPaths []string, files ...string) shape {
	t.Helper()

	fds, err := protoc.Compile(t.Context(), importPaths, files...)
	require.NoError(t, err)
	return shapeOfFiles(fds)
}

func shapeOfFiles(fds []protoreflect.FileDescriptor) shape {
	s := shape{Messages: map[string][]string{}, Enums: map[string][]string{}}
	for _, fd := range fds {
		msgs := fd.Messages()
		for i := range msgs.Len() {
			m := msgs.Get(i)
			var fields []string
			fs := m.Fields()
			for j := range fs.Len() {
				f := fs.Get(j)
				fields = append(fields, fmt.Sprintf("%d %s %s %s",
					f.Number(), f.Name(), f.Kind(), local(f.Message())))
			}
			sort.Strings(fields)
			s.Messages[string(m.Name())] = fields
		}

		enums := fd.Enums()
		for i := range enums.Len() {
			e := enums.Get(i)
			var values []string
			vs := e.Values()
			for j := range vs.Len() {
				v := vs.Get(j)
				values = append(values, fmt.Sprintf("%d %s", v.Number(), v.Name()))
			}
			sort.Strings(values)
			s.Enums[string(e.Name())] = values
		}

		exts := fd.Extensions()
		for i := range exts.Len() {
			x := exts.Get(i)
			s.Extensions = append(s.Extensions, fmt.Sprintf("%d %s on %s -> %s",
				x.Number(), x.Name(), x.ContainingMessage().FullName(), local(x.Message())))
		}
	}
	sort.Strings(s.Extensions)
	return s
}

// local strips the proto package from a message reference, which is the one
// difference between the two vocabularies that is intended.
func local(m protoreflect.MessageDescriptor) string {
	if m == nil {
		return ""
	}
	full := string(m.FullName())
	if i := strings.LastIndex(full, "."); i >= 0 {
		return full[i+1:]
	}
	return full
}
