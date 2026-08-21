package plugin

import (
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/heojeongbo/calque/gen"
	"github.com/heojeongbo/calque/ormcompat"
)

// Serve reads a CodeGeneratorRequest, generates, and writes the response.
//
// What it returns is the plugin contract, and it is narrower than it looks. A
// schema the user got wrong is **not** an error here: it is written into
// response.error and Serve returns nil, so the process exits 0 and buf prints
// that message and fails the build itself. An error from Serve means the
// request could not be read or the response could not be written — the only
// failure buf cannot explain on calque's behalf, and the only one worth a
// non-zero exit.
//
// It returns an error rather than an exit code because its caller is a command
// among other commands now, and one of them printing "calque: ..." while the
// others return is how two spellings of the same failure appear in one build
// log. The caller decides; see package cmd.
//
// There is no `-check` and no exit code 3. A plugin cannot read its own output
// directory (buf may be writing into an archive, and the plugin is never told
// the root), so drift is a thing `go test` checks against committed goldens,
// not something a plugin can discover about itself.
func Serve(stdin io.Reader, stdout, stderr io.Writer, r *gen.Registry) error {
	in, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}

	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(in, req); err != nil {
		return fmt.Errorf("parse request: %w", err)
	}

	res := generate(req, r, stderr)
	res.SupportedFeatures = proto.Uint64(uint64(
		pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL |
			pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS))
	res.MinimumEdition = proto.Int32(int32(descriptorpb.Edition_EDITION_PROTO2))
	res.MaximumEdition = proto.Int32(int32(descriptorpb.Edition_EDITION_MAX))

	out, err := proto.Marshal(res)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := stdout.Write(out); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

// generate does the work and turns any failure into response.error.
func generate(req *pluginpb.CodeGeneratorRequest, r *gen.Registry, stderr io.Writer) *pluginpb.CodeGeneratorResponse {
	fail := func(format string, a ...any) *pluginpb.CodeGeneratorResponse {
		return &pluginpb.CodeGeneratorResponse{Error: proto.String("calque: " + fmt.Sprintf(format, a...))}
	}

	params, err := ParseParams(req.GetParameter())
	if err != nil {
		return fail("%v", err)
	}

	path, err := params.ConfigPath()
	if err != nil {
		return fail("%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fail("%v", err)
	}
	cfg, err := gen.ParseConfig(raw, path)
	if err != nil {
		return fail("%v", err)
	}

	// Command line over file. A `<section>.<key>=` opt is how a build varies one
	// setting without a second config, and it is applied here rather than by the
	// config parser because the parser is also used by tests that never saw an
	// option string.
	for _, key := range params.OverrideKeys() {
		if err := cfg.Override(key, params.Overrides[key]); err != nil {
			return fail("%v", err)
		}
	}

	s, files, err := ormcompat.ParseFiles(req)
	if err != nil {
		return fail("%v", err)
	}

	// Progress goes to stderr because stdout is the response protocol. Warnings
	// go there too, and unlike progress they are not silenceable.
	progress := gen.NewProgress(nil)
	if !params.Quiet {
		progress = gen.NewProgress(stderr)
	}

	out, err := gen.Run(s, cfg, r,
		gen.WithDescriptors(req, files),
		gen.WithWarnings(stderr),
		gen.WithProgress(progress))
	if err != nil {
		return fail("%v", err)
	}

	res := &pluginpb.CodeGeneratorResponse{}
	for _, name := range out.Names() {
		body, _ := out.Body(name)
		res.File = append(res.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    proto.String(name),
			Content: proto.String(string(body)),
		})
	}
	if params.Manifest != "" {
		res.File = append(res.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    proto.String(params.Manifest),
			Content: proto.String(string(out.Manifest())),
		})
	}
	return res
}
