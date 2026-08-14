// Command calque turns orm-annotated .proto files into ORM code for whichever
// target languages and storage backends a calque.yaml selects.
//
// It is a protoc plugin, so buf runs it rather than a person:
//
//	plugins:
//	  - local: [go, run, github.com/HeoJeongBo/calque]
//	    out: gen
//	    opt: [config=calque.yaml]
//
// The targets and backends a given binary knows are the ones registered below,
// at composition time. There is no config-driven loading of third-party ones:
// Go's runtime plugin support is Linux-only and requires an identical toolchain
// and identical build flags, which is a worse contract than a twenty-five line
// program that imports what it wants and calls Serve.
package main

import (
	"os"

	"github.com/HeoJeongBo/calque/backend/dexie"
	"github.com/HeoJeongBo/calque/backend/entsql"
	"github.com/HeoJeongBo/calque/gen"
	"github.com/HeoJeongBo/calque/plugin"
	"github.com/HeoJeongBo/calque/target/gotarget"
	"github.com/HeoJeongBo/calque/target/ts"
)

func main() { os.Exit(plugin.Serve(os.Stdin, os.Stdout, os.Stderr, registry())) }

// registry is the composition point. Adding a target or a backend is a line
// here and nothing else.
func registry() *gen.Registry {
	return gen.NewRegistry().
		Target(ts.New()).
		Target(gotarget.New()).
		Backend(dexie.New()).
		Backend(entsql.New())
}
