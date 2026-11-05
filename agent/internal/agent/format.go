package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Current local configuration and private state formats.
const CurrentConfigVersion = 1
const CurrentStateVersion = 1

func unsupportedVersion(kind, field string, version, supported int) error {
	return fmt.Errorf("unsupported PAM Agent %s %s %d (supported: %d); use a PAM Agent that supports this format", kind, field, version, supported)
}

// Require the current explicit format version.
func readFormatVersion(raw []byte, kind, field string, supported int) (int, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return 0, formatJSONError(kind, err)
	}
	if object == nil {
		return 0, fmt.Errorf("PAM Agent %s must contain one JSON object", kind)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return 0, fmt.Errorf("PAM Agent %s must contain one JSON object", kind)
	}
	for name := range object {
		if name != field && strings.EqualFold(name, field) {
			return 0, fmt.Errorf("PAM Agent %s version field must be named %q", kind, field)
		}
	}
	version := 0
	if value, exists := object[field]; exists {
		if err := json.Unmarshal(value, &version); err != nil || version < 1 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return 0, fmt.Errorf("PAM Agent %s %s must be a positive integer", kind, field)
		}
	}
	if version == 0 {
		return 0, fmt.Errorf("PAM Agent %s requires %s", kind, field)
	}
	if version != supported {
		return 0, unsupportedVersion(kind, field, version, supported)
	}
	return version, nil
}

// Report field names and byte offsets, never identity or credential values.
func formatJSONError(kind string, err error) error {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Errorf("invalid PAM Agent %s JSON at byte %d", kind, syntax.Offset)
	}
	var field *json.UnmarshalTypeError
	if errors.As(err, &field) && field.Field != "" {
		return fmt.Errorf("invalid PAM Agent %s field %q at byte %d: expected %s", kind, field.Field, field.Offset, field.Type)
	}
	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		return fmt.Errorf("invalid PAM Agent %s: %s", kind, err)
	}
	return fmt.Errorf("invalid PAM Agent %s JSON", kind)
}

// Decode the current private state format.
func decodeState(raw []byte, state *State) error {
	version, err := readFormatVersion(raw, "state", "state_version", CurrentStateVersion)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, state); err != nil {
		return formatJSONError("state", err)
	}
	state.StateVersion = version
	return nil
}
