package inspect

import holonmetav1 "github.com/organic-programming/grace-op/gen/go/holonmeta/v1"

// FromDescribeResponse normalizes a HolonMeta.Describe response into the
// offline inspection document shape used by op inspect.
func FromDescribeResponse(response *holonmetav1.DescribeResponse) *Document {
	if response == nil {
		return &Document{}
	}

	services := make([]Service, 0, len(response.GetServices()))
	for _, service := range response.GetServices() {
		methods := make([]Method, 0, len(service.GetMethods()))
		for _, method := range service.GetMethods() {
			methods = append(methods, Method{
				Name:            method.GetName(),
				Description:     method.GetDescription(),
				InputType:       method.GetInputType(),
				OutputType:      method.GetOutputType(),
				InputFields:     fromDescribeFields(method.GetInputFields()),
				OutputFields:    fromDescribeFields(method.GetOutputFields()),
				ClientStreaming: method.GetClientStreaming(),
				ServerStreaming: method.GetServerStreaming(),
				ExampleInput:    method.GetExampleInput(),
			})
		}

		services = append(services, Service{
			Name:        service.GetName(),
			Description: service.GetDescription(),
			Methods:     methods,
		})
	}

	return &Document{
		Slug:     response.GetSlug(),
		Motto:    response.GetMotto(),
		Services: services,
	}
}

func fromDescribeFields(fields []*holonmetav1.FieldDoc) []Field {
	out := make([]Field, 0, len(fields))
	for _, field := range fields {
		out = append(out, Field{
			Name:         field.GetName(),
			Type:         field.GetType(),
			Number:       field.GetNumber(),
			Description:  field.GetDescription(),
			Label:        describeFieldLabel(field.GetLabel()),
			MapKeyType:   field.GetMapKeyType(),
			MapValueType: field.GetMapValueType(),
			NestedFields: fromDescribeFields(field.GetNestedFields()),
			EnumValues:   fromDescribeEnumValues(field.GetEnumValues()),
			Required:     field.GetRequired(),
			Example:      field.GetExample(),
		})
	}
	return out
}

func fromDescribeEnumValues(values []*holonmetav1.EnumValueDoc) []EnumValue {
	out := make([]EnumValue, 0, len(values))
	for _, value := range values {
		out = append(out, EnumValue{
			Name:        value.GetName(),
			Number:      value.GetNumber(),
			Description: value.GetDescription(),
		})
	}
	return out
}

func describeFieldLabel(label holonmetav1.FieldLabel) string {
	switch label {
	case holonmetav1.FieldLabel_FIELD_LABEL_REPEATED:
		return FieldLabelRepeated
	case holonmetav1.FieldLabel_FIELD_LABEL_MAP:
		return FieldLabelMap
	case holonmetav1.FieldLabel_FIELD_LABEL_REQUIRED:
		return FieldLabelRequired
	default:
		return FieldLabelOptional
	}
}
