package cmd

import (
	"context"
	"strings"

	"github.com/lesomnus/xli"

	"github.com/heojeongbo/calque/gen"
)

// NewCmdTargets prints what this binary was composed with.
//
// The registry is a compile-time fact with no observable form: until now the
// only way to learn it was to misspell a target and read the error
// Registry.LookupTarget produces, which happens after a build has already
// failed. For a fork that added one, this is how you confirm the composition
// took.
//
// It also answers the question the config file cannot: which targets are
// storeless, and therefore which entries take no `backend`.
func NewCmdTargets(r *gen.Registry) *xli.Command {
	return &xli.Command{
		Name:  "targets",
		Brief: "the targets and backends this build knows",
		Handler: xli.OnRun(func(ctx context.Context, cmd *xli.Command, next xli.Next) error {
			// A registry with two things under one name is a main written
			// wrong, and every line below would be describing a build that
			// cannot run.
			if err := r.Err(); err != nil {
				return err
			}

			for _, name := range r.TargetNames() {
				t, err := r.LookupTarget(name)
				if err != nil {
					return err
				}
				cmd.Printf("target   %-9s %s\n", name, accepts(t))
			}
			for _, name := range r.BackendNames() {
				cmd.Printf("backend  %s\n", name)
			}
			cmd.Println()
			cmd.Println("A target's own name is also the key of its options section in calque.yaml.")
			return next(ctx)
		}),
	}
}

// accepts is what a target's `backend:` may say. See gen.Target.Backends.
func accepts(t gen.Target) string {
	if _, ok := t.(gen.Storeless); ok {
		return "storeless -- takes no backend"
	}
	if b := t.Backends(); b != nil {
		return strings.Join(b, ", ")
	}
	return "any backend"
}
