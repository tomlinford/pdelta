# pdelta: Protobuf Delta

> :warning: **Project should not be considered production-ready.** APIs are also unstable.

`pdelta` is a Go package that can efficiently derive and apply deltas between protobuf messages.

While generally it can be trivial to store the differences between two different protobuf messages by just retaining the serialized version of both, this can be quite inefficient for large messages where only a small subset of fields change. `pdelta` solves this problem by supporting fine-grained field deltas with minimal overhead.

## Protobuf serialization background

Protobuf messages are serialized a set of fields, which are just key/value pairs. The "key" of the field is the field number and one of 6 field types (two of which are unsupported in proto3) encoded as a varint. The remaining four types are:
1. Length-delimited bytes fields. This is encoded as a varint length and then the raw bytes.
2. Varints (up to 8 bytes long)
3. Fixed32 fields (4 bytes long float/uint/int)
4. Fixed64 fields (8 bytes long double/uint/int)

Embedded messages are encoded in a length-delimited bytes field. Repeated fields are self-explanatory: the field number shows up multiple times. While generally the default value (`""` for length delimited fields and `0` for the int-ish field family) is not serialized, it can be either through using proto3 optional fields or empty messages. The latter can help to distinguish between null and `0`/`""` for the canonical JSON serialization format.

## pdelta encoding format

When computing a new delta between two messages, `pdelta` simply recursively visits each field and assesses if there has been any change. If there has, `pdelta` will store information about the old value and the new value. There will always be at least one bit of information. Since this package could at some point be used for critical data, it was important to be explicit about any data changes.

The data change can either be encoded in a flag (which is encoded as a varint) or directly as the data itself. The full set of fields and flags can be found in the `Field` message in `pdelta.proto`. The information stored is as follows:
1. The field number this field refers to.
2. The field index this field refers to -- this is used for repeated fields.
3. Fields for embedded messages. If this field refers to a message and that message has fine-grained edits, it can be set here.
4. Flags indicating whether a field has been deleted ("not set") or set to the default value (which includes the serialized type). These flags can be used for either the new field value or the old field value. This is unused if fine-grained field edits are set.
5. The new and old field values, if not covered by the flags and fine-grained field edits.

There's some additional complexity around repeated fields where the field has shifted from one index to another, but this functionality hasn't been implemented yet.

This coding is compact enough that the `Field` message definition has fewer than 16 fields, which means each field header for `Field` only takes one byte to encode. Additionally, there are a few different flavors of `Field`:
* `Message`. Note that since a `Field` could contain an embedded message, the functionality for `Message` is a strict subset of the functionality for `Field`. So in the implementation, a `Message` is basically just a `Field` but without the varint/fixed32/fixed64/repeated index support. `Message` is wire-compatible with `Field`.
* `Forward(Message|Field)`. Storing bidirectional data can be inefficient, so the forward messages only store information about the new messages and fields. Both are wire-compatible with `Field`. (note: functionality currently unimplemented).
* `Reverse(Message|Field)`. Ditto above, but for storing data to undo changes. Both are wire-compatible with `Field`. (note: functionality currently unimplemented).

The `pdelta.proto` file is a generated file which uses [`srotoc`](https://github.com/tomlinford/sroto). Using sroto helps a lot to generate a protobuf schema file where many of the message types are wire-compatible.

## `Message` interaction

Users of the API don't really care about how the fields are implemented, they're just working with protobuf messages. The API provides two:
1. `GetDelta` returns a `*pdeltapb.Message` given two messages of the same type.
2. `ApplyDelta` takes a `proto.Message` and `*pdeltapb.Message` and gives a new `proto.Message` with the changes applied.

In the implementation, the code works off of the serialized messages so in the future additional APIs could be exposed that operate on the serialized message to avoid serialization/de-serialization steps.

## `protoc-gen-changes` command

As sample usage and potential use case, the `protoc-gen-changes` command is provided. This implements a `protoc` extension that creates a changelog of a particular `.proto` file serialized in a yaml file. The yaml file looks like:

```yaml
entries:
  - data: "{pdelta.Message serialized and encoded in base64}"
  - data: "{pdelta.Message serialized and encoded in base64}"
    uncommitted: true
```

If that `uncommitted: true` field is set on the last entry, then further changes to the `.proto` file and reruns of `protoc-gen-changes` will change the last entry. Deleting that line will make it so further changes and reruns generate a new entry. This could be useful if you want to do something like build additional tooling to check forward compatibility of changes to your `.proto` files.

Note though that the `pdelta.Message` format is designed for compact encoding, not easy readability. So if this becomes important, it's likely that further tooling would be important to provide better human readability of `pdelta.Messages`.
