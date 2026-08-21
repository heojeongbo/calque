// Package cmd is calque's command tree and its composition root.
//
// buf runs this program with an **empty argv** — the parameter arrives inside
// the CodeGeneratorRequest and stdin and stdout are the protocol — so no
// subcommand means "serve the plugin". That is the opposite of the usual shape,
// where a root with subcommands requires one, and it is why the root carries an
// xli.OnRun handler instead of xli.RequireSubcommand.
//
// Everything else here is for a person at a terminal. None of it is on the path
// buf takes, and none of it reads a request.
//
// # Why the CLI lives only here
//
// calque is a build-time plugin: what it imports goes into the go.mod of every
// repository that generates with it. So xli and z are allowed in this package
// and in main, and nowhere else — gen, schema, config, ormcompat and the
// targets are the surface docs/extending.md documents, and a fork should be
// able to use them without taking a CLI framework with them.
//
// internal/policy's TestCliStaysInCmd is what enforces that, because a rule
// nothing checks is a preference.
package cmd

import (
	"github.com/lesomnus/xli"

	"github.com/heojeongbo/calque/gen"
)

// NewCmdRoot builds the command tree around a registry.
//
// The registry is a parameter rather than something read from a context: it is
// decided at composition time, in main, and a lookup for a value known there
// buys nothing but a Must that panics.
//
// It is also how a fork extends calque without forking. Adding a target is a
// line in your own main:
//
//	cmd.NewCmdRoot(cmd.Registry().Target(mylang.New()).Backend(mystore.New()))
func NewCmdRoot(r *gen.Registry) *xli.Command {
	return &xli.Command{
		Name:  "calque",
		Brief: "generate ORM code from orm-annotated .proto files",
		Synop: "A protoc plugin. buf runs it; with no subcommand it reads a\n" +
			"CodeGeneratorRequest on stdin and writes the response on stdout.\n" +
			"The subcommands are for a person, and none of them reads a request.",

		Commands: xli.Commands{
			NewCmdVersion(),
			NewCmdTargets(r),
		},

		// OnRun and not OnRunPass: it fires only when this command is the leaf,
		// which is exactly "buf invoked us with nothing".
		Handler: xli.OnRun(serve(r)),
	}
}
