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
		text := value.String()
		if !utf8.ValidString(text) {
			return false
		}
		switch field {
		case "ID", "RecordID", "ProposalID", "OperationID", "Actor", "PolicyVersion", "AuthorityPolicyVersion", "ProviderVersion", "PayloadCodec", "ReferenceCodec", "Codec", "Extractor", "Sink", "Name", "Backend", "Purpose", "Tenant", "Namespace", "Subject":
			return text == "" || validIdentifier(text)
		case "Revision", "Records": // source revision, not the numeric record revision
			return validIdentifier(text)
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			f := value.Type().Field(i)
			if f.IsExported() && !metadataValueValid(value.Field(i), f.Name) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return true
		}
		for i := 0; i < value.Len(); i++ {
			if !metadataValueValid(value.Index(i), field) {
				return false
			}
		}
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
		if i+4 >= len(raw) {
			return false
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
