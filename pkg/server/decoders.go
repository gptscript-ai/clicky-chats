package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
)

// plainBodyDecoder is a custom body decoder for the text/plain content type.
// It is used to correctly decode fields of multipart/form-data bodies to the type specified by their schema.
func plainBodyDecoder(body io.Reader, _ http.Header, schema *openapi3.SchemaRef, _ openapi3filter.EncodingFn) (any, error) {
	var (
		value any
		err   error
	)
	switch schema.Value.Type {
	case "number", "integer", "boolean", "array", "object":
		dec := json.NewDecoder(body)
		dec.UseNumber()
		err = dec.Decode(&value)
	default:
		var data []byte
		data, err = io.ReadAll(body)
		value = string(data)
	}

	if err != nil {
		return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Cause: err}
	}

	return value, nil
}
