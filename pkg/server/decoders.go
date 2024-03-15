package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
)

// plainBodyDecoder is a custom body decoder for the text/plain content type.
// It is used to correctly decode fields of multipart/form-data bodies to the type specified by their schema.
func plainBodyDecoder(body io.Reader, _ http.Header, schema *openapi3.SchemaRef, _ openapi3filter.EncodingFn) (any, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var (
		value any
		dec   = json.NewDecoder(bytes.NewBuffer(data))
	)
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		switch schema.Value.Type {
		case "number", "integer", "boolean", "array", "object":
			return nil, err
		}

		value = string(data)
	}

	return value, nil
}
