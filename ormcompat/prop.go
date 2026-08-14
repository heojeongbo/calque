package ormcompat

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/HeoJeongBo/calque/ormopt"
	"github.com/HeoJeongBo/calque/schema"
)

// parsed is one prop as a spec, before anything is built from it.
//
// It stays a spec because the key is made unique and immutable once the entity
// knows which prop the key is, and a built prop is immutable by design. keyed
// says the annotation asked for this one; fo is kept alongside so the entity
// can check what the annotation said rather than what has since been derived
// from it.
type parsed struct {
	isEdge bool
	field  schema.FieldSpec
	edge   schema.EdgeSpec

	keyed bool
	fo    *ormopt.FieldOptions
}

// parseProp turns one proto field into a prop, or into nothing when it opted
// out.
//
// Everything it reports goes into diags rather than stopping, so one run names
// every bad annotation in the file.
//
// Unlike upstream, this does not clone the option messages or stage
// normalizations to apply on success. It does not have to: calque builds its
// own values instead of writing back into the descriptor's options, so there is
// no shared state a half-finished parse could leave behind.
func parseProp(fd protoreflect.FieldDescriptor, diags *schema.Diagnostics) *parsed {
	at := diags.At(string(fd.Name()))

	hasField := proto.HasExtension(fd.Options(), ormopt.E_Field)
	hasEdge := proto.HasExtension(fd.Options(), ormopt.E_Edge)
	if hasField && hasEdge {
		at.Add("", `only one of "orm.field" or "orm.edge" can be specified`)
		return nil
	}

	fo, _ := proto.GetExtension(fd.Options(), ormopt.E_Field).(*ormopt.FieldOptions)
	eo, _ := proto.GetExtension(fd.Options(), ormopt.E_Edge).(*ormopt.EdgeOptions)

	if fo.GetDisabled() || eo.GetDisabled() {
		return nil
	}

	if hasEdge {
		return parseEdge(fd, eo, at)
	}
	return parseField(fd, fo, hasField, at)
}

// parseField handles both an annotated field and a bare one: a proto field that
// says nothing is a field whose type is deduced.
func parseField(
	fd protoreflect.FieldDescriptor,
	fo *ormopt.FieldOptions,
	annotated bool,
	at *schema.Diagnostics,
) *parsed {
	spec := schema.FieldSpec{
		Names: schema.Names{
			Proto: schema.ProtoName(fd.Name()),
			Value: schema.ValueName(fd.JSONName()),
		},
		Number:     int32(fd.Number()),
		List:       fd.IsList() || fd.IsMap(),
		Unique:     fo.GetUnique(),
		Immutable:  fo.GetImmutable(),
		HasDefault: fo.HasDefault(),
		Default:    fo.GetDefault(),
		Version:    fo.HasVersion(),
		Erased:     fo.HasErased(),
		Source:     fd,
	}

	// The declared type wins; otherwise the kind decides.
	var err error
	if annotated && fo.GetType() != ormopt.Type_TYPE_UNSPECIFIED {
		spec.Type, err = typeOf(fo.GetType())
	} else {
		spec.Type, err = deduceType(fd)
	}
	if err != nil {
		at.Add("", err.Error())
		return nil
	}

	if spec.Type == schema.TypeMessage {
		at.Hintf("", "a field cannot be a message type",
			"store it as json, or make it an edge with (orm.edge) if it points at an entity")
		return nil
	}

	spec.Nullable = fieldIsNullable(fd, fo, spec.Type)

	if !checkVersion(fo, spec, at) {
		return nil
	}
	if !checkErased(fo, &spec, at) {
		return nil
	}

	return &parsed{field: spec, keyed: fo.GetKey(), fo: fo}
}

// fieldIsNullable applies proto's presence rules to a field.
//
// Saying so wins. The `optional` keyword says so too. Beyond that, explicit
// presence means nullable -- except for a time, because a google.protobuf.
// Timestamp is a message and every message field has presence, so without the
// exception every timestamp in every schema would be nullable and no schema
// could say otherwise.
func fieldIsNullable(fd protoreflect.FieldDescriptor, fo *ormopt.FieldOptions, t schema.Type) bool {
	if fd.Cardinality() == protoreflect.Repeated {
		// Proto cannot tell an empty list from an absent one.
		return false
	}
	if fo.GetNullable() || fd.HasOptionalKeyword() {
		return true
	}
	if fd.HasPresence() {
		return t != schema.TypeTime
	}
	return false
}

