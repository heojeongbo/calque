package ormcompat_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/compiler/protogen"

	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormcompat"
	"github.com/heojeongbo/calque/schema"
)

// TestParseProtogenAgreesWithParse is the whole contract of the second entry
// point: a plugin that has already built a protogen.Plugin gets the same schema
// a plugin that has only the request would get.
//
// If the two ever disagree, the one that disagrees is a bug -- and a caller
// would have no way to tell, because each looks correct on its own.
func TestParseProtogenAgreesWithParse(t *testing.T) {
	// go_package is on every file in this corpus, which is what lets protogen
	// build at all. Parse does not need it; ParseProtogen's caller does.
	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../testdata/proto/valid", "../testdata/proto/_upstream"},
		"module=example.com",
		"apptest.proto", "apptest_svc.proto")
	require.NoError(t, err)

	viaRequest, err := ormcompat.Parse(req)
	require.NoError(t, err)

	p, err := protogen.Options{}.New(req)
	require.NoError(t, err)
	viaProtogen, err := ormcompat.ParseProtogen(p)
	require.NoError(t, err)

	require.Equal(t, names(viaRequest.Entities()), names(viaProtogen.Entities()),
		"entity order comes from the request in both cases")
	require.Equal(t, names(viaRequest.Sources()), names(viaProtogen.Sources()),
		"protogen's Generate flag has to mean what file_to_generate means")

	// And the props, so that "same entities" is not just the same names.
	for i, want := range viaRequest.Entities() {
		got := viaProtogen.Entities()[i]
		require.Equal(t, propNames(want), propNames(got), want.FullName())
		require.Equal(t, want.Key().Name(), got.Key().Name())
		require.Equal(t, len(want.Indexes()), len(got.Indexes()))
	}
}

// TestParseProtogenReportsTheSameRefusals: the validation is shared, so a schema
// Parse rejects has to be rejected here too, with the same message.
func TestParseProtogenReportsTheSameRefusals(t *testing.T) {
	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../testdata/proto/invalid", "../testdata/proto/_upstream"},
		// The invalid corpus carries no go_package -- it is there to be refused
		// by the annotation reader, which does not need one. protogen does, so
		// the path is supplied here rather than written into the fixture, where
		// it would only serve this one test.
		"module=example.com,Mno_key.proto=example.com/invalid", "no_key.proto")
	require.NoError(t, err)

	_, wantErr := ormcompat.Parse(req)
	require.Error(t, wantErr)

	p, err := protogen.Options{}.New(req)
	require.NoError(t, err)
	_, gotErr := ormcompat.ParseProtogen(p)
	require.Error(t, gotErr)

	require.Equal(t, wantErr.Error(), gotErr.Error())
}

func names(es []*schema.Entity) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.FullName()
	}
	return out
}

func propNames(e *schema.Entity) []string {
	out := make([]string, 0, len(e.Props()))
	for _, p := range e.Props() {
		out = append(out, string(p.Name()))
	}
	return out
}
