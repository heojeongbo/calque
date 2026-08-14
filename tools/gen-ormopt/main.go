// Command gen-ormopt regenerates the ormopt package from proto/calque/orm.
//
// It exists so that refreshing the vendored vocabulary is `go run
// ./tools/gen-ormopt` and nothing else -- no protoc, no buf, no BSR. The
// compile happens in this process and only protoc-gen-go, pinned by go.mod's
// tool directive, is a subprocess.
//
// Usage:
//
//	go run ./tools/gen-ormopt
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/HeoJeongBo/calque/internal/protoc"
)

// sources are the vendored annotation files, named as they are imported.
var sources = []string{
	"calque/orm/orm.proto",
	"calque/orm/options.proto",
	"calque/orm/index.proto",
	"calque/orm/ref.proto",
	"calque/orm/rpc.proto",
	"calque/orm/type.proto",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen-ormopt: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	// The opaque API is what upstream's ormpb uses, and the reason to match it
	// is Has*: telling `unique: false` from an absent `unique` is the whole of
	// the presence rules, and the open API spells that as a pointer field the
	// caller can forget to check.
	param := "module=github.com/HeoJeongBo/calque,default_api_level=API_OPAQUE"

	req, err := protoc.CompileRequest(ctx, []string{"proto"}, param, sources...)
	if err != nil {
		return err
	}

	files, err := protoc.Run(ctx, []string{"go", "tool", "protoc-gen-go"}, req)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("protoc-gen-go produced nothing")
	}

	if err := protoc.WriteAll(".", files); err != nil {
		return err
	}
	for _, f := range files {
		fmt.Println(f.Name)
	}
	return nil
}
