package main

import (
	"os"
	"strings"

	"github.com/deepmap/oapi-codegen/v2/pkg/codegen"
	"github.com/deepmap/oapi-codegen/v2/pkg/util"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gptscript-ai/clicky-chats/pkg/extendedapi"
	"github.com/invopop/yaml"
)

//go:generate go run generate.go

func main() {
	s, err := util.LoadSwagger("https://raw.githubusercontent.com/openai/openai-openapi/399458ce091927c74893736464d85e4ca3036d59/openapi.yaml")
	if err != nil {
		panic(err)
	}

	// The file_ids field is not required for CreateMessageRequest, but the OpenAPI spec has minItems of 1. This doesn't make sense.
	s.Components.Schemas["CreateMessageRequest"].Value.Properties["file_ids"].Value.MinItems = 0

	// There is not "thread_id" field for a run, it is taken from the paths.
	s.Components.Schemas["CreateRunRequest"].Value.Required = []string{"assistant_id"}
	s.Components.Schemas["CreateThreadAndRunRequest"].Value.Required = []string{"assistant_id"}

	// Tools is nullable in the CreateChatCompletionRequest
	s.Components.Schemas["CreateChatCompletionRequest"].Value.Properties["tools"].Value.Nullable = true
	s.Components.Schemas["FunctionObject"].Value.Properties["parameters"].Value.Nullable = true

	// Embeddings can be requested as an array of floats or a base64-encoded string, but the OpenAI API Spec doesn't support string as return type

	s.Components.Schemas["Embedding"].Value.Properties["embedding"].Value = &openapi3.Schema{
		Description: "The embedding vector, which is a list of floats or a base64 encoded string, depending on the requested return type. The length of vector depends on the model as listed in the [embedding guide](/docs/guides/embeddings).",
		OneOf: []*openapi3.SchemaRef{
			{
				Value: &openapi3.Schema{
					Type: "array",
					Items: &openapi3.SchemaRef{
						Value: &openapi3.Schema{
							Type:   "number",
							Format: "float",
						},
					},
				},
			},
			{
				Value: &openapi3.Schema{
					Type: "string",
				},
			},
		},
	}

	// Finished with OpenAI API and extensions, move on to new APIs
	newS, err := util.LoadSwagger("rubrax.yaml")
	if err != nil {
		panic(err)
	}

	// This nonsense allows our extended APIs to reference types in the OpenAI API schema.
	for key, component := range newS.Components.Schemas {
		component.Ref = strings.TrimPrefix(component.Ref, "../server/openapi.yaml")
		for _, item := range component.Value.Properties {
			item.Ref = strings.TrimPrefix(item.Ref, "../server/openapi.yaml")
			if item.Value != nil && item.Value.Items != nil {
				item.Value.Items.Ref = strings.TrimPrefix(item.Value.Items.Ref, "../server/openapi.yaml")
			}
		}
		s.Components.Schemas[key] = component
	}
	for path, pathItem := range newS.Paths.Map() {
		if pathItem.Get != nil {
			if pathItem.Get.RequestBody != nil {
				for _, val := range pathItem.Get.RequestBody.Value.Content {
					val.Schema.Ref = strings.TrimPrefix(val.Schema.Ref, "../server/openapi.yaml")
				}
			}
			if pathItem.Get.Responses != nil {
				newResponses := openapi3.NewResponsesWithCapacity(pathItem.Get.Responses.Len())
				for key, val := range pathItem.Get.Responses.Map() {
					for _, mediaType := range val.Value.Content {
						mediaType.Schema.Ref = strings.TrimPrefix(mediaType.Schema.Ref, "../server/openapi.yaml")
					}
					newResponses.Set(key, val)
				}
				pathItem.Get.Responses = newResponses
			}
		}
		s.Paths.Set(path, pathItem)
	}

	s.Servers = openapi3.Servers{
		&openapi3.Server{
			URL: "http://localhost:8080/v1",
		},
	}

	extendedAPIs := extendedapi.GetExtendedAPIs()
	for key, existingSchema := range s.Components.Schemas {
		for k, v := range extendedAPIs[key] {
			existingSchema.Value.Properties[k] = v
		}
	}

	b, err := s.MarshalJSON()
	if err != nil {
		panic(err)
	}

	b, err = yaml.JSONToYAML(b)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile("../server/openapi.yaml", b, 0o644)
	if err != nil {
		panic(err)
	}

	s, err = util.LoadSwaggerWithCircularReferenceCount("../server/openapi.yaml", 0)
	if err != nil {
		panic(err)
	}

	opts := codegen.Configuration{
		PackageName: "openai",
		Generate: codegen.GenerateOptions{
			Models: true,
		},
		OutputOptions: codegen.OutputOptions{
			SkipPrune: true,
		},
	}

	if err = opts.Validate(); err != nil {
		panic(err)
	}

	code, err := codegen.Generate(s, opts)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile("openai/types.go", []byte(code), 0o644)
	if err != nil {
		panic(err)
	}

	opts = codegen.Configuration{
		PackageName: "openai",
		Generate: codegen.GenerateOptions{
			StdHTTPServer: true,
			EmbeddedSpec:  true,
		},
	}

	if err = opts.Validate(); err != nil {
		panic(err)
	}

	code, err = codegen.Generate(s, opts)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile("openai/server.go", []byte(code), 0o644)
	if err != nil {
		panic(err)
	}
}
