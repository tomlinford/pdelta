local sroto = import "sroto.libsonnet";

local messageFlagValues = {
    NOT_SET: sroto.EnumValue(1),
    EMPTY_BYTES: sroto.EnumValue(2),
    MASK: sroto.EnumValue(7),
};

local fieldFlagValues = messageFlagValues {
    DEFAULT_VARINT: sroto.EnumValue(3),
    DEFAULT_FIXED32: sroto.EnumValue(4),
    DEFAULT_FIXED64: sroto.EnumValue(5),
};

local bidirectionalFieldFlagValues = fieldFlagValues {
    SET_ELSEWHERE: sroto.EnumValue(6),
};

local flagEnumValueName(new_or_old, enum_value_name) =
    local format =
        if enum_value_name == "MASK" then "%s_FLAG_%s"
        else "%s_IS_%s";
    format % [new_or_old, enum_value_name];
local newValues(values) = {
    [flagEnumValueName("NEW", n)]: sroto.EnumValue(values[n].number)
    for n in std.objectFields(values)
};
local oldValues(values) = {
    [flagEnumValueName("OLD", n)]: sroto.EnumValue(
        values[n].number * std.parseOctal("10"),
    ) for n in std.objectFields(values)
};

local newAndOldValues(values) = newValues(values) + oldValues(values);

local unchangedFlag = sroto.EnumValue(std.parseOctal("100"));

local baseMessageDecls = {
    Flag: {},
    flags: sroto.Field("Flag", 1),
    fields: sroto.Field("Field", 2) {repeated: true},
};

local baseForwardMessageDecls = {
    Flag+: sroto.Enum(newValues(messageFlagValues)),
    new_bytes: sroto.BytesField(3),
};

local forwardMessageDecls = baseMessageDecls + baseForwardMessageDecls {
    fields+: {type: {name: "ForwardField"}},
};

local baseReverseMessageDecls = {
    Flag+: sroto.Enum(oldValues(messageFlagValues)),
    old_bytes: sroto.BytesField(4),
};

local reverseMessageDecls = baseMessageDecls + baseReverseMessageDecls {
    fields+: {type: {name: "ReverseField"}},
};

local messageDecls =
    baseMessageDecls + baseForwardMessageDecls + baseReverseMessageDecls;

local baseFieldDecls = baseMessageDecls {
    number: sroto.Int32Field(5),
    index: sroto.Int32Field(6),
    new_index: sroto.Int32Field(7),
    old_index: sroto.Int32Field(8),
};

local baseForwardFieldDecls = baseForwardMessageDecls {
    Flag+: sroto.Enum(newValues(fieldFlagValues) {
        UNCHANGED: unchangedFlag,
    }),
    new_varint: sroto.Uint64Field(9),
    new_fixed32: sroto.Fixed32Field(11),
    new_fixed64: sroto.Fixed64Field(13),
};

local forwardFieldDecls =
    baseFieldDecls + forwardMessageDecls + baseForwardFieldDecls;

local baseReverseFieldDecls = baseReverseMessageDecls {
    Flag+: sroto.Enum(oldValues(fieldFlagValues) {
        UNCHANGED: unchangedFlag,
    }),
    old_varint: sroto.Uint64Field(10),
    old_fixed32: sroto.Fixed32Field(12),
    old_fixed64: sroto.Fixed64Field(14),
};

local reverseFieldDecls =
    baseFieldDecls + reverseMessageDecls + baseReverseFieldDecls;

local fieldDecls =
    baseFieldDecls + baseForwardFieldDecls + baseReverseFieldDecls;