// checkVersion enforces what an optimistic-locking field may be.
//
// The key is read from the raw annotation rather than from spec.Unique, because
// the key is *made* unique and immutable after every prop is parsed. Reading
// the derived value here would let `key: true, version: {}` pass -- and being
// immutable it would then drop out of the patch request entirely, so the lock
// would vanish from the RPC instead of failing.
func checkVersion(fo *ormopt.FieldOptions, spec schema.FieldSpec, at *schema.Diagnostics) bool {
	if !fo.HasVersion() {
		return true
	}
	switch {
	case fo.GetKey():
		at.Add("", "the version field cannot be the key")
	case fo.GetUnique(), fo.GetNullable(), fo.GetImmutable():
		at.Add("", "the version field cannot be unique, nullable or immutable")
	case spec.Type != schema.TypeTime:
		at.Addf("", "only the time type supports versioning, but this is %s", spec.Type)
	default:
		return true
	}
	return false
}

// checkErased enforces what a soft-delete stamp may be, and makes it nullable.
func checkErased(fo *ormopt.FieldOptions, spec *schema.FieldSpec, at *schema.Diagnostics) bool {
	if !fo.HasErased() {
		return true
	}
	switch {
	case fo.GetKey():
		at.Add("", "the erased field cannot be the key")
	case fo.GetUnique(), fo.GetImmutable():
		at.Add("", "the erased field cannot be unique or immutable")
	case fo.HasVersion():
		at.Add("", "the erased field cannot also be the version field")
	case spec.Type != schema.TypeTime:
		at.Addf("", "only the time type can say that a row was erased, but this is %s", spec.Type)
	case fo.HasNullable() && !fo.GetNullable():
		// Refused rather than overruled. It is the one setting that would stop
		// the whole thing working, and whoever wrote it meant something by it.
		at.Add("", "the erased field is nullable, since being null is how it says the row is still there")
	default:
		spec.Nullable = true
		return true
	}
	return false
}

func parseEdge(fd protoreflect.FieldDescriptor, eo *ormopt.EdgeOptions, at *schema.Diagnostics) *parsed {
	if fd.Kind() != protoreflect.MessageKind {
		at.Hintf("", "an edge must reference a message, but this is "+fd.Kind().String(),
			"an edge points at another entity; for a stored value use (orm.field)")
		return nil
	}
	if fd.IsMap() {
		at.Add("", "a map cannot be an edge")
		return nil
	}

	repeated := fd.Cardinality() == protoreflect.Repeated
	if eo.GetUnique() && repeated {
		at.Add("", "an edge with repeated cardinality cannot be unique")
		return nil
	}

	spec := schema.EdgeSpec{
		Names: schema.Names{
			Proto: schema.ProtoName(fd.Name()),
			Value: schema.ValueName(fd.JSONName()),
		},
		Number:     int32(fd.Number()),
		TargetName: string(fd.Message().FullName()),
		List:       repeated,
		Unique:     eo.GetUnique(),
		Nullable:   edgeIsNullable(fd, eo),
		Immutable:  eo.GetImmutable(),
		HasDefault: eo.HasDefault(),
		Default:    eo.GetDefault(),
		Source:     fd,
	}
	if eo.HasFrom() {
		spec.FromName = eo.GetFrom().GetName()
		spec.FromNumber = eo.GetFrom().GetNumber()
	}
	if eo.HasBind() {
		spec.BindName = eo.GetBind().GetName()
		spec.BindNumber = eo.GetBind().GetNumber()
	}

	return &parsed{isEdge: true, edge: spec}
}

// edgeIsNullable is deliberately shorter than the field rule.
//
// Every message field in proto has presence, so treating presence as
// nullability would make every relation optional and none ever required. An
// edge is nullable when it says so, and not otherwise.
func edgeIsNullable(fd protoreflect.FieldDescriptor, eo *ormopt.EdgeOptions) bool {
	if fd.Cardinality() == protoreflect.Repeated {
		return false
	}
	return eo.GetNullable() || fd.HasOptionalKeyword()
}
