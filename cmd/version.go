package cmd

import (
	"context"

	"github.com/lesomnus/xli"

	"github.com/heojeongbo/calque/cmd/version"
)

// NewCmdVersion prints which calque this is.
//
// The plugin version is the one that decides what a regeneration produces, and
// docs/versioning.md makes `git tag -n1` stand in for a changelog. Until now
// there was no way to ask a running plugin which tag it came from — the only
// evidence was the go.mod of whoever invoked it.
func NewCmdVersion() *xli.Command {
	return &xli.Command{
		Name:  "version",
		Brief: "print which calque this is",
		Handler: xli.OnRun(func(ctx context.Context, cmd *xli.Command, next xli.Next) error {
			v := version.Get()
			cmd.Printf("calque %s\n", v.Version)
			if v.Revision != "" {
				dirty := ""
				if v.Dirty {
					dirty = " (dirty)"
				}
				cmd.Printf("revision %s%s\n", v.Revision, dirty)
			}
			return next(ctx)
		}),
	}
}
