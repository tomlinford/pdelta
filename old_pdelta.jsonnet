local sroto = import "sroto.libsonnet";

// pdelta enables efficient changelogs for protobuf messages.
sroto.File("pdelta/pdelta.proto", "pdelta", {
    Message: sroto.Message({
        // Either `fields` or `new_value` should be set. Need to separate
        // them because proto disallows labels (like `repeated`) in oneofs.
        // Making a distinction enables us to pick which approach results in
        // a smaller serialized delta. Generally, specifying fields has
        // non-trivial overhead, so if a message is being mostly or completely
        // overwritten we can simply store the complete copy of the new
        // message. The overhead is necessary to support granular removals
        // and rewrites of specific fields.

        // Fine-grained deltas on a per-field level
        fields: sroto.Field("Field", 1) {repeated: true},

        // Wholesale rewriting of the Message
        // Essentially an inlined BytesValue
        new_value: sroto.Oneof({
            serialized: sroto.BytesField(2),
            deleted: sroto.BoolField(3),
            // Technically setting empty=true is unnecessary since an
            // empty Message could imply that the new value is empty,
            // but being explicit is probably best.
            empty: sroto.BoolField(4),
        }),
    }),
    Field: sroto.Message({
        WireType: sroto.Enum({
            VARINT: 1,
            FIXED32: 2,
            FIXED64: 3,
            LENGTH_DELIMITED: 4,
        }),

        number: sroto.Int32Field(1),

        // Handling of repeated fields:
        // * If field is not repeated, all these will be 0. Else:
        // * If field index did not change, repeated_index will be used
        //   and (prev|next)_repeated_index will be 0.
        // * If field index did change, repeated_index will be zero and
        //   one of (prev|next)_repeated_index will be non-zero.
        // This approach should enable the most efficient encoding of
        // repeated field indexes without having to rely on proto3 optional
        // semantics.
        repeated_index: sroto.Int32Field(2),
        prev_repeated_index: sroto.Int32Field(3),
        next_repeated_index: sroto.Int32Field(4),

        // If prev_repeated_index != next_repeated_index or the value of
        // the field changed, one of the following fields should be set:
        // (note we can't use oneof because we have a repeated field).
        deleted: sroto.BoolField(5),
        // sub_fields is only used if the field is a message.
        sub_fields: sroto.Field("Field", 6) {repeated: true},
        // Only handle the 4 non-deprecated wire types
        // https://developers.google.com/protocol-buffers/docs/encoding
        varint: sroto.Uint64Field(7),
        fixed_32: sroto.Fixed32Field(8),
        fixed_64: sroto.Fixed64Field(9),
        length_delimited: sroto.BytesField(10),
        // default_value enables optional semantics: if the field is set
        // but is the default value (0 or ""), we just need to know what
        // the protobuf wire type is.
        default_value: sroto.Field("WireType", 11),
    }),
}) {options+: [
    {type: {name: "go_package"}, value: "github.com/tomlinford/sroto/pdelta"},
]}
