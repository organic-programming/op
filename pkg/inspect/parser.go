package inspect

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ParseProtoDir parses all .proto files under protoDir and returns a normalized
// inspection document. Identity and skills are attached by the caller.
func ParseProtoDir(protoDir string) (*Document, error) {
	absDir, err := filepath.Abs(protoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve proto dir %s: %w", protoDir, err)
	}

	relFiles, err := collectProtoFiles(absDir)
	if err != nil {
		return nil, err
	}
	if len(relFiles) == 0 {
		return nil, fmt.Errorf("no .proto files found in %s", absDir)
	}

	parser := protoparse.Parser{
		ImportPaths:               []string{absDir},
		InferImportPaths:          true,
		IncludeSourceCodeInfo:     true,
		LookupImport:              desc.LoadFileDescriptor,
		LookupImportProto:         nil,
		AllowExperimentalEditions: true,
	}
	files, err := parser.ParseFiles(relFiles...)
	if err != nil {
		return nil, fmt.Errorf("parse proto files in %s: %w", absDir, err)
	}

	inputFiles := make(map[string]struct{}, len(relFiles))
	for _, rel := range relFiles {
		inputFiles[filepath.ToSlash(rel)] = struct{}{}
	}

	builder := parserBuilder{inputFiles: inputFiles}
	return &Document{
		Services: builder.buildServices(files),
	}, nil
}

type parserBuilder struct {
	inputFiles map[string]struct{}
}

func (b parserBuilder) buildServices(files []*desc.FileDescriptor) []Service {
	out := make([]Service, 0)
	for _, file := range files {
		for _, service := range file.GetServices() {
			out = append(out, b.buildService(service))
		}
	}
	return out
}

func (b parserBuilder) buildService(service *desc.ServiceDescriptor) Service {
	meta := parseCommentBlock(sourceComments(service.GetSourceInfo()))
	methods := make([]Method, 0, len(service.GetMethods()))
	for _, method := range service.GetMethods() {
		methods = append(methods, b.buildMethod(method))
	}
	return Service{
		Name:        service.GetFullyQualifiedName(),
		Description: meta.Description,
		Methods:     methods,
	}
}

func (b parserBuilder) buildMethod(method *desc.MethodDescriptor) Method {
	meta := parseCommentBlock(sourceComments(method.GetSourceInfo()))
	return Method{
		Name:            method.GetName(),
		Description:     meta.Description,
		InputType:       method.GetInputType().GetFullyQualifiedName(),
		OutputType:      method.GetOutputType().GetFullyQualifiedName(),
		InputFields:     b.buildFields(method.GetInputType(), map[string]bool{}),
		OutputFields:    b.buildFields(method.GetOutputType(), map[string]bool{}),
		ClientStreaming: method.IsClientStreaming(),
		ServerStreaming: method.IsServerStreaming(),
		ExampleInput:    meta.Example,
	}
}

func (b parserBuilder) buildFields(message *desc.MessageDescriptor, seen map[string]bool) []Field {
	if message == nil {
		return nil
	}
	name := message.GetFullyQualifiedName()
	if seen[name] {
		return nil
	}
	nextSeen := cloneSeen(seen)
	nextSeen[name] = true

	out := make([]Field, 0, len(message.GetFields()))
	for _, field := range message.GetFields() {
		out = append(out, b.buildField(field, nextSeen))
	}
	return out
}

func (b parserBuilder) buildField(field *desc.FieldDescriptor, seen map[string]bool) Field {
	meta := parseCommentBlock(sourceComments(field.GetSourceInfo()))
	out := Field{
		Name:        field.GetName(),
		Type:        descriptorTypeName(field),
		Number:      field.GetNumber(),
		Description: meta.Description,
		Label:       fieldLabel(field),
		Required:    meta.Required,
		Example:     meta.Example,
	}

	if field.IsMap() {
		out.MapKeyType = descriptorTypeName(field.GetMapKeyType())
		out.MapValueType = descriptorTypeName(field.GetMapValueType())
		return out
	}

	if enumType := field.GetEnumType(); enumType != nil && b.shouldExpand(enumType.GetFile().GetName()) {
		out.EnumValues = buildEnumValues(enumType)
	}

	if msgType := field.GetMessageType(); msgType != nil && !msgType.IsMapEntry() && b.shouldExpand(msgType.GetFile().GetName()) {
		out.NestedFields = b.buildFields(msgType, seen)
	}

	return out
}

