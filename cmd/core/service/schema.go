package service

import (
	"encoding/json"
	"errors"
	"fmt"
)

func ValidateSchema(schema string) error {
	if schema == "" {
		return errors.New("schema must not be empty")
	}
	var v any
	if err := json.Unmarshal([]byte(schema), &v); err != nil {
		return fmt.Errorf("schema is not valid JSON: %w", err)
	}
	switch v.(type) {
	case map[string]any, bool:
		return nil
	default:
		return fmt.Errorf("schema must be a JSON object or boolean, got %T", v)
	}
}
