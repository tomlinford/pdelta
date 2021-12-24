//go:generate srotoc --proto_out=. --go_out=paths=source_relative:pdeltapb pdelta.jsonnet

package pdelta

import (
	"bytes"
	"errors"
	"math"
	"sort"

	"github.com/tomlinford/pdelta/pdeltapb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type MessageDescriptor interface {
	FullName() string
	FieldMessageDescriptor(number int32) MessageDescriptor
}

type prefMessageDescriptor struct {
	desc protoreflect.MessageDescriptor
}

func (m *prefMessageDescriptor) FullName() string {
	return string(m.desc.FullName())
}

func (m *prefMessageDescriptor) FieldMessageDescriptor(number int32) MessageDescriptor {
	fieldDesc := m.desc.Fields().ByNumber(protoreflect.FieldNumber(number))
	if fieldDesc == nil || fieldDesc.Kind() != protoreflect.MessageKind {
		return nil
	}
	return &prefMessageDescriptor{fieldDesc.Message()}
}

var extensionMap = map[string][]MessageDescriptor{}

func RegisterExtension(desc MessageDescriptor) {
	key := desc.FullName()
	extensionMap[key] = append(extensionMap[key], desc)
}

func GetDelta(old, new proto.Message) (*pdeltapb.Message, error) {
	if old.ProtoReflect().Type() != new.ProtoReflect().Type() {
		return nil, errors.New("types are different")
	}
	oldSerialized, err := proto.Marshal(old)
	if err != nil {
		return nil, err
	}
	newSerialized, err := proto.Marshal(new)
	if err != nil {
		return nil, err
	}
	return getDelta(oldSerialized, newSerialized,
		&prefMessageDescriptor{old.ProtoReflect().Descriptor()})
}

// testing hook
var alwaysMakeFieldLevelDeltas = false

