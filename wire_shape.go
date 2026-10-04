package memy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"time"
)

// requiredJSONFields verifies shape before decoding zero values. Persisted
// structs serialize every field; omission is corruption, never a default.
func requiredJSONFields(raw []byte, shape reflect.Type) error {
	if shape.Kind() == reflect.Pointer {
		if bytes.Equal(raw, []byte("null")) {
			return nil
		}
		return requiredJSONFields(raw, shape.Elem())
	}
	if shape.Kind() != reflect.Slice && bytes.Equal(raw, []byte("null")) {
		return ErrSchema
	}
	if shape == reflect.TypeFor[time.Time]() {
		return requiredTime(raw)
	}
	if shape.Kind() == reflect.Slice && shape.Elem().Kind() == reflect.Uint8 &&
		shape != reflect.TypeFor[json.RawMessage]() {
		return requiredBase64(raw)
	}
	if shape.Kind() == reflect.Struct {
		return requiredJSONObject(raw, shape)
	}
	if shape.Kind() == reflect.Slice && shape.Elem().Kind() != reflect.Uint8 {
		var elements []json.RawMessage
		if err := json.Unmarshal(raw, &elements); err != nil {
			return ErrSchema
		}
		for _, element := range elements {
			if err := requiredJSONFields(element, shape.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func requiredJSONObject(raw []byte, shape reflect.Type) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return ErrSchema
	}
	for field := range shape.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !field.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		value, present := fields[name]
		delete(fields, name)
		if !present {
			return ErrSchema
		}
		if err := requiredJSONFields(value, field.Type); err != nil {
			return err
		}
	}
	if len(fields) != 0 {
		return ErrSchema
	}
	return nil
}

func requiredBase64(raw []byte) error {
	if bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if len(raw) == 0 || raw[0] != '"' {
		return ErrSchema
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return ErrSchema
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.StdEncoding.EncodeToString(decoded) != encoded {
		return ErrSchema
	}
	return nil
}

func requiredTime(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return ErrSchema
	}
	const profile = `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`
	matched, err := regexp.MatchString(profile, text)
	if err != nil || !matched {
		return ErrSchema
	}
	if _, err = time.Parse(time.RFC3339Nano, text); err != nil {
		return ErrSchema
	}
	return nil
}
