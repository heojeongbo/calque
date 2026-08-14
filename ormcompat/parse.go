// Package ormcompat reads the orm.* annotations off a set of proto files and
// builds calque's schema from them.
//
// It is the only package that knows the annotation vocabulary exists.
// Everything downstream reads schema and nothing else, which is what keeps a
// change in the vocabulary from reaching a target, and a change in a target
// from needing to know how a schema was written down.
//
// It reads the *upstream* vocabulary. calque owns a copy of the option
// definitions under a different proto package, but extensions resolve by
// number, so a proto that imports "orm.proto" and writes (orm.field) is read
// here without alteration. See proto/calque/orm/orm.proto.
package ormcompat

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/HeoJeongBo/calque/ormopt"
	"github.com/HeoJeongBo/calque/schema"
)

// Parse builds the schema described by a plugin request.
//
// Every file in the request is read, not just the ones to generate: an entity
// reachable only as an edge target has to be understood for the edge to mean
// anything. Which files were asked for decides what is emitted, not what is
// modelled, and Schema.Sources is where that distinction lives.
func Parse(req *pluginpb.CodeGeneratorRequest) (*schema.Schema, error) {
	files, err := resolve(req.GetProtoFile())
	if err != nil {
		return nil, err
	}

	generate := make(map[string]bool, len(req.GetFileToGenerate()))
	for _, name := range req.GetFileToGenerate() {
		generate[name] = true
	}

	var diags schema.Diagnostics
	var entities []*schema.Entity

	// In request order, which is dependency-first, so the schema's entity order
	// is stable and golden output does not move because a file was renamed.
	for _, fdp := range req.GetProtoFile() {
		fd, err := files.FindFileByPath(fdp.GetName())
		if err != nil {
			return nil, fmt.Errorf("ormcompat: %s: %w", fdp.GetName(), err)
		}

		if err := checkVocabulary(fd, &diags); err != nil {
			return nil, err
		}

		msgs := fd.Messages()
		for i := range msgs.Len() {
			md := msgs.Get(i)
			if !isEntity(md) {
				continue
			}
			if e := parseEntity(md, generate[fd.Path()], &diags); e != nil {
				entities = append(entities, e)
			}
		}
	}

	// Report what the annotations got wrong before asking Build to make sense
	// of them: Build's diagnostics are about structure, and structure is hard
	// to talk about when the parts are wrong.
	if err := diags.Err(); err != nil {
		return nil, err
	}
	return schema.Build(entities)
}

// resolve turns the request's descriptors into a resolvable file set.
//
// It does not use protogen. protogen.Options.New fails on any file in the
// closure without a go_package, which is a Go import path -- and a proto tree
// generated only for TypeScript has no reason to carry one. protoc-gen-orm-ts
// avoided that by putting a Go import path in every TypeScript-only proto and
// then never using protogen's import handling for anything.
func resolve(fdps []*descriptorpb.FileDescriptorProto) (*protoregistry.Files, error) {
	set := &descriptorpb.FileDescriptorSet{File: fdps}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, fmt.Errorf("ormcompat: build descriptors: %w", err)
	}
	return files, nil
}

// checkVocabulary refuses an annotation carrying a field calque does not know.
//
// Unknown bytes on an option message mean the user's buf.build/orm/orm declares
// something this copy of the vocabulary does not. Dropping them silently is how
// a schema comes to say one thing and generate another, so it is an error --
// and it says which option and where, because "upgrade calque" is only useful
// advice when it names what for.
func checkVocabulary(fd protoreflect.FileDescriptor, diags *schema.Diagnostics) error {
	at := diags.At(fd.Path())

	msgs := fd.Messages()
	for i := range msgs.Len() {
		md := msgs.Get(i)

		if unknown(proto.GetExtension(md.Options(), ormopt.E_Message)) {
			at.Hintf(string(md.Name()), "(orm.message) carries a field this calque does not know",
				"the schema was written against a newer buf.build/orm/orm; upgrade calque or remove the option")
		}

		fields := md.Fields()
		for j := range fields.Len() {
			f := fields.Get(j)
			where := string(md.Name()) + "." + string(f.Name())

			if unknown(proto.GetExtension(f.Options(), ormopt.E_Field)) {
				at.Hintf(where, "(orm.field) carries a field this calque does not know",
					"the schema was written against a newer buf.build/orm/orm; upgrade calque or remove the option")
			}
			if unknown(proto.GetExtension(f.Options(), ormopt.E_Edge)) {
				at.Hintf(where, "(orm.edge) carries a field this calque does not know",
					"the schema was written against a newer buf.build/orm/orm; upgrade calque or remove the option")
			}
		}
	}
	return nil
}

// unknown reports whether a decoded option kept bytes it could not name.
func unknown(v any) bool {
	m, ok := v.(proto.Message)
	if !ok || m == nil {
		return false
	}
	r := m.ProtoReflect()
	return r.IsValid() && len(r.GetUnknown()) > 0
}
