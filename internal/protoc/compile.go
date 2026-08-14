// Package protoc compiles .proto sources in this process and shapes the result
// into the CodeGeneratorRequest a plugin would have been handed.
//
// It is here so that calque's tests need neither protoc nor buf: a fixture is a
// .proto file and nothing else. protobuf-orm's own tests cannot do that -- its
// fixtures are reached through generated Go, so adding one means running
// `buf generate` first -- and the cost shows up as fixtures nobody adds.
package protoc

import (
	"context"
	"fmt"
	"sort"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/protoutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// CompilerVersion is what calque reports as the compiler that produced a
// request built here.
//
// It is fixed rather than read from the protocompile module so that golden
// output never moves because a dependency was upgraded. Nothing reads it for
// meaning; it exists because the field is part of the request and an absent one
// reads as "unknown" rather than "irrelevant".
var CompilerVersion = &pluginpb.Version{
	Major:  proto.Int32(5),
	Minor:  proto.Int32(29),
	Patch:  proto.Int32(0),
	Suffix: proto.String("calque"),
}

// Compile parses and links filenames, resolving imports against importPaths and
// the well-known types.
//
// filenames are slash-separated and relative to an import path, exactly as they
// appear in an `import` statement -- not filesystem paths. A file compiled as
// "calque/orm/orm.proto" is the file that another proto importing that string
// gets, and passing an absolute path instead produces a descriptor whose name
// nothing can import.
func Compile(ctx context.Context, importPaths []string, filenames ...string) ([]protoreflect.FileDescriptor, error) {
	if len(filenames) == 0 {
		return nil, fmt.Errorf("protoc: nothing to compile")
	}

	c := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: importPaths,
		}),
		// Standard keeps the comment and location spans that let a diagnostic
		// name a line rather than a message.
		SourceInfoMode: protocompile.SourceInfoStandard,
	}

	files, err := c.Compile(ctx, filenames...)
	if err != nil {
		return nil, err
	}

	out := make([]protoreflect.FileDescriptor, 0, len(files))
	for _, f := range files {
		out = append(out, f)
	}
	return out, nil
}

// Request shapes compiled files into the request a plugin reads.
//
// roots are the files to generate; ProtoFile carries them plus their whole
// transitive import closure, dependencies first, because that is what protoc
// sends and what protodesc needs to resolve a reference without a second pass.
func Request(roots []protoreflect.FileDescriptor, param string) (*pluginpb.CodeGeneratorRequest, error) {
	seen := map[string]bool{}
	var closure []*descriptorpb.FileDescriptorProto

	// walk emits dependencies before the file that imports them, so the slice
	// is topologically ordered by construction rather than sorted afterwards --
	// a sort would have to invent an order for two files that do not depend on
	// each other, and that order would be a golden-output dependency.
	var walk func(fd protoreflect.FileDescriptor) error
	walk = func(fd protoreflect.FileDescriptor) error {
		if seen[fd.Path()] {
			return nil
		}
		seen[fd.Path()] = true

		imports := fd.Imports()
		for i := range imports.Len() {
			if err := walk(imports.Get(i).FileDescriptor); err != nil {
				return err
			}
		}

		fdp := protoutil.ProtoFromFileDescriptor(fd)
		if fdp == nil {
			return fmt.Errorf("protoc: %s: no descriptor proto", fd.Path())
		}
		closure = append(closure, fdp)
		return nil
	}

	generate := make([]string, 0, len(roots))
	for _, fd := range roots {
		if err := walk(fd); err != nil {
			return nil, err
		}
		generate = append(generate, fd.Path())
	}
	sort.Strings(generate)

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate:  generate,
		ProtoFile:       closure,
		CompilerVersion: CompilerVersion,
	}
	if param != "" {
		req.Parameter = proto.String(param)
	}

	// SourceFileDescriptors is what a plugin must read instead of ProtoFile if
	// an option it needs is ever marked RETENTION_SOURCE. The orm.* options are
	// runtime-retention today, so calque reads ProtoFile; this is populated so
	// that switching is a one-line change in the reader rather than a change
	// here, and so a test can compare the two.
	req.SourceFileDescriptors = closure

	// Round-trip through the wire, which is not a formality.
	//
	// A request built here holds custom options resolved by the compiler
	// against the files it just compiled, so an extension arrives as a
	// dynamicpb.Message typed against *that* descriptor. A request built by
	// protoc arrives as bytes and is resolved against the types linked into
	// this binary. Those two are different values, and code that works on one
	// can panic on the other -- which is precisely how calque reads a proto
	// annotated with upstream's (orm.field) through its own calque.orm types:
	// by number, on unmarshal.
	//
	// Doing it here means an in-process request is the same shape as a real
	// one, so a test cannot pass on a value production never sees.
	b, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("protoc: marshal request: %w", err)
	}
	wire := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(b, wire); err != nil {
		return nil, fmt.Errorf("protoc: reparse request: %w", err)
	}
	return wire, nil
}

// CompileRequest is Compile followed by Request, which is what a caller
// building a request from source almost always wants.
func CompileRequest(ctx context.Context, importPaths []string, param string, filenames ...string) (*pluginpb.CodeGeneratorRequest, error) {
	files, err := Compile(ctx, importPaths, filenames...)
	if err != nil {
		return nil, err
	}
	return Request(files, param)
}
