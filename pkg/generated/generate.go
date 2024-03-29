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

	extendedAPIs := extendedapi.GetExtendedAPIs()

	newComponents := make(map[string]*openapi3.SchemaRef, len(s.Components.Schemas)*2)
	for key, existingSchema := range s.Components.Schemas {
		newComponents[key] = existingSchema

		b, err := existingSchema.MarshalJSON()
		if err != nil {
			panic(err)
		}

		newSchema := new(openapi3.Schema)
		err = newSchema.UnmarshalJSON(b)
		if err != nil {
			panic(err)
		}

		for k, v := range extendedAPIs[key] {
			newSchema.Properties[k] = v
		}

		newComponents["Extended"+key] = openapi3.NewSchemaRef("", newSchema)
	}

	s.Components.Schemas = newComponents

	newPaths := openapi3.NewPathsWithCapacity(s.Paths.Len() * 3)
	for path, existingPath := range s.Paths.Map() {
		newPaths.Set(path, existingPath)
		b, err := existingPath.MarshalJSON()
		if err != nil {
			panic(err)
		}

		pathValue := new(openapi3.PathItem)
		err = pathValue.UnmarshalJSON(b)
		if err != nil {
			panic(err)
		}

		for _, op := range pathValue.Operations() {
			for _, resp := range op.Responses.Map() {
				for t, mediaType := range resp.Value.Content {
					if strings.HasPrefix(mediaType.Schema.Ref, "#/components/schemas/") {
						mediaType.Schema.Ref = "#/components/schemas/Extended" + strings.TrimPrefix(mediaType.Schema.Ref, "#/components/schemas/")
						resp.Value.Content[t] = mediaType
					}
				}
			}

			if reqBody := op.RequestBody; reqBody != nil {
				for t, mediaType := range reqBody.Value.Content {
					if strings.HasPrefix(mediaType.Schema.Ref, "#/components/schemas/") {
						mediaType.Schema.Ref = "#/components/schemas/Extended" + strings.TrimPrefix(mediaType.Schema.Ref, "#/components/schemas/")
						reqBody.Value.Content[t] = mediaType
					}
				}
			}

			op.OperationID = "extended" + strings.ToTitle(op.OperationID[:1]) + op.OperationID[1:]
		}

		newPaths.Set("/rubra"+path, pathValue)
	}
	s.Paths = newPaths

	// Finished with OpenAI API and extensions, move on to new APIs
	newS, err := util.LoadSwagger("rubrax.yaml")
	if err != nil {
		panic(err)
	}

	for key, component := range newS.Components.Schemas {
		// This nonsense allows our extended APIs to reference types in the OpenAI API schema.
		component.Ref = strings.TrimPrefix(component.Ref, "../../openapi.yaml")
		for _, item := range component.Value.Properties {
			item.Ref = strings.TrimPrefix(item.Ref, "../../openapi.yaml")
			if item.Value != nil && item.Value.Items != nil {
				item.Value.Items.Ref = strings.TrimPrefix(item.Value.Items.Ref, "../../openapi.yaml")
			}
		}
		s.Components.Schemas[key] = component
	}
	for path, pathItem := range newS.Paths.Map() {
		s.Paths.Set(path, pathItem)
	}

	b, err := s.MarshalJSON()
	if err != nil {
		panic(err)
	}

	b, err = yaml.JSONToYAML(b)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile("../../openapi.yaml", b, 0o644)
	if err != nil {
		panic(err)
	}

	s, err = util.LoadSwaggerWithCircularReferenceCount("../../openapi.yaml", 0)
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
