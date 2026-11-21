package workcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

var jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

// encoding/json accepts case-insensitive aliases, which would let {"key":...,
// "KEY":...} write the same DTO field twice despite distinct raw JSON keys.
// Derive exact names from the real DTO, never a parallel request schema.
func validateDTOFields(data []byte, typ reflect.Type) error {
	if typ == nil {
		return nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if reflect.PointerTo(typ).Implements(jsonUnmarshalerType) {
		return nil
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		if data[0] != '{' {
			return nil
		} // The actual decoder diagnoses type mismatches.
		fields := map[string]reflect.Type{}
		var collect func(reflect.Type)
		collect = func(t reflect.Type) {
			for i := 0; i < t.NumField(); i++ {
				field := t.Field(i)
				if field.PkgPath != "" {
					continue
				}
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				if name == "-" {
					continue
				}
				nested := field.Type
				for nested.Kind() == reflect.Pointer {
					nested = nested.Elem()
				}
				if field.Anonymous && name == "" && nested.Kind() == reflect.Struct {
					collect(nested)
					continue
				}
				if name == "" {
					name = field.Name
				}
				fields[name] = field.Type
			}
		}
		collect(typ)
		var values map[string]json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for name, value := range values {
			field, known := fields[name]
			if !known {
				return fmt.Errorf("unknown field %q", name)
			}
			if err := validateDTOFields(value, field); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if data[0] != '[' {
			return nil
		}
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := validateDTOFields(value, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		if data[0] != '{' {
			return nil
		}
		var values map[string]json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := validateDTOFields(value, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
