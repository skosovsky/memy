package memy_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/skosovsky/memy"
)

func FuzzJSONCodec(f *testing.F) {
	for _, seed := range []string{`{"a":1}`, `{"a":1,"a":2}`, `null`, `[true, "data"]`, `1e99`, `{"n":9007199254740993}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		// Arrange.
		codec := memy.JSONCodec[json.RawMessage]{}
		// Act: valid input must have a stable encode/decode fixed point.
		value, transactionErr := codec.Decode(raw)
		if transactionErr != nil {
			return
		}
		encoded, transactionErr := codec.Encode(value)
		if transactionErr != nil {
			t.Fatal(transactionErr)
		}
		again, transactionErr := codec.Decode(encoded)
		if transactionErr != nil {
			t.Fatal(transactionErr)
		}
		reencoded, transactionErr := codec.Encode(again)
		if transactionErr != nil {
			t.Fatal(transactionErr)
		}
		// Assert.
		if string(encoded) != string(reencoded) {
			t.Fatal("canonical encoding is unstable")
		}
	})
}

func TestJSONCodecCanonicalMapOrder(t *testing.T) {
	// Arrange.
	codec := memy.JSONCodec[map[string]int]{}
	a := map[string]int{"b": 2, "a": 1}
	b := make(map[string]int)
	b["a"] = 1
	b["b"] = 2
	// Act.
	left, operationErr := codec.Encode(a)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	right, operationErr := codec.Encode(b)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert.
	if string(left) != `{"a":1,"b":2}` || string(left) != string(right) {
		t.Fatalf("nondeterministic: %s / %s", left, right)
	}
}

func TestJSONCodecRejectMalformed(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"nested":{"a":1,"a":2}}`, `{"a":1} {}`, `{"a":`, ``, `[1,]`} {
		t.Run(raw, func(t *testing.T) {
			// Arrange / Act.
			_, transactionErr := (memy.JSONCodec[map[string]int]{}).Decode([]byte(raw))
			// Assert.
			if !errors.Is(transactionErr, memy.ErrInvalid) {
				t.Fatalf("malformed accepted: %v", transactionErr)
			}
		})
	}
	// Arrange / Act.
	_, operationErr := (memy.JSONCodec[float64]{}).Encode(math.NaN())
	// Assert.
	if !errors.Is(operationErr, memy.ErrInvalid) {
		t.Fatalf("NaN accepted: %v", operationErr)
	}
}