// *Message returned can be nil -- means there has been no change
func getDelta(oldSerialized, newSerialized []byte,
	descriptor MessageDescriptor) (*pdeltapb.Message, error) {

	// trivial cases
	if bytes.Equal(oldSerialized, newSerialized) {
		return nil, nil
	}
	if len(newSerialized) == 0 {
		return &pdeltapb.Message{
			Flags:    pdeltapb.Message_NEW_IS_EMPTY_BYTES,
			OldBytes: oldSerialized,
		}, nil
	}
	if len(oldSerialized) == 0 {
		return &pdeltapb.Message{
			Flags:    pdeltapb.Message_OLD_IS_EMPTY_BYTES,
			NewBytes: newSerialized,
		}, nil
	}

	old, err := parseMessage(oldSerialized)
	if err != nil {
		return nil, err
	}
	new, err := parseMessage(newSerialized)
	if err != nil {
		return nil, err
	}
	allNumbers := map[protowire.Number]struct{}{}
	for num := range old {
		allNumbers[num] = struct{}{}
	}
	for num := range new {
		allNumbers[num] = struct{}{}
	}

	fields := []*pdeltapb.Field{}
	for num := range allNumbers {
		// oldFields or newFields could be empty
		oldFields := old[num]
		newFields := new[num]
		maxLen := len(oldFields)
		if len(newFields) > maxLen {
			maxLen = len(newFields)
		}
		for i := 0; i < maxLen; i++ {
			field := &pdeltapb.Field{
				Number: int32(num),
				Index:  int32(i),
			}
			if i >= len(oldFields) {
				setNew(field, &newFields[i])
				field.Flags |= pdeltapb.Field_OLD_IS_NOT_SET
				fields = append(fields, field)
				continue
			}
			if i >= len(newFields) {
				setOld(field, &oldFields[i])
				field.Flags |= pdeltapb.Field_NEW_IS_NOT_SET
				fields = append(fields, field)
				continue
			}
			if fieldDataEqual(&oldFields[i], &newFields[i]) {
				continue
			}

			fieldMsgDesc := descriptor.FieldMessageDescriptor(int32(num))
			extensions := extensionMap[descriptor.FullName()]
			for i := 0; fieldMsgDesc == nil && i < len(extensions); i++ {
				fieldMsgDesc = extensions[i].FieldMessageDescriptor(int32(num))
			}
			if fieldMsgDesc != nil {
				if newFields[i].Type != protowire.BytesType ||
					oldFields[i].Type != protowire.BytesType {
					return nil, errors.New("invalid type for embedded message")
				}
				message, err := getDelta(oldFields[i].Bytes,
					newFields[i].Bytes, fieldMsgDesc)
				if err != nil {
					return nil, err
				}
				if message == nil {
					continue
				}
				field.Flags = pdeltapb.Field_Flag(message.Flags)
				field.Fields = message.Fields
				field.OldBytes = message.OldBytes
				field.NewBytes = message.NewBytes
				fields = append(fields, field)
				continue
			}

			setOld(field, &oldFields[i])
			setNew(field, &newFields[i])
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return nil, nil
	}
	sort.Sort(sortByNumberIndex(fields))
	messageWithFields := &pdeltapb.Message{Fields: fields}
	withFieldsSerialized, err := proto.Marshal(messageWithFields)
	if err != nil {
		// shouldn't happen
		return nil, err
	}
	// 2 1-byte headers, varint-encoded lengths for each bytes, and the bytes.
	rawLen := 2 + protowire.SizeVarint(uint64(len(oldSerialized))) +
		protowire.SizeVarint(uint64(len(newSerialized))) +
		len(oldSerialized) + len(newSerialized)
	if len(withFieldsSerialized) < rawLen || alwaysMakeFieldLevelDeltas {
		return messageWithFields, nil
	}
	return &pdeltapb.Message{
		OldBytes: oldSerialized,
		NewBytes: newSerialized,
	}, nil
}

func setOld(field *pdeltapb.Field, old *fieldData) {
	switch old.Type {
	case protowire.BytesType:
		if len(old.Bytes) == 0 {
			field.Flags |= pdeltapb.Field_OLD_IS_EMPTY_BYTES
		} else {
			field.OldBytes = old.Bytes
		}
	case protowire.VarintType:
		if old.Varint == 0 {
			field.Flags |= pdeltapb.Field_OLD_IS_DEFAULT_VARINT
		} else {
			field.OldVarint = old.Varint
		}
	case protowire.Fixed32Type:
		if old.Varint == 0 {
			field.Flags |= pdeltapb.Field_OLD_IS_DEFAULT_FIXED32
		} else {
			field.OldFixed32 = old.Fixed32
		}
	case protowire.Fixed64Type:
		if old.Varint == 0 {
			field.Flags |= pdeltapb.Field_OLD_IS_DEFAULT_FIXED64
		} else {
			field.OldFixed64 = old.Fixed64
		}
	default:
		panic("unreachable")
	}
}

func setNew(field *pdeltapb.Field, new *fieldData) {
	switch new.Type {
	case protowire.BytesType:
		if len(new.Bytes) == 0 {
			field.Flags |= pdeltapb.Field_NEW_IS_EMPTY_BYTES
		} else {
			field.NewBytes = new.Bytes
		}
	case protowire.VarintType:
		if new.Varint == 0 {
			field.Flags |= pdeltapb.Field_NEW_IS_DEFAULT_VARINT
		} else {
			field.NewVarint = new.Varint
		}
	case protowire.Fixed32Type:
		if new.Varint == 0 {
			field.Flags |= pdeltapb.Field_NEW_IS_DEFAULT_FIXED32
		} else {
			field.NewFixed32 = new.Fixed32
		}
	case protowire.Fixed64Type:
		if new.Varint == 0 {
			field.Flags |= pdeltapb.Field_NEW_IS_DEFAULT_FIXED64
		} else {
			field.NewFixed64 = new.Fixed64
		}
	default:
		panic("unreachable")
	}
}

type fieldData struct {
	Type    protowire.Type
	Bytes   []byte
	Varint  uint64
	Fixed32 uint32
	Fixed64 uint64
}

func fieldDataEqual(a, b *fieldData) bool {
	return a.Type == b.Type &&
		bytes.Equal(a.Bytes, b.Bytes) &&
		a.Varint == b.Varint &&
		a.Fixed32 == b.Fixed32 &&
		a.Fixed64 == b.Fixed64
}

type sortByNumberIndex []*pdeltapb.Field

func (a sortByNumberIndex) Len() int      { return len(a) }
func (a sortByNumberIndex) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a sortByNumberIndex) Less(i, j int) bool {
	if a[i].Number == a[j].Number {
		if a[i].Index == a[j].Index {
			if a[i].OldIndex == a[j].OldIndex {
				return a[i].NewIndex < a[j].NewIndex
			}
			return a[i].OldIndex < a[j].OldIndex
		}
		return a[i].Index < a[j].Index
	}
	return a[i].Number < a[j].Number
}

