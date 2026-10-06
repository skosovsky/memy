package memy

import (
	"encoding"
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"
)

type unicodeVisit struct {
	kind    reflect.Type
	pointer uintptr
	length  int
}

// jsonUnicodeValue checks text before encoding/json can replace invalid UTF-8.
// Custom callbacks own their representation; validate their emitted text instead
// of inspecting hidden fields. Determinism is the host's admission obligation.
func jsonUnicodeValue(value reflect.Value, seen map[unicodeVisit]bool) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return jsonUnicodeValue(value.Elem(), seen)
	}
	if jsonUnicodeSeen(value, seen) {
		return nil
	}
	if handled, err := jsonUnicodeCustom(value); handled {
		return err
	}
	switch value.Kind() {
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return ErrInvalid
		}
	case reflect.Pointer:
		return jsonUnicodeValue(value.Elem(), seen)
	case reflect.Map:
		return jsonUnicodeMap(value, seen)
	case reflect.Struct:
		return jsonUnicodeStruct(value, seen)
	case reflect.Slice, reflect.Array:
		return jsonUnicodeArray(value, seen)
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer:
		// Nontext values are admitted or rejected by encoding/json itself.
	}
	return nil
}

func jsonUnicodeCustom(value reflect.Value) (bool, error) {
	if value.CanAddr() && value.Addr().CanInterface() {
		if handled, err := jsonUnicodeMarshaler(value.Addr().Interface()); handled {
			return true, err
		}
	}
	if value.CanInterface() {
		return jsonUnicodeMarshaler(value.Interface())
	}
	return false, nil
}

func jsonUnicodeMarshaler(value any) (bool, error) {
	if marshaler, ok := value.(json.Marshaler); ok {
		raw, err := marshaler.MarshalJSON()
		if err != nil {
			return true, err
		}
		if !strictWireText(raw) {
			return true, ErrInvalid
		}
		return true, nil
	}
	if marshaler, ok := value.(encoding.TextMarshaler); ok {
		raw, err := marshaler.MarshalText()
		if err != nil {
			return true, err
		}
		if !utf8.Valid(raw) {
			return true, ErrInvalid
		}
		return true, nil
	}
	return false, nil
}

func jsonUnicodeMap(value reflect.Value, seen map[unicodeVisit]bool) error {
	entries := value.MapRange()
	for entries.Next() {
		key := entries.Key()
		if err := jsonUnicodeKey(key); err != nil {
			return err
		}
		if err := jsonUnicodeValue(entries.Value(), seen); err != nil {
			return err
		}
	}
	return nil
}

func jsonUnicodeStruct(value reflect.Value, seen map[unicodeVisit]bool) error {
	for i := range value.NumField() {
		field := value.Type().Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if !utf8.ValidString(name) {
			return ErrInvalid
		}
		if err := jsonUnicodeValue(value.Field(i), seen); err != nil {
			return err
		}
	}
	return nil
}

func jsonUnicodeSeen(value reflect.Value, seen map[unicodeVisit]bool) bool {
	kind := value.Kind()
	if kind != reflect.Pointer && kind != reflect.Map && kind != reflect.Slice {
		return false
	}
	if value.IsNil() {
		return true
	}
	length := 0
	if kind == reflect.Slice {
		length = value.Len()
	}
	visit := unicodeVisit{kind: value.Type(), pointer: uintptr(value.UnsafePointer()), length: length}
	if seen[visit] {
		return true
	}
	seen[visit] = true
	return false
}

func jsonUnicodeArray(value reflect.Value, seen map[unicodeVisit]bool) error {
	// Byte slices have a base64 representation, not a Unicode string domain.
	if jsonUnicodeBinarySlice(value) {
		return nil
	}
	for i := range value.Len() {
		if err := jsonUnicodeValue(value.Index(i), seen); err != nil {
			return err
		}
	}
	return nil
}

func jsonUnicodeKey(key reflect.Value) error {
	// JSON map keys prefer their string kind to TextMarshaler, unlike values.
	if key.Kind() == reflect.String {
		if !utf8.ValidString(key.String()) {
			return ErrInvalid
		}
		return nil
	}
	if key.Kind() == reflect.Pointer && key.IsNil() {
		return nil
	}
	if !key.CanInterface() {
		return nil
	}
	marshaler, ok := reflect.TypeAssert[encoding.TextMarshaler](key)
	if !ok {
		return nil
	}
	raw, err := marshaler.MarshalText()
	if err != nil {
		return err
	}
	if !utf8.Valid(raw) {
		return ErrInvalid
	}
	return nil
}

func jsonUnicodeBinarySlice(value reflect.Value) bool {
	if value.Kind() != reflect.Slice || value.Type().Elem().Kind() != reflect.Uint8 {
		return false
	}
	pointer := reflect.PointerTo(value.Type().Elem())
	return !pointer.Implements(reflect.TypeFor[json.Marshaler]()) &&
		!pointer.Implements(reflect.TypeFor[encoding.TextMarshaler]())
}
