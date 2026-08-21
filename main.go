// Command calque turns orm-annotated .proto files into ORM code for whichever
// target languages and storage backends a calque.yaml selects.
//
// It is a protoc plugin, so buf runs it rather than a person:
//
//	plugins:
//	  - local: [go, run, github.com/heojeongbo/calque]
//	    out: gen
//	    opt: [config=calque.yaml]
//
// The command tree, the registry it is composed from, and why the CLI lives in
// one package are all in [github.com/heojeongbo/calque/cmd].
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/heojeongbo/calque/cmd"
)

func main() {
	c := cmd.NewCmdRoot(cmd.Registry())
	if err := c.Run(context.Background(), os.Args[1:]); err != nil {
		// "calque:" and not "error:", because buf interleaves a plugin's
		// stderr with its own and an unattributed line is not attributable.
		// Every other line this program writes is prefixed the same way.
		fmt.Fprintln(os.Stderr, "calque:", err)
		os.Exit(1)
	}
}
