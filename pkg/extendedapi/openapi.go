package extendedapi

import (
	"github.com/acorn-io/z"
	"github.com/getkin/kin-openapi/openapi3"
)

var (
	extraAssistantFields = openapi3.Schemas{
		"tools": {
			Value: &openapi3.Schema{
				Description: "A list of tool enabled on the assistant. There can be a maximum of 128 tools per assistant. Tools can be of types `code_interpreter`, `retrieval`, `function`, or `gptscript`.",
				Type:        "array",
				Default:     []string{},
				MaxItems:    z.Pointer[uint64](128),
				Items: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						OneOf: []*openapi3.SchemaRef{
							{
								Ref: "#/components/schemas/AssistantToolsCode",
							},
							{
								Ref: "#/components/schemas/AssistantToolsRetrieval",
							},
							{
								Ref: "#/components/schemas/AssistantToolsFunction",
							},
							{
								Ref: "#/components/schemas/XAssistantToolsGPTScript",
							},
						},
					},
				},
			},
		},
	}

	extendedAPIs = map[string]openapi3.Schemas{
		"AssistantObject":        extraAssistantFields,
		"CreateAssistantRequest": extraAssistantFields,
		"ModifyAssistantRequest": extraAssistantFields,
	}
)

// GetExtendedAPIs returns the extended APIs used for generating code.
func GetExtendedAPIs() map[string]openapi3.Schemas {
	return extendedAPIs
}
