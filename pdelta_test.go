package pdelta

import (
	"testing"

	"github.com/tomlinford/pdelta/pdeltapb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/apipb"
	"google.golang.org/protobuf/types/known/typepb"
)

func TestApplyDelta(t *testing.T) {
	inMethod := &apipb.Method{
		Name:             "foo",
		RequestStreaming: true,
		Options: []*typepb.Option{
			{Name: "baz"},
		},
	}
	in, err := proto.Marshal(inMethod)
	if err != nil {
		t.Fatal(err)
	}
	delta := &pdeltapb.Message{
		Fields: []*pdeltapb.Field{
			{Number: 1, NewBytes: []byte("bar"), OldBytes: []byte("foo")},
			{
				Number:   2,
				NewBytes: []byte("example.com"),
				Flags:    pdeltapb.Field_OLD_IS_NOT_SET,
			},
			{
				Number:    3,
				Flags:     pdeltapb.Field_NEW_IS_NOT_SET,
				OldVarint: 1,
			},
			{Number: 6, Fields: []*pdeltapb.Field{
				{Number: 1, NewBytes: []byte("qux"), OldBytes: []byte("baz")},
			}},
		},
	}
	out, err := applySerialized(in, delta)
	if err != nil {
		t.Fatal(err)
	}
	var outMsg apipb.Method
	if err := proto.Unmarshal(out, &outMsg); err != nil {
		t.Fatal(err)
	}
	expected := &apipb.Method{
		Name:           "bar",
		RequestTypeUrl: "example.com",
		Options:        []*typepb.Option{{Name: "qux"}},
	}
	if !proto.Equal(&outMsg, expected) {
		t.Fatal("expected ", *expected, " got ", outMsg)
	}
	outDelta, err := GetDelta(inMethod, expected)
	if err != nil {
		t.Fatal(err)
	}
	actual := &apipb.Method{}
	if err := proto.Unmarshal(outDelta.NewBytes, actual); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(expected, actual) {
		t.Fatal("expected ", expected, " got ", actual)
	}
	alwaysMakeFieldLevelDeltas = true
	defer func() {
		alwaysMakeFieldLevelDeltas = false
	}()
	outDelta, err = GetDelta(inMethod, expected)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(delta, outDelta) {
		t.Fatal("expected ", delta, " got ", outDelta)
	}
}
