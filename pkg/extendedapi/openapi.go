package extendedapi

import (
	"github.com/acorn-io/z"
	"github.com/getkin/kin-openapi/openapi3"
)

var (
	extraAssistantFields = openapi3.Schemas{
		"gptscript_tools": {
			Value: &openapi3.Schema{
				Description: "A list of gptscript tools available to the assistant.",
				Type:        "array",
				Default:     []string{},
				MaxItems:    z.Pointer[uint64](128),
				Items: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: "string",
					},
				},
			},
		},
	}

	extendedAPIs = map[string]openapi3.Schemas{
		"CreateAssistantRequest": extraAssistantFields,
		"ModifyAssistantRequest": extraAssistantFields,
		"AssistantObject":        extraAssistantFields,
	}
)

// GetExtendedAPIs returns the extended APIs used for generating code.
func GetExtendedAPIs() map[string]openapi3.Schemas {
	return extendedAPIs
}