sroto.File("pdelta.proto", "pdelta", {
    Flag: sroto.Enum(bidirectionalFieldFlagValues {
        NOT_SET+: {help: "NOT_SET just means deleted."},
        EMPTY_BYTES+: {
            help: |||
                To avoid relying on proto3 optional semantics, if the
                field is set, but set to a default value, we'll note that
                as a flag here.
            |||
        },
        SET_ELSEWHERE+: {
            help: |||
                Why we need the SET_ELSEWHERE flag:
                Suppose you have a repeated field with values [f1]
                You prepend a new value f2 and get [f2, f1]
                Assume f2 is very similar to f1.
                Ideal encoding would have (note: `number` field omitted):
                f1 -> f2 mapping: {index: 0, fields: ...}
                f1 -> f1 new index mapping:
                {old_index: 0, new_index: 1, flags: UNCHANGED}
                But this makes the deletion of index 1 when reversing
                implicit, so therefore we must add:
                {index: 1, flags: OLD_IS_NOT_SET | NEW_IS_SET_ELSEWHERE}
            |||
        },
        UNCHANGED: unchangedFlag {
            help: |||
                Represents whether the field has been unchanged, which is
                only used in Field messages where new_index != old_index and
                nothing else in the field value has changed.
            |||
        },
    }) {
        help: |||
            Flags only use 7 bits, so they can be efficiently coded in a
            varint. The layout is: UOOONNN
            The 3 least significant bits are for new fields.
            The next 3 bits are for old fields.
            If set, the value will be between 1 and 6 inclusive (7 is
            currently unused).
            The most significant bit represents whether the field is
            unchanged.
        |||,
    },
    Message: sroto.Message(messageDecls {
        Flag: sroto.Enum(newAndOldValues(messageFlagValues)),
    }) {
        help: |||
            Message is a strict subset of Field but will be the type that is
            generally used by clients. A client may have a message in their
            type and a pdelta.Message and will want to apply that
            pdelta.Message to their message. Alternatively, the client may have
            two messages of their own type and will want to generate a
            pdelta.Message to get a delta between the types.

            If clients only care about applying deltas in one direction, they
            can use either ForwardMessage or ReverseMessage. These are
            wire-compatible with Message but enable a reduction in storage
            usage.

            The protocol was designed to be explicit about changes. This means
            that if when applying or reverting a delta on a particular
            message, any change to that message will have at least one bit of
            information. While this helps us to retain potentially critical
            data, this has an added benefit of letting us check if applying a
            change is invalid due to not having generated changes for that
            direction.
        |||,
    },
    Field: sroto.Message(fieldDecls {
        Flag: sroto.Enum(newAndOldValues(bidirectionalFieldFlagValues) {
            UNCHANGED: unchangedFlag {
                help: |||
                    This should only be set if new_index != old_index.
                |||
            },
            NEW_IS_NOT_SET+: {
                help: |||
                    To get a pdelta.Flag, bitwise-and field.flag with 7.
                |||,
            },
            OLD_IS_NOT_SET+: {
                help: |||
                    To get a pdelta.Flag, bitwise-and field.flag with 56
                    and bitwise-rshift by 3.
                |||,
            },
        }),

        fields+: {
            help: |||
                Fine-grained field edits. Only used if we're inside a message.
            |||
        },
        new_bytes+: {
            help: |||
                Note this can also fully overwrite messages if it's determined
                that a full overwrite would take up less space than
                fine-grained edits.
            |||,
        },
        number+: {
            help: |||
                Field number of the current field.
            |||,
        },
        index+: {
            help: |||
                If new_index == old_index, this field is referenced to
                determine the index of the current field. If the field is not
                repeated, this value will trivially be 0, but if the field is
                repeated, this refers to which index we're changing.
            |||,
        },

        new_index+: {
            help: |||
                If new_index != old_index, the ordering of the fields has
                likely shifted. Note that functionality to set these fields
                is not currently implemented.
            |||,
        },

        new_varint+: {
            help: |||
                Literal old or new values are stored in these fields. This
                is intended to match the different kinds of encoded protobuf
                types, not the different kinds of defined protobuf types. So
                for example a `double` would be stored in a `fixed64` field.
            |||,
        },
    }) {
        help: |||
            Field represents a (potentially) bidirectional delta of a specific
            field value. Each entry inside a message (including individual
            entries for repeated fields) will have its own instance of a Field.
            Note that the Field message is designed to be wire-compatible with
            all the other messages defined in this file.
        |||,
    },
    ForwardMessage: sroto.Message(forwardMessageDecls) {
        help: "See `Field` and `Message` for documentation.",
    },
    ForwardField: sroto.Message(forwardFieldDecls) {
        help: "See `Field` for documentation.",
    },
    ReverseMessage: sroto.Message(reverseMessageDecls) {
        help: "See `Field` and `Message` for documentation.",
    },
    ReverseField: sroto.Message(reverseFieldDecls) {
        help: "See `Field` for documentation.",
    },
}) {options+: [{go_package: "github.com/tomlinford/pdelta/pdeltapb"}]}
