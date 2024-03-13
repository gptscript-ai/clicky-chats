package main

import (
	"os"
	"strings"

	"github.com/deepmap/oapi-codegen/v2/pkg/util"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gptscript-ai/clicky-chats/pkg/extendedapi"
	"github.com/invopop/yaml"
)

//go:generate go run generate.go
//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen -package openai -generate types,skip-prune -o openai/types.go ../../openapi.yaml
//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen -package openai -generate std-http-server,spec -o openai/server.go ../../openapi.yaml

func main() {
	if err := os.Remove("../../openapi.yaml"); err != nil && !strings.Contains(err.Error(), "no such file or directory") {
		panic(err)
	}

	s, err := util.LoadSwagger("https://raw.githubusercontent.com/openai/openai-openapi/6b64280c3db0082cbafa34495b9f3a3a58eb960d/openapi.yaml")
	if err != nil {
		panic(err)
	}

	// The file_ids field is not required for CreateMessageRequest, but the OpenAPI spec has minItems of 1. This doesn't make sense.
	s.Components.Schemas["CreateMessageRequest"].Value.Properties["file_ids"].Value.MinItems = 0
	// There is not "thread_id" field for a run, it is taken from the paths.
	s.Components.Schemas["CreateRunRequest"].Value.Required = []string{"assistant_id"}
	// Tools is nullable in the CreateChatCompletionRequest
	s.Components.Schemas["CreateChatCompletionRequest"].Value.Properties["tools"].Value.Nullable = true

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

			op.OperationID = "extended" + strings.ToTitle(op.OperationID[:1]) + op.OperationID[1:]
		}

		newPaths.Set("/rubra"+path, pathValue)
	}
	s.Paths = newPaths

	// Finished with OpenAI API and extensions, move on to new APIs
	newS, err := util.LoadSwagger("./rubrax.yaml")
	if err != nil {
		panic(err)
	}

	for key, component := range newS.Components.Schemas {
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

	f, err := os.Create("../../openapi.yaml")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	_, err = f.Write(b)
	if err != nil {
		panic(err)
	}
}
