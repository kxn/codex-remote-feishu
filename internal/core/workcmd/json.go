package workcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"unicode/utf8"
)

// ValidateJSON rejects duplicate keys at every nesting level, excessive nesting,
// trailing JSON values and malformed input before any product command runs.
func ValidateJSON(data []byte) error {
	if len(data) == 0 || len(data) > MaxRequestBytes {
		return fmt.Errorf("JSON body must contain 1..%d bytes", MaxRequestBytes)
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("JSON body must be valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var consume func(int) error
	consume = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("JSON nesting exceeds 64 levels")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("invalid JSON object key")
				}
				if seen[key] {
					return fmt.Errorf("duplicate JSON field %q", key)
				}
				seen[key] = true
				if err := consume(depth + 1); err != nil {
					return err
				}
			}
			token, err = decoder.Token()
			if err != nil {
				return err
			}
			if token != json.Delim('}') {
				return fmt.Errorf("unclosed JSON object")
			}
		case '[':
			for decoder.More() {
				if err := consume(depth + 1); err != nil {
					return err
				}
			}
			token, err = decoder.Token()
			if err != nil {
				return err
			}
			if token != json.Delim(']') {
				return fmt.Errorf("unclosed JSON array")
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter")
		}
		return nil
	}
	if err := consume(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func ValidatePayload(data json.RawMessage) error {
	if err := ValidateJSON(data); err != nil {
		return err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("command payload must be a JSON object")
	}
	return nil
}

// DecodePayload uses the actual application DTO as its single field schema.
func DecodePayload(data json.RawMessage, destination any) error {
	if err := ValidatePayload(data); err != nil {
		return err
	}
	if err := validateDTOFields(data, reflect.TypeOf(destination)); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return nil
}

func DecodeRequest(data []byte) (DispatchRequest, error) {
	var request DispatchRequest
	if err := ValidateJSON(data); err != nil {
		return request, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return request, err
	}
	for key := range fields {
		switch key {
		case "name", "run_ref", "payload":
		default:
			return request, fmt.Errorf("unknown request field %q", key)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	return request, request.Validate()
}