func buildEnumValues(enumType *desc.EnumDescriptor) []EnumValue {
	out := make([]EnumValue, 0, len(enumType.GetValues()))
	for _, value := range enumType.GetValues() {
		meta := parseCommentBlock(sourceComments(value.GetSourceInfo()))
		out = append(out, EnumValue{
			Name:        value.GetName(),
			Number:      value.GetNumber(),
			Description: meta.Description,
		})
	}
	return out
}

func (b parserBuilder) shouldExpand(fileName string) bool {
	_, ok := b.inputFiles[filepath.ToSlash(fileName)]
	return ok
}

func collectProtoFiles(dir string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".proto" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan proto dir %s: %w", dir, err)
	}
	sort.Strings(files)
	return files, nil
}

type commentMeta struct {
	Description string
	Required    bool
	Example     string
}

func parseCommentBlock(raw string) commentMeta {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	description := make([]string, 0, len(lines))
	examples := make([]string, 0, 1)
	required := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case line == "@required":
			required = true
		case strings.HasPrefix(line, "@example"):
			example := strings.TrimSpace(strings.TrimPrefix(line, "@example"))
			if example != "" {
				examples = append(examples, example)
			}
		default:
			description = append(description, line)
		}
	}

	return commentMeta{
		Description: strings.Join(description, " "),
		Required:    required,
		Example:     strings.Join(examples, "\n"),
	}
}

func sourceComments(location *descriptorpb.SourceCodeInfo_Location) string {
	if location == nil {
		return ""
	}
	if leading := strings.TrimSpace(location.GetLeadingComments()); leading != "" {
		return leading
	}
	return strings.TrimSpace(location.GetTrailingComments())
}

func cloneSeen(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func fieldLabel(field *desc.FieldDescriptor) string {
	if field.IsMap() {
		return FieldLabelMap
	}
	if field.IsRepeated() {
		return FieldLabelRepeated
	}
	if field.IsRequired() {
		return FieldLabelRequired
	}
	return FieldLabelOptional
}

func descriptorTypeName(field *desc.FieldDescriptor) string {
	if field == nil {
		return ""
	}
	if field.IsMap() {
		return fmt.Sprintf("map<%s, %s>", descriptorTypeName(field.GetMapKeyType()), descriptorTypeName(field.GetMapValueType()))
	}
	if enumType := field.GetEnumType(); enumType != nil {
		return enumType.GetFullyQualifiedName()
	}
	if msgType := field.GetMessageType(); msgType != nil {
		return msgType.GetFullyQualifiedName()
	}

	switch field.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE:
		return "double"
	case descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		return "float"
	case descriptorpb.FieldDescriptorProto_TYPE_INT64:
		return "int64"
	case descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		return "uint64"
	case descriptorpb.FieldDescriptorProto_TYPE_INT32:
		return "int32"
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		return "fixed64"
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		return "fixed32"
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		return "bool"
	case descriptorpb.FieldDescriptorProto_TYPE_STRING:
		return "string"
	case descriptorpb.FieldDescriptorProto_TYPE_GROUP:
		return "group"
	case descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return "bytes"
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32:
		return "uint32"
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
		return "sfixed32"
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		return "sfixed64"
	case descriptorpb.FieldDescriptorProto_TYPE_SINT32:
		return "sint32"
	case descriptorpb.FieldDescriptorProto_TYPE_SINT64:
		return "sint64"
	default:
		return strings.TrimPrefix(field.GetType().String(), "TYPE_")
	}
}
