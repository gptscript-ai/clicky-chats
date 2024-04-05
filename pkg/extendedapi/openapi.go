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

	extraRunFields = openapi3.Schemas{
		"required_action": {
			Value: &openapi3.Schema{
				Nullable:    true,
				Description: "Details on the action required to continue the run. Will be `null` if no action is required.",
				Properties: map[string]*openapi3.SchemaRef{
					"submit_tool_outputs": {
						Value: &openapi3.Schema{
							Description: "Details on the tool outputs needed for this run to continue.",
							Properties: map[string]*openapi3.SchemaRef{
								"tool_calls": {
									Value: &openapi3.Schema{
										Description: "A list of the relevant tool calls.",
										Items: &openapi3.SchemaRef{
											Ref: "#/components/schemas/RunToolCallObject",
										},
										Type: "array",
									},
								},
							},
						},
					},
					"x-confirm": {
						Value: &openapi3.Schema{
							Description: "Confirm an action can be taken.",
							Properties: map[string]*openapi3.SchemaRef{
								"action": {
									Value: &openapi3.Schema{
										Description: "The action the tool would like to take.",
										Type:        "string",
									},
								},
								"id": {
									Value: &openapi3.Schema{
										Description: "The ID of the tool call.",
										Type:        "string",
									},
								},
							},
							Type: "object",
							Required: []string{
								"action",
								"id",
							},
						},
					},
					"type": {
						Value: &openapi3.Schema{
							Description: "For now, this is either `submit_tool_outputs` or `confirm`.",
							Enum:        []any{"submit_tool_outputs", "confirm"},
							Type:        "string",
						},
					},
				},
				Required: []string{"type"},
				Type:     "object",
			},
		},
		"status": {
			Value: &openapi3.Schema{
				Description: "The status of the run, which can be either `queued`, `in_progress`, `requires_action`, `requires_confirmation`, `cancelling`, `cancelled`, `failed`, `completed`, or `expired`.",
				Type:        "string",
				Enum: []any{
					"queued",
					"in_progress",
					"requires_action",
					"requires_confirmation",
					"cancelling",
					"cancelled",
					"failed",
					"completed",
					"expired",
				},
			},
		},
	}

	extraToolCallFunctionFields = openapi3.Schemas{
		"function": {
			Value: &openapi3.Schema{
				Description: "The name of the function to call.",
				Properties: map[string]*openapi3.SchemaRef{
					"arguments": {
						Value: &openapi3.Schema{
							Description: "The arguments to pass to the function.",
							Type:        "string",
						},
					},
					"name": {
						Value: &openapi3.Schema{
							Description: "The name of the function to call.",
							Type:        "string",
						},
					},
					"output": {
						Value: &openapi3.Schema{
							Description: "The output of the function. This will be `null` if the outputs have not been [submitted](/docs/api-reference/runs/submitToolOutputs) yet.",
							Type:        "string",
							Nullable:    true,
						},
					},
					"x-confirmation": {
						Value: &openapi3.Schema{
							Description: "Whether or not the function should be called.",
							Type:        "boolean",
						},
					},
				},
				Required: []string{
					"name",
					"arguments",
					"output",
				},
				Type: "object",
			},
		},
	}

	extendedAPIs = map[string]openapi3.Schemas{
		"AssistantObject":        extraAssistantFields,
		"CreateAssistantRequest": extraAssistantFields,
		"ModifyAssistantRequest": extraAssistantFields,

		"RunObject":                             extraRunFields,
		"RunStepDetailsToolCallsFunctionObject": extraToolCallFunctionFields,
	}
)

// GetExtendedAPIs returns the extended APIs used for generating code.
func GetExtendedAPIs() map[string]openapi3.Schemas {
	return extendedAPIs
}
