package ormopt_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/heojeongbo/calque/internal/protoc"
	"github.com/heojeongbo/calque/ormopt"
)

// TestUpstreamVocabularyDecodes is the test the whole vendoring decision rests
// on: a proto annotated with upstream's (orm.field) must decode through
// calque's calque.orm types, which are a different proto package with the same
// field numbers.
//
// If this fails, calque cannot read the protos its users already have, and
// vendoring has to be replaced by a dependency on protobuf-orm/ormpb.
func TestUpstreamVocabularyDecodes(t *testing.T) {
	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../testdata/proto/vocab", "../testdata/proto/_upstream"},
		"",
		"upstream.proto",
	)
	require.NoError(t, err)

	var account *descriptorpb.DescriptorProto
	for _, f := range req.GetProtoFile() {
		if f.GetName() != "upstream.proto" {
			continue
		}
		for _, m := range f.GetMessageType() {
			if m.GetName() == "Account" {
				account = m
			}
		}
	}
	require.NotNil(t, account, "Account is missing from the request")

	t.Run("message options", func(t *testing.T) {
		opts := proto.GetExtension(account.GetOptions(), ormopt.E_Message).(*ormopt.MessageOptions)
		require.NotNil(t, opts, "(orm.message) did not decode as calque.orm.MessageOptions")

		require.True(t, opts.GetRpc().GetCrud())
		require.Len(t, opts.GetIndexes(), 1)

		idx := opts.GetIndexes()[0]
		require.Equal(t, "slug", idx.GetName())
		require.True(t, idx.GetUnique())
		require.Len(t, idx.GetRefs(), 2)
		require.Equal(t, "alias", idx.GetRefs()[0].GetName())
		require.Equal(t, int32(4), idx.GetRefs()[0].GetNumber())
		require.Equal(t, "team", idx.GetRefs()[1].GetName())
		require.Equal(t, int32(2), idx.GetRefs()[1].GetNumber())
	})

	t.Run("field options", func(t *testing.T) {
		byName := map[string]*descriptorpb.FieldDescriptorProto{}
		for _, f := range account.GetField() {
			byName[f.GetName()] = f
		}

		id := proto.GetExtension(byName["id"].GetOptions(), ormopt.E_Field).(*ormopt.FieldOptions)
		require.NotNil(t, id)
		require.True(t, id.GetKey())
		require.Equal(t, ormopt.Type_TYPE_UUID, id.GetType())

		alias := proto.GetExtension(byName["alias"].GetOptions(), ormopt.E_Field).(*ormopt.FieldOptions)
		require.True(t, alias.GetUnique())

		// Has* is what separates "said false" from "did not say", and every
		// presence rule downstream depends on it. `name` sets only a default,
		// so unique is absent rather than false.
		name := proto.GetExtension(byName["name"].GetOptions(), ormopt.E_Field).(*ormopt.FieldOptions)
		require.False(t, name.HasUnique(), "unique must be absent, not false")
		require.True(t, name.HasDefault())

		ver := proto.GetExtension(byName["date_updated"].GetOptions(), ormopt.E_Field).(*ormopt.FieldOptions)
		require.True(t, ver.HasVersion(), "version: {} must be present even though it is empty")
	})

	t.Run("edge options", func(t *testing.T) {
		var team *descriptorpb.FieldDescriptorProto
		for _, f := range account.GetField() {
			if f.GetName() == "team" {
				team = f
			}
		}
		require.NotNil(t, team)

		// An edge with an empty body still has to arrive as a present
		// EdgeOptions: `(orm.edge) = {}` is how a field says it is a relation,
		// so "present and empty" and "absent" mean opposite things.
		require.True(t, proto.HasExtension(team.GetOptions(), ormopt.E_Edge),
			"(orm.edge) = {} must be present")
		edge := proto.GetExtension(team.GetOptions(), ormopt.E_Edge).(*ormopt.EdgeOptions)
		require.NotNil(t, edge)
		require.False(t, edge.HasUnique())
	})
}

// TestVendoredNumbersMatchUpstream pins the three extension numbers. They are
// the entire compatibility contract: a proto compiled against upstream carries
// 45001/45101/45102 and nothing else identifies these options.
func TestVendoredNumbersMatchUpstream(t *testing.T) {
	require.Equal(t, int32(45001), int32(ormopt.E_Message.TypeDescriptor().Number()))
	require.Equal(t, int32(45101), int32(ormopt.E_Field.TypeDescriptor().Number()))
	require.Equal(t, int32(45102), int32(ormopt.E_Edge.TypeDescriptor().Number()))

	require.Equal(t, "google.protobuf.MessageOptions", string(ormopt.E_Message.TypeDescriptor().ContainingMessage().FullName()))
	require.Equal(t, "google.protobuf.FieldOptions", string(ormopt.E_Field.TypeDescriptor().ContainingMessage().FullName()))
	require.Equal(t, "google.protobuf.FieldOptions", string(ormopt.E_Edge.TypeDescriptor().ContainingMessage().FullName()))
}
