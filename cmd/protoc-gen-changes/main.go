package main

import (
	"errors"
	"io/ioutil"
	"os"

	pgs "github.com/lyft/protoc-gen-star"
	"github.com/tomlinford/pdelta"
	"github.com/tomlinford/pdelta/pdeltapb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"sigs.k8s.io/yaml"
)

func main() {
	pgs.Init(pgs.DebugEnv("DEBUG")).
		RegisterModule(New()).
		// RegisterPostProcessor(&myPostProcessor{}).
		Render()
}

type Changelog struct {
	Entries []ChangeEntry `json:"entries"`
}

type ChangeEntry struct {
	Data        []byte `json:"data"`
	Uncommitted bool   `json:"uncommitted,omitempty"`
}

type changesModule struct {
	*pgs.ModuleBase
}

func New() pgs.Module                 { return &changesModule{&pgs.ModuleBase{}} }
func (m *changesModule) Name() string { return "changes" }

func (m *changesModule) Execute(targets map[string]pgs.File,
	packages map[string]pgs.Package) []pgs.Artifact {

	registerExtensions(targets)

	for _, f := range targets {
		m.printFile(f)
	}
	return m.Artifacts()
}

func (m *changesModule) printFile(f pgs.File) {
	m.Push(f.Name().String())
	defer m.Pop()

	if len(f.Descriptor().Extension) > 0 {
		panic(f.Descriptor().Extension[0].Extendee)
	}

	filename := f.InputPath().SetExt(".changes.yaml").String()
	changelog := &Changelog{}
	if b, err := ioutil.ReadFile(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.CheckErr(err)
	} else {
		m.CheckErr(yaml.Unmarshal(b, changelog))
	}
	newEntries := []ChangeEntry{}
	running := &descriptorpb.FileDescriptorProto{}
	for i, entry := range changelog.Entries {
		if entry.Uncommitted {
			if i < len(changelog.Entries)-1 {
				m.AddError("saw uncommitted flag too early")
				return
			}
			break
		}
		delta := &pdeltapb.Message{}
		m.CheckErr(proto.Unmarshal(entry.Data, delta))
		runningIface, err := pdelta.ApplyDelta(running, delta)
		m.CheckErr(err)
		running = runningIface.(*descriptorpb.FileDescriptorProto)
		newEntries = changelog.Entries[:i+1]
	}
	currFileDesc := proto.Clone(f.Descriptor()).(*descriptorpb.FileDescriptorProto)
	currFileDesc.SourceCodeInfo = nil
	delta, err := pdelta.GetDelta(running, currFileDesc)
	m.CheckErr(err)
	if delta == nil && len(changelog.Entries) == len(newEntries) {
		return
	}
	if delta == nil {
		if len(changelog.Entries) == len(newEntries) {
			return
		} // else need to overwrite existing changelog
	} else {
		data, err := proto.Marshal(delta)
		m.CheckErr(err)
		newEntries = append(newEntries, ChangeEntry{Data: data, Uncommitted: true})
	}
	data, err := yaml.Marshal(Changelog{newEntries})
	m.CheckErr(err)
	// panic(fmt.Sprintf("%d, %v", len(newEntries[len(newEntries)-1].Data), delta))
	m.AddGeneratorFile(filename, string(data))
}

type pgsExtMessageDescriptor struct {
	ext pgs.Extension
}

func (m *pgsExtMessageDescriptor) FullName() string {
	return m.ext.Extendee().FullyQualifiedName()[1:]
}

func (m *pgsExtMessageDescriptor) FieldMessageDescriptor(number int32) pdelta.MessageDescriptor {
	if number == m.ext.Descriptor().GetNumber() {
		return pgsTypeToMessageDescriptor(m.ext.Type())
	}
	return nil
}

type pgsMessageDescriptor struct {
	message pgs.Message
}

func (m *pgsMessageDescriptor) FullName() string {
	return m.message.FullyQualifiedName()[1:]
}

func (m *pgsMessageDescriptor) FieldMessageDescriptor(number int32) pdelta.MessageDescriptor {
	for _, f := range m.message.Fields() {
		if f.Descriptor().GetNumber() == number {
			return pgsTypeToMessageDescriptor(f.Type())
		}
	}
	return nil
}

func pgsTypeToMessageDescriptor(typ pgs.FieldType) pdelta.MessageDescriptor {
	if typ.IsEmbed() {
		return &pgsMessageDescriptor{typ.Embed()}
	}
	if typ.IsRepeated() && typ.Element().IsEmbed() {
		return &pgsMessageDescriptor{typ.Element().Embed()}
	}
	return nil
}

func registerExtensions(targets map[string]pgs.File) {
	filesProcessed := map[pgs.FilePath]struct{}{}
	files := make([]pgs.File, 0, len(targets))
	for _, f := range targets {
		files = append(files, f)
	}
	for len(files) > 0 {
		f := files[0]
		files = files[1:]
		if _, ok := filesProcessed[f.InputPath()]; ok {
			continue
		}
		files = append(files, f.Imports()...)
		for _, ext := range f.DefinedExtensions() {
			pdelta.RegisterExtension(&pgsExtMessageDescriptor{ext})
		}
	}
}
