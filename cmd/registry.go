package cmd

import (
	"github.com/heojeongbo/calque/backend/dexie"
	"github.com/heojeongbo/calque/backend/entsql"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/target/gotarget"
	"github.com/heojeongbo/calque/target/service"
	"github.com/heojeongbo/calque/target/ts"
)

// Registry is the composition point: the targets and backends this binary
// knows. Adding one is a line here and nothing else.
//
// There is no config-driven loading of third-party targets or backends. Go's
// runtime plugin support is Linux-only and requires an identical toolchain and
// identical build flags, which is a worse contract than a main that imports
// what it wants. Fork and add a line, or keep your own main and pass the
// result of this to NewCmdRoot with your own entries chained on.
func Registry() *gen.Registry {
	return gen.NewRegistry().
		Target(ts.New()).
		Target(gotarget.New()).
		Target(service.New()).
		Backend(dexie.New()).
		Backend(entsql.New())
}
