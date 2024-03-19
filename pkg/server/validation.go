package server

import (
	"fmt"
	"strings"

	"github.com/gptscript-ai/clicky-chats/pkg/tools"
)

// validateToolFunctionName returns an error if the given function isn't valid.
func validateToolFunctionName(name string) error {
	if strings.HasPrefix(name, tools.GPTScriptToolNamePrefix) {
		return fmt.Errorf("name cannot have reserved reserved prefix %q", tools.GPTScriptToolNamePrefix)
	}

	return nil
}

// validateMetadata checks if the metadata is valid, according to the OpenAI API specification.
// From the OpenAI documentation:
// Set of 16 key-value pairs that can be attached to an object.
// This can be useful for storing additional information about the object in a structured format.
// Keys can be a maximum of 64 characters long and values can be a maximum of 512 characters long.
func validateMetadata(metadata *map[string]interface{}) error {
	if metadata != nil {
		if l := len(*metadata); l > 16 {
			return NewAPIError(fmt.Sprintf("metadata length should be less than or equal to 16, has %d", l), InvalidRequestErrorType)
		}
		for key, value := range *metadata {
			if len(key) > 64 {
				return NewAPIError(fmt.Sprintf("metadata key length should be less than or equal to 64, key %s has length %d", key, len(key)), InvalidRequestErrorType)
			}

			switch value := value.(type) {
			case string:
				if len(value) > 256 {
					return NewAPIError(fmt.Sprintf("metadata value length should be less than or equal to 256, value %s has length %d", value, len(value)), InvalidRequestErrorType)
				}
			default:
				return NewAPIError(fmt.Sprintf("metadata value should be a string, has %T", value), InvalidRequestErrorType)
			}
		}
	}

	return nil
}
