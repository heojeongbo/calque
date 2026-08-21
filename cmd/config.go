package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/lesomnus/xli"
	"github.com/lesomnus/xli/flg"
	"github.com/lesomnus/z"

	"github.com/heojeongbo/calque/config"
	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/plugin"
)

// NewCmdConfig reads a calque.yaml and says what this build makes of it.
//
// It is gen.Run without the generating: resolve every target and backend name,
// let each claim its options, and refuse anything left over. Every error a
// build can hit before it touches a proto is reachable from here, which means
// a config can be checked without a schema, without buf, and without waiting
// for a codegen run to fail.
//
// What it prints is the effective configuration rather than the file: the
// defaults each target applied are in it, and so is anything an `--opt` said.
// That is only possible because Config.Section remembers the value its
// claimant decoded into -- this package cannot name *ts.Options and does not
// have to.
func NewCmdConfig(r *gen.Registry, environ []string) *xli.Command {
	return &xli.Command{
		Name:  "config",
		Brief: "read calque.yaml and say what this build makes of it",
		Synop: "Resolves and configures everything the file names, then prints what\n" +
			"each target and backend ended up with. Generates nothing.",

		Flags: flg.Flags{
			&flg.String{Name: "config", Alias: 'c', Brief: "path to calque.yaml"},
			&flg.Strings{Name: "opt", Alias: 'o', Brief: "an override, as <section>.<key>=<value>; repeatable"},
		},

		Handler: xli.OnRun(func(ctx context.Context, cmd *xli.Command, next xli.Next) error {
			cfg, err := loadConfig(cmd, environ)
			if err != nil {
				return err
			}

			plan, err := gen.Resolve(cfg, r)
			if err != nil {
				return err
			}

			cmd.Println(cfg.Path())
			cmd.Printf("  version %d\n", cfg.Version)
			for _, p := range plan {
				cmd.Printf("  target  %-9s %s\n", p.Config.Label(), pairing(p))
			}

			cmd.Println()
			cmd.Println("sections")
			for _, c := range cfg.Claimants() {
				b, err := yaml.Marshal(c.Options)
				if err != nil {
					return z.Err(err, "render section %s", c.Section)
				}
				cmd.Printf("  %s  (%s)\n", c.Section, source(c))
				for _, line := range splitLines(string(b)) {
					cmd.Printf("    %s\n", line)
				}
			}

			// What the environment can say, and what it did say. It is the
			// only layer with no record in the tree, so the command that
			// exists to answer "what will this build do" has to show it.
			if names := cfg.UnreadEnv(); len(names) > 0 {
				cmd.Println()
				cmd.Println("ignored")
				for _, name := range names {
					cmd.Printf("  %s  nothing answers to this\n", name)
				}
			}
			return next(ctx)
		}),
	}
}

// loadConfig finds and parses the file, and records the overrides.
//
// The path resolution is plugin.Params's, not a second implementation of it:
// what `calque config` reads has to be what a build reads, or the command is
// answering a question nobody asked.
func loadConfig(cmd *xli.Command, environ []string) (*gen.Config, error) {
	p := &plugin.Params{Overrides: map[string]string{}}
	if v, ok := flg.Get[string](cmd, "config"); ok {
		p.Config = v
	}

	path, err := p.ConfigPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := gen.ParseConfig(raw, path)
	if err != nil {
		return nil, err
	}
	cfg.ReadEnv(environ)

	opts, _ := flg.Get[[]string](cmd, "opt")
	for _, o := range opts {
		key, value, ok := strings.Cut(o, "=")
		if !ok {
			return nil, fmt.Errorf("opt %q is not key=value", o)
		}
		if err := cfg.Override(key, value); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// pairing is how an entry reads: its target, and the store it was paired with.
func pairing(p gen.Entry) string {
	if p.Backend == nil {
		return p.Target.Name() + " (storeless)"
	}
	return p.Target.Name() + " → " + p.Backend.Name()
}

// source says where a section's values came from, in the order they were
// applied, which is the question a person running this actually has.
func source(c config.Claimant) string {
	var from []string
	if c.FromFile {
		from = append(from, "file")
	}
	if len(c.FromEnv) > 0 {
		from = append(from, strings.Join(c.FromEnv, " "))
	}
	if c.Overridden {
		from = append(from, "--opt")
	}
	if len(from) == 0 {
		return "defaults only"
	}
	return strings.Join(from, ", then ")
}

func splitLines(s string) []string {
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
