package query_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/HeoJeongBo/calque/internal/protoc"
	"github.com/HeoJeongBo/calque/ormcompat"
	"github.com/HeoJeongBo/calque/query"
	"github.com/HeoJeongBo/calque/schema"
)

func parse(t *testing.T, file string) *schema.Schema {
	t.Helper()

	req, err := protoc.CompileRequest(t.Context(),
		[]string{"../testdata/proto/valid", "../testdata/proto/_upstream"},
		"", file)
	require.NoError(t, err)

	s, err := ormcompat.Parse(req)
	require.NoError(t, err)
	return s
}

func entity(t *testing.T, s *schema.Schema, name string) *schema.Entity {
	t.Helper()
	e, ok := s.Get(name)
	require.True(t, ok, "%s is missing", name)
	return e
}

// TestPlansCoverDeclaredRpcs is the guard against the predecessor's central
// omission: it declared four operations in the service protos and generated
// one, and `implements Partial<XServiceClient>` made the gap invisible to the
// compiler.
//
// Deriving centrally means the schema decides, and this says so.
func TestPlansCoverDeclaredRpcs(t *testing.T) {
	for _, file := range []string{"apptest.proto", "erased.proto"} {
		s := parse(t, file)
		for _, e := range s.Entities() {
			t.Run(e.FullName(), func(t *testing.T) {
				set, err := query.Derive(e)
				require.NoError(t, err)

				for _, op := range e.Rpc().Ops() {
					var want query.Op
					switch op {
					case schema.OpGet:
						want = query.OpGet
					case schema.OpAdd:
						want = query.OpInsert
					case schema.OpPatch:
						want = query.OpUpdate
					case schema.OpErase:
						want = query.OpDelete
						if e.ErasesSoftly() {
							want = query.OpErase
						}
					}

					found := false
					for _, p := range set.Plans {
						if p.Op == want {
							found = true
						}
					}
					require.True(t, found,
						"%s declares %s but no plan implements it", e.FullName(), op)
				}
			})
		}
	}
}

// TestGetPlanPerCandidateKey: every way a row can be named is a way it can be
// asked for.
func TestGetPlanPerCandidateKey(t *testing.T) {
	s := parse(t, "apptest.proto")
	set, err := query.Derive(entity(t, s, "apptest.User"))
	require.NoError(t, err)

	require.Equal(t, []string{"get", "getBySlug", "add", "patch", "erase"}, set.IDs())

	byKey, ok := set.Plan("get")
	require.True(t, ok)
	require.Equal(t, query.LookupPrimary, byKey.By.Kind)
	require.Len(t, byKey.Args, 1)
	require.Equal(t, []schema.ValueName{"ref", "key", "value"}, byKey.Args[0].Path)

	// The composite lookup takes one argument per member, in index order, and
	// the edge member contributes its target's key rather than the target.
	slug, ok := set.Plan("getBySlug")
	require.True(t, ok)
	require.Equal(t, query.LookupIndex, slug.By.Kind)
	require.Equal(t, "slug", slug.By.Index)
	require.Len(t, slug.Args, 2)
	require.Equal(t, "alias", slug.Args[0].Name)
	require.Equal(t, "tenant", slug.Args[1].Name)
	require.Equal(t, []schema.ValueName{"ref", "key", "value", "tenant", "id"}, slug.Args[1].Path,
		"an edge is looked up by the row it points at")
}

// TestUniqueEdgeLookup is the case protoc-gen-orm-ts still crashes on: its
// get() emitter has `default: panic("unimplemented: key type not Field")` and a
// unique edge used as a candidate key reaches it.
//
// Here it is a plan like any other, because dispatch goes through
// schema.VisitElem and the edge arm is a method that had to be written.
func TestUniqueEdgeLookup(t *testing.T) {
	s := parse(t, "erased.proto")
	set, err := query.Derive(entity(t, s, "erasedtest.Credential"))
	require.NoError(t, err)

	byHolder, ok := set.Plan("getByHolder")
	require.True(t, ok, "a unique edge is a candidate key, so it is a lookup")
	require.Equal(t, query.LookupIndex, byHolder.By.Kind)
	require.Len(t, byHolder.Args, 1)
	require.Equal(t, []schema.ValueName{"ref", "key", "value", "holder", "id"}, byHolder.Args[0].Path)
}

