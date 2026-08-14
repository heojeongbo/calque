// Command protoc-gen-calque turns orm-annotated .proto files into ORM code for
// whichever target languages and storage backends a calque.yaml selects.
//
// It is a protoc plugin, so it is run by buf rather than directly:
//
//	plugins:
//	  - local: [go, run, github.com/HeoJeongBo/calque]
//	    out: gen
//	    opt: [config=calque.yaml]
//
// The targets and backends a given binary knows are the ones registered here,
// at composition time. There is no config-driven loading of third-party ones:
// Go's runtime plugin support is Linux-only and requires an identical toolchain
// and identical build flags, which is a worse contract than a twenty-five line
// program that imports what it wants and calls Serve.
package main

import (
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() { os.Exit(run(os.Stdin, os.Stdout, os.Stderr)) }

// run reads a CodeGeneratorRequest and writes a CodeGeneratorResponse.
//
// A schema the user got wrong is reported as response.error and exits 0: that
// is the plugin contract, and buf prints the message and fails the build
// itself. A non-zero exit is reserved for not being able to read the request or
// write the response, which is the only failure buf cannot explain.
func run(stdin io.Reader, stdout, stderr io.Writer) int {
	in, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "protoc-gen-calque: read request: %v\n", err)
		return 1
	}

	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(in, req); err != nil {
		fmt.Fprintf(stderr, "protoc-gen-calque: parse request: %v\n", err)
		return 1
	}

	// TODO(step 14): resolve the registry and hand off to plugin.Serve.
	res := &pluginpb.CodeGeneratorResponse{
		Error: proto.String("protoc-gen-calque: no targets are registered in this build"),
	}

	out, err := proto.Marshal(res)
	if err != nil {
		fmt.Fprintf(stderr, "protoc-gen-calque: marshal response: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(out); err != nil {
		fmt.Fprintf(stderr, "protoc-gen-calque: write response: %v\n", err)
		return 1
	}
	return 0
}
