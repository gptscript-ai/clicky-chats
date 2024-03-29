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

	extraCreateRunFields = openapi3.Schemas{
		"stream": {
			Value: &openapi3.Schema{
				Description: "If true, returns a stream of events that happen during the Run as server-sent events, terminating when the Run enters a terminal state with a data: [DONE] message.",
				Type:        "boolean",
				Default:     false,
				Nullable:    true,
			},
		},
	}

	extraMessagesFields = openapi3.Schemas{
		"status": {
			Value: &openapi3.Schema{
				Description: "The status of the message, which can be either in_progress, incomplete, or completed.",
				Type:        "string",
				Default:     "pending",
				Enum:        []any{"in_progress", "incomplete", "completed"},
			},
		},
		"completed_at": {
			Value: &openapi3.Schema{
				Description: "The Unix timestamp (in seconds) for when the message was completed.",
				Type:        "integer",
				Nullable:    true,
			},
		},
		"incomplete_at": {
			Value: &openapi3.Schema{
				Description: "The Unix timestamp (in seconds) for when the message was completed.",
				Type:        "integer",
				Nullable:    true,
			},
		},
		"incomplete_details": {
			Value: &openapi3.Schema{
				Description: "On an incomplete message, details about why the message is incomplete.",
				Type:        "object",
				Nullable:    true,
				Properties: openapi3.Schemas{
					"reason": {
						Value: &openapi3.Schema{
							Type: "string",
						},
					},
				},
			},
		},
	}

	extraChatStreamFields = openapi3.Schemas{
		"usage": {
			Ref: "#/components/schemas/CompletionUsage",
			//Value: &openapi3.Schema{
			//	Description: "The token usage for a chat completion request. It should only be set on the final message of a stream",
			//	Type:        "object",
			//	Nullable:    true,
			//	Required: []string{
			//		"completion_tokens",
			//		"prompt_tokens",
			//		"total_tokens",
			//	},
			//	Properties: openapi3.Schemas{
			//		"completion_tokens": {
			//			Value: &openapi3.Schema{
			//				Type:        "integer",
			//				Description: "Number of completion tokens used over the course of the chat request",
			//			},
			//		},
			//		"prompt_tokens": {
			//			Value: &openapi3.Schema{
			//				Type:        "integer",
			//				Description: "Number of prompt tokens used over the course of the run",
			//			},
			//		},
			//		"total_tokens": {
			//			Value: &openapi3.Schema{
			//				Type:        "integer",
			//				Description: "Total number of tokens used (prompt + completion)",
			//			},
			//		},
			//	},
			//},
		},
	}

	extendedAPIs = map[string]openapi3.Schemas{
		"CreateAssistantRequest":             extraAssistantFields,
		"ModifyAssistantRequest":             extraAssistantFields,
		"AssistantObject":                    extraAssistantFields,
		"CreateRunRequest":                   extraCreateRunFields,
		"SubmitToolOutputsRunRequest":        extraCreateRunFields,
		"MessageObject":                      extraMessagesFields,
		"CreateChatCompletionStreamResponse": extraChatStreamFields,
	}
)

// GetExtendedAPIs returns the extended APIs used for generating code.
func GetExtendedAPIs() map[string]openapi3.Schemas {
	return extendedAPIs
}
