package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/lesomnus/xli"
	"github.com/lesomnus/z"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/plugin"
)

// serve is what happens when nobody said a subcommand, which is what buf does.
//
// The command is the IO: xli fills its ReadCloser, Writer and ErrWriter from
// the process, or from in-memory buffers under xlitest. That is what lets
// cmd/serve_test.go drive the whole plugin — request in, response out —
// without a pipe, which nothing could do while this was a call to os.Stdin in
// main.
func serve(r *gen.Registry) xli.HandlerFunc {
	return func(ctx context.Context, cmd *xli.Command, next xli.Next) error {
		// Read here rather than handing plugin.Serve the reader, because a
		// person who typed `calque` deserves a sentence instead of a
		// CodeGeneratorResponse rendered on their terminal. protoc and buf
		// always send a request; an empty stdin means there is no build.
		in, err := io.ReadAll(cmd)
		if err != nil {
			return z.Err(err, "read request")
		}
		if len(in) == 0 {
			return errors.New(
				"nothing on stdin. calque is a protoc plugin, so buf runs it:\n" +
					"\tplugins:\n" +
					"\t  - local: [go, run, github.com/heojeongbo/calque]\n" +
					"\t    out: gen\n" +
					"\t    opt: [config=calque.yaml]\n" +
					"try `calque --help` for what a person can ask it")
		}

		if err := plugin.Serve(bytes.NewReader(in), cmd, cmd.ErrWriter, r); err != nil {
			return err
		}
		return next(ctx)
	}
}