// TestSoftDeleteReachesEveryRead: "every read path" is a promise no per-call
// site keeps, so the core sets it once.
func TestSoftDeleteReachesEveryRead(t *testing.T) {
	s := parse(t, "erased.proto")

	cred, err := query.Derive(entity(t, s, "erasedtest.Credential"))
	require.NoError(t, err)
	for _, p := range cred.Plans {
		switch p.Op {
		case query.OpGet, query.OpUpdate, query.OpErase:
			require.True(t, p.LiveOnly, "%s reads or writes live rows only", p.ID)
		}
	}

	erase, ok := cred.Plan("erase")
	require.True(t, ok)
	require.Equal(t, query.OpErase, erase.Op, "a softly-erasing entity stamps rather than removes")
	require.Len(t, erase.Set, 1)
	require.True(t, erase.Set[0].Now)
	require.Equal(t, schema.ProtoName("date_erased"), erase.Set[0].Prop.Name())

	// An entity with nothing to erase deletes, and has nothing to exclude.
	plain := parse(t, "apptest.proto")
	user, err := query.Derive(entity(t, plain, "apptest.User"))
	require.NoError(t, err)
	del, ok := user.Plan("erase")
	require.True(t, ok)
	require.Equal(t, query.OpDelete, del.Op)
	require.False(t, del.LiveOnly)
}

// TestPatchExcludesWhatCannotBePatched pins the three exclusions, each for a
// different reason.
func TestPatchExcludesWhatCannotBePatched(t *testing.T) {
	s := parse(t, "erased.proto")
	set, err := query.Derive(entity(t, s, "erasedtest.Credential"))
	require.NoError(t, err)

	patch, ok := set.Plan("patch")
	require.True(t, ok)

	written := map[schema.ProtoName]bool{}
	for _, a := range patch.Set {
		written[a.Prop.Name()] = true
	}

	require.False(t, written["id"], "the key names the row; changing it is not a patch")
	require.False(t, written["secret"], "an immutable prop is absent from the patch request entirely")
	require.False(t, written["date_erased"], "a row is erased by asking to erase it")

	require.True(t, written["username"])
	require.True(t, written["note"])
	require.True(t, written["holder"])
}

// TestPatchCarriesANullCompanion: proto's implicit presence cannot tell an
// absent value from an empty one, so clearing needs its own signal.
func TestPatchCarriesANullCompanion(t *testing.T) {
	s := parse(t, "erased.proto")
	set, err := query.Derive(entity(t, s, "erasedtest.Credential"))
	require.NoError(t, err)
	patch, _ := set.Plan("patch")

	var cleared bool
	for _, a := range patch.Set {
		if a.Prop.Name() == "note" && a.Clear {
			cleared = true
		}
	}
	require.True(t, cleared, "a nullable prop needs a way to say `write null`")

	names := map[string]bool{}
	for _, a := range patch.Args {
		names[a.Name] = true
	}
	require.True(t, names["noteNull"])
	require.False(t, names["usernameNull"], "a non-nullable prop has nothing to clear")
}

// TestVersionGuard: the lock is part of the write, and it can be overridden by
// a caller that says so.
func TestVersionGuard(t *testing.T) {
	s := parse(t, "erased.proto")
	set, err := query.Derive(entity(t, s, "erasedtest.Credential"))
	require.NoError(t, err)
	patch, _ := set.Plan("patch")

	require.NotNil(t, patch.Guard, "a versioned entity locks its updates")
	require.Equal(t, schema.ProtoName("date_updated"), patch.Guard.Prop.Name())
	require.NotNil(t, patch.Guard.Force, "`_force` drops the guard")
	require.Equal(t, "dateUpdatedForce", patch.Args[*patch.Guard.Force].Name)

	// The server stamps the new version; the caller does not supply it.
	var stamped bool
	for _, a := range patch.Set {
		if a.Prop.Name() == "date_updated" && a.Now {
			stamped = true
		}
	}
	require.True(t, stamped)

	// An entity with no version has no guard at all.
	plain := parse(t, "apptest.proto")
	tenant, err := query.Derive(entity(t, plain, "apptest.Tenant"))
	require.NoError(t, err)
	tp, _ := tenant.Plan("patch")
	require.Nil(t, tp.Guard)
}

// TestAddStampsRatherThanAsks: the version is the server's, and the erased
// stamp is not a value a caller writes.
func TestAddStampsRatherThanAsks(t *testing.T) {
	s := parse(t, "erased.proto")
	set, err := query.Derive(entity(t, s, "erasedtest.Credential"))
	require.NoError(t, err)
	add, ok := set.Plan("add")
	require.True(t, ok)

	asked := map[schema.ProtoName]bool{}
	for _, a := range add.Args {
		if a.Prop != nil {
			asked[a.Prop.Name()] = true
		}
	}
	require.False(t, asked["date_updated"], "the version is stamped, not supplied")
	require.False(t, asked["date_erased"], "a new row is not erased")
	require.True(t, asked["username"])
	require.True(t, asked["secret"], "immutable is settable once, at creation")

	var stamped bool
	for _, a := range add.Set {
		if a.Prop.Name() == "date_updated" && a.Now {
			stamped = true
		}
	}
	require.True(t, stamped)
}
