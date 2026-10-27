package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// These are local file formats, independent of the SDK and wire protocol versions.
// Additive changes keep v1 defaults; incompatible semantics need a new reader
// and migration before either constant changes.
const CurrentConfigVersion = 2
const CurrentStateVersion = 1

func unsupportedVersion(kind, field string, version, supported int) error {
	return fmt.Errorf("unsupported Agent %s %s %d (supported: %d); use an Agent that supports this format", kind, field, version, supported)
}

// Missing version fields identify the original v1 format. Explicit null, zero,
// fractional and non-numeric values must not silently select that default.
func readFormatVersion(raw []byte, kind, field string, supported int) (int, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return 0, formatJSONError(kind, err)
	}
	if object == nil {
		return 0, fmt.Errorf("Agent %s must contain one JSON object", kind)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return 0, fmt.Errorf("Agent %s must contain one JSON object", kind)
	}
	for name := range object {
		if name != field && strings.EqualFold(name, field) {
			return 0, fmt.Errorf("Agent %s version field must be named %q", kind, field)
		}
	}
	version := 1
	if value, exists := object[field]; exists {
		if err := json.Unmarshal(value, &version); err != nil || version < 1 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return 0, fmt.Errorf("Agent %s %s must be a positive integer", kind, field)
		}
	}
	if version != supported && !(kind == "configuration" && version == 1) {
		return 0, unsupportedVersion(kind, field, version, supported)
	}
	return version, nil
}

// Report field names and byte offsets, never identity or credential values.
func formatJSONError(kind string, err error) error {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Errorf("invalid Agent %s JSON at byte %d", kind, syntax.Offset)
	}
	var field *json.UnmarshalTypeError
	if errors.As(err, &field) && field.Field != "" {
		return fmt.Errorf("invalid Agent %s field %q at byte %d: expected %s", kind, field.Field, field.Offset, field.Type)
	}
	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		return fmt.Errorf("invalid Agent %s: %s", kind, err)
	}
	return fmt.Errorf("invalid Agent %s JSON", kind)
}

// Normalize only in memory. Startup must not rewrite the state file; normal
// atomic persistence adds the version while keeping the existing runtime state.
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
