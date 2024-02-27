package server

import "fmt"

// validateMetadata checks if the metadata is valid, according to the OpenAI API specification.
// From the OpenAI documentation:
// Set of 16 key-value pairs that can be attached to an object.
// This can be useful for storing additional information about the object in a structured format.
// Keys can be a maximum of 64 characters long and values can be a maximum of 512 characters long.
func validateMetadata(metadata *map[string]interface{}) error {
	if metadata != nil {
		if l := len(*metadata); l > 16 {
			return fmt.Errorf("metadata should have 16 elements or fewer, has %d", l)
		}
		for key, value := range *metadata {
			if len(key) > 64 {
				return fmt.Errorf("metadata key length should be less than or equal to 64, key %s has length %d", key, len(key))
			}

			switch value := value.(type) {
			case string:
				if len(value) > 256 {
					return fmt.Errorf("metadata value length should be less than or equal to 256, value %s for key %s has length %d", value, key, len(value))
				}
			default:
				return fmt.Errorf("metadata value for key %s should be string", key)
			}
		}
	}

	return nil
}