func ApplyDelta(message proto.Message, delta *pdeltapb.Message) (proto.Message, error) {
	oldSerialized, err := proto.Marshal(message)
	if err != nil {
		return nil, err
	}
	newSerialized, err := applySerialized(oldSerialized, delta)
	if err != nil {
		return nil, err
	}
	newMessage := message.ProtoReflect().New().Interface()
	if err := proto.Unmarshal(newSerialized, newMessage); err != nil {
		return nil, err
	}
	return newMessage, nil
}

func applySerialized(serialized []byte, delta *pdeltapb.Message) ([]byte, error) {
	if delta.Flags&pdeltapb.Message_NEW_FLAG_MASK == pdeltapb.Message_NEW_IS_EMPTY_BYTES {
		return []byte{}, nil
	}
	if len(delta.NewBytes) > 0 {
		return delta.NewBytes, nil
	}
	parsed, err := parseMessage(serialized)
	if err != nil {
		return nil, err
	}
	deltaFieldMap := map[protowire.Number]map[int32]*pdeltapb.Field{}
	for _, f := range delta.Fields {
		if f.OldIndex != f.NewIndex {
			return nil, errors.New("referencing other indexes currently unsupported")
		}
		num := protowire.Number(f.Number)
		if deltaFieldMap[num] == nil {
			deltaFieldMap[num] = map[int32]*pdeltapb.Field{}
		}
		// might overwrite, but should be fine since the result should be the same.
		deltaFieldMap[num][f.Index] = f
		parsed[num] = append(parsed[num])
	}
	newMessage := map[protowire.Number][]fieldData{}
	for num, fields := range parsed {
		if deltaFieldMap[num] == nil {
			newMessage[num] = fields
			continue
		}
		newFields := []fieldData{}
		deltas := deltaFieldMap[num]
		for i := 0; i < len(fields) || len(deltas) > 0; i++ {
			var fd *fieldData
			if i < len(fields) {
				fd = &fields[i]
			}
			fieldDelta := deltas[int32(i)]
			if fieldDelta == nil {
				if fd == nil {
					return []byte{}, errors.New("missing field delta")
				}
				newFields = append(newFields, *fd)
				continue
			}
			newField, err := applyField(fd, fieldDelta)
			if err != nil {
				return []byte{}, err
			}
			if newField != nil {
				newFields = append(newFields, *newField)
			}
			delete(deltas, int32(i))
		}
		newMessage[num] = newFields
	}
	// no need to check for fields leftover in deltaFieldMap since we ensured
	// set(parsed.keys()) == set(deltaFieldMap.keys())
	return marshalMessage(newMessage), nil
}

