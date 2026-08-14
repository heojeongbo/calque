package protoc

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

// Generated is one file a plugin produced.
type Generated struct {
	Name string
	Body []byte
}

// Run pipes req through a protoc plugin and returns what it produced.
//
// argv names the plugin, and is run as given: `go tool protoc-gen-go` keeps the
// version pinned by go.mod rather than by whatever is on PATH, which is the
// difference between output that moves when a machine changes and output that
// moves when a commit says so.
func Run(ctx context.Context, argv []string, req *pluginpb.CodeGeneratorRequest) ([]Generated, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("protoc: no plugin to run")
	}

	in, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("protoc: marshal request: %w", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("protoc: %s: %w\n%s", argv[0], err, stderr.String())
	}

	res := &pluginpb.CodeGeneratorResponse{}
	if err := proto.Unmarshal(stdout.Bytes(), res); err != nil {
		return nil, fmt.Errorf("protoc: parse response: %w", err)
	}
	// A plugin reports a user error here and still exits 0, so an unchecked
	// response is a generation that silently produced nothing.
	if msg := res.GetError(); msg != "" {
		return nil, fmt.Errorf("protoc: %s: %s", argv[0], msg)
	}

	out := make([]Generated, 0, len(res.GetFile()))
	for _, f := range res.GetFile() {
		out = append(out, Generated{Name: f.GetName(), Body: []byte(f.GetContent())})
	}
	return out, nil
}

// WriteAll writes generated files beneath dir, creating directories as needed.
//
// A name that escapes dir is refused rather than cleaned: a plugin that asks to
// write outside the root it was given is not a path to normalize, it is a
// plugin doing something nobody asked for.
func WriteAll(dir string, files []Generated) error {
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f.Name))
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == ".." || len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator) {
			return fmt.Errorf("protoc: %s: writes outside %s", f.Name, dir)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.Body, 0o644); err != nil {
			return err
		}
	}
	return nil
}
