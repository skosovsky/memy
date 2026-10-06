package memy

import (
	"reflect"
	"strconv"
	"unicode/utf8"
)

// metadataStringsValid is used only on library-owned envelopes. Payload and
// reference values have already been encoded as opaque bytes by their codecs.
func metadataStringsValid(value any) bool {
	return metadataValueValid(reflect.ValueOf(value), "")
}

func metadataValueValid(value reflect.Value, field string) bool {
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return true
		}
		return metadataValueValid(value.Elem(), field)
	case reflect.String:
		return metadataTextValid(value.String(), field)
	case reflect.Struct:
		for i := range value.NumField() {
			f := value.Type().Field(i)
			if f.IsExported() && !metadataValueValid(value.Field(i), f.Name) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return true
		}
		for i := range value.Len() {
			if !metadataValueValid(value.Index(i), field) {
				return false
			}
		}
	case reflect.Invalid,
		reflect.Bool,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr,
		reflect.Float32,
		reflect.Float64,
		reflect.Complex64,
		reflect.Complex128,
		reflect.Chan,
		reflect.Func,
		reflect.Map,
		reflect.UnsafePointer:
		// Non-text scalar and opaque values contain no metadata strings.
	}
	return true
}

func metadataTextValid(text, field string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	switch field {
	case "ID",
		"RecordID",
		"ProposalID",
		"OperationID",
		"Actor",
		"PolicyVersion",
		"AuthorityPolicyVersion",
		"ProviderVersion",
		"PayloadCodec",
		"ReferenceCodec",
		"Codec",
		"Extractor",
		"Sink",
		"Name",
		"Backend",
		"Purpose",
		"Tenant",
		"Namespace",
		"Subject":
		return text == "" || validIdentifier(text)
	case "Revision", "Records": // source revision, not the numeric record revision
		return validIdentifier(text)
	}
	return true
}

// strictWireText rejects inputs that encoding/json would silently repair.
// In particular, an unpaired escaped UTF-16 surrogate is not literal U+FFFD.
func strictWireText(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	quoted := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		end, valid := strictUnicodeEscape(raw, i)
		if !valid {
			return false
		}
		i = end
	}
	return true
}

func strictUnicodeEscape(raw []byte, i int) (int, bool) {
	if i+4 >= len(raw) {
		return i, false
	}
	code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
	if err != nil || (code >= 0xdc00 && code <= 0xdfff) {
		return i, false
	}
	i += 4
	if code < 0xd800 || code > 0xdbff {
		return i, true
	}
	if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
		return i, false
	}
	low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
	return i + escapedSurrogateTailLength, err == nil && low >= 0xdc00 && low <= 0xdfff
}

const escapedSurrogateTailLength = 6