func applyField(fd *fieldData, delta *pdeltapb.Field) (*fieldData, error) {
	if delta.Flags == pdeltapb.Field_UNCHANGED {
		if fd == nil {
			return nil, errors.New("field can't be new and unchanged")
		}
		return fd, nil
	}

	switch delta.Flags & pdeltapb.Field_NEW_FLAG_MASK {
	case pdeltapb.Field_FLAG_UNSPECIFIED:
		// noop
	case pdeltapb.Field_NEW_IS_NOT_SET:
		return nil, nil
	case pdeltapb.Field_NEW_IS_EMPTY_BYTES:
		return &fieldData{Type: protowire.BytesType}, nil
	case pdeltapb.Field_NEW_IS_DEFAULT_VARINT:
		return &fieldData{Type: protowire.VarintType}, nil
	case pdeltapb.Field_NEW_IS_DEFAULT_FIXED32:
		return &fieldData{Type: protowire.Fixed32Type}, nil
	case pdeltapb.Field_NEW_IS_DEFAULT_FIXED64:
		return &fieldData{Type: protowire.Fixed64Type}, nil
	case pdeltapb.Field_NEW_IS_SET_ELSEWHERE:
		if fd == nil {
			return nil, errors.New("don't know what to set field to")
		}
		return fd, nil
	default:
		return nil, errors.New("unknown field flag")
	}

	if len(delta.NewBytes) > 0 {
		return &fieldData{
			Type:  protowire.BytesType,
			Bytes: delta.NewBytes,
		}, nil
	}
	if delta.NewVarint > 0 {
		return &fieldData{
			Type:   protowire.VarintType,
			Varint: delta.NewVarint,
		}, nil
	}
	if delta.NewFixed32 > 0 {
		return &fieldData{
			Type:    protowire.Fixed32Type,
			Fixed32: delta.NewFixed32,
		}, nil
	}
	if delta.NewFixed64 > 0 {
		return &fieldData{
			Type:    protowire.Fixed64Type,
			Fixed64: delta.NewFixed64,
		}, nil
	}
	if len(delta.Fields) > 0 {
		oldBytes := []byte{}
		if fd != nil {
			if fd.Type != protowire.BytesType {
				return nil, errors.New(
					"delta has fine-grained fields but is being applied " +
						"to a non-message",
				)
			}
			oldBytes = fd.Bytes
		}
		newBytes, err := applySerialized(oldBytes, &pdeltapb.Message{
			Fields: delta.Fields,
		})
		if err != nil {
			return nil, err
		}
		return &fieldData{
			Type:  protowire.BytesType,
			Bytes: newBytes,
		}, nil
	}

	return nil, errors.New("invalid field")
}

func parseMessage(b []byte) (map[protowire.Number][]fieldData, error) {
	fields := map[protowire.Number][]fieldData{}
	for len(b) > 0 {
		v, n := protowire.ConsumeVarint(b)
		num, typ := protowire.DecodeTag(v)
		if n < 0 || num <= 0 || v > math.MaxUint32 {
			return nil, errors.New("parsing error")
		}
		b = b[n:]

		fields[num] = append(fields[num], fieldData{Type: typ})
		fp := &fields[num][len(fields[num])-1]
		switch typ {
		case protowire.BytesType:
			v, n := protowire.ConsumeVarint(b)
			remaining := len(b) - n
			if n < 0 || remaining < 0 || uint64(remaining) < v {
				return nil, errors.New("parsing error")
			}
			b = b[n:]
			fp.Bytes = b[:v]
			b = b[v:]
		case protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return nil, errors.New("parsing error")
			}
			b = b[n:]
			fp.Varint = v
		case protowire.Fixed32Type:
			v, n := protowire.ConsumeFixed32(b)
			if n < 0 {
				return nil, errors.New("parsing error")
			}
			b = b[n:]
			fp.Fixed32 = v
		case protowire.Fixed64Type:
			v, n := protowire.ConsumeFixed64(b)
			if n < 0 {
				return nil, errors.New("parsing error")
			}
			b = b[n:]
			fp.Fixed64 = v
		case protowire.StartGroupType, protowire.EndGroupType:
			return nil, errors.New("groups are unsupported")
		default:
			panic("unreachable")
		}
	}
	return fields, nil
}

func marshalMessage(fieldMap map[protowire.Number][]fieldData) []byte {
	out := []byte{}
	for num, fields := range fieldMap {
		for _, field := range fields {
			out = protowire.AppendTag(out, num, field.Type)
			switch field.Type {
			case protowire.BytesType:
				out = protowire.AppendBytes(out, field.Bytes)
			case protowire.VarintType:
				out = protowire.AppendVarint(out, field.Varint)
			case protowire.Fixed32Type:
				out = protowire.AppendFixed32(out, field.Fixed32)
			case protowire.Fixed64Type:
				out = protowire.AppendFixed64(out, field.Fixed64)
			default:
				panic("unreachable")
			}
		}
	}
	return out
}
