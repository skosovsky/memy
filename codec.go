package memy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

// Codec is a versioned deterministic consumer-type encoding. Encode must not
// depend on map iteration or mutable global state. Decode must reject malformed
// data rather than interpreting it as a zero value.
type Codec[T any] interface {
	Version() string
	Encode(T) ([]byte, error)
	Decode([]byte) (T, error)
}

// JSONCodec implements deterministic JSON with strict decoding and duplicate
// object-key rejection and Unicode scalar validation. Generic numbers decode as
// [json.Number]; explicit numeric fields follow their declared Go type. Consumers
// may supply another codec for their own types.
type JSONCodec[T any] struct{}

// Version identifies the payload representation contract.
func (JSONCodec[T]) Version() string { return "json/v2" }

// Encode canonicalizes map ordering and rejects invalid JSON values.
func (JSONCodec[T]) Encode(value T) ([]byte, error) {
	if err := jsonUnicodeValue(reflect.ValueOf(value), make(map[unicodeVisit]bool)); err != nil {
		return nil, errors.Join(ErrInvalid, err)
	}
	raw, operationErr := json.Marshal(value)
	if operationErr != nil {
		return nil, errors.Join(ErrInvalid, operationErr)
	}
	return canonicalJSON(raw)
}

// Decode rejects malformed JSON, unknown struct fields and duplicate keys.
func (JSONCodec[T]) Decode(raw []byte) (T, error) {
	var value T
	canonical, operationErr := canonicalJSON(raw)
	if operationErr != nil {
		return value, operationErr
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, errors.Join(ErrInvalid, err)
	}
	return value, nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	if !strictWireText(raw) {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, operationErr := jsonValue(decoder)
	if operationErr != nil {
		return nil, errors.Join(ErrInvalid, operationErr)
	}
	if _, trailingErr := decoder.Token(); !errors.Is(trailingErr, io.EOF) {
		return nil, ErrInvalid
	}
	encoded, operationErr := json.Marshal(value)
	if operationErr != nil {
		return nil, errors.Join(ErrInvalid, operationErr)
	}
	return encoded, nil
}

func jsonValue(decoder *json.Decoder) (any, error) {
	token, operationErr := decoder.Token()
	if operationErr != nil {
		return nil, operationErr
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		return jsonObject(decoder)
	case '[':
		return jsonArray(decoder)
	default:
		return nil, ErrInvalid
	}
}

func jsonObject(decoder *json.Decoder) (any, error) {
	object := make(map[string]any)
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, tokenErr
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, ErrInvalid
		}
		if _, exists := object[key]; exists {
			return nil, ErrInvalid
		}
		value, valueErr := jsonValue(decoder)
		if valueErr != nil {
			return nil, valueErr
		}
		object[key] = value
	}
	if err := jsonEnd(decoder, '}'); err != nil {
		return nil, err
	}
	return object, nil
}

func jsonArray(decoder *json.Decoder) (any, error) {
	array := make([]any, 0)
	for decoder.More() {
		value, err := jsonValue(decoder)
		if err != nil {
			return nil, err
		}
		array = append(array, value)
	}
	if err := jsonEnd(decoder, ']'); err != nil {
		return nil, err
	}
	return array, nil
}

func jsonEnd(decoder *json.Decoder, expected json.Delim) error {
	end, operationErr := decoder.Token()
	if operationErr != nil || end != expected {
		return ErrInvalid
	}
	return nil
}

func digest(value any) (string, error) {
	raw, operationErr := json.Marshal(value)
	if operationErr != nil {
		return "", fmt.Errorf("digest: %w", errors.Join(ErrInvalid, operationErr))
	}
	canonical, operationErr := canonicalJSON(raw)
	if operationErr != nil {
		return "", operationErr
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
