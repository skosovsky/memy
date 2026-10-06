package memy_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/conformance"
)

type rawJSONOutput string

func (r rawJSONOutput) MarshalJSON() ([]byte, error) { return []byte(r), nil }

type textOutput int

func (textOutput) MarshalText() ([]byte, error) { return []byte{0xff}, nil }

type namedByte byte

func (namedByte) MarshalText() ([]byte, error) { return []byte{0xff}, nil }

type pointerByte byte

func (*pointerByte) MarshalText() ([]byte, error) { return []byte{0xff}, nil }

type pointerText string

func (p *pointerText) MarshalText() ([]byte, error) { return []byte(*p), nil }

func TestJSONCodecGenericConformance(t *testing.T) {
	// Arrange: numeric lexemes are explicit host values, including source-shaped data.
	samples := []any{
		json.Number("9007199254740993"),
		json.Number("9223372036854775807"),
		map[string]any{
			"nested": []any{json.Number("-9223372036854775808"), json.Number("1.2300"), json.Number("1e99")},
		},
		map[string]any{"reference": map[string]any{"number": json.Number("1E-99"), "text": "�😀"}},
	}
	malformed := [][]byte{[]byte(`{"a":1,"a":2}`), []byte(`"\ud800"`), []byte(`"\udc00"`), {'"', 0xff, '"'}}
	// Act / Assert: common suite independently verifies bytes and typed round trips.
	conformance.CodecSuite(t, memy.JSONCodec[any]{}, samples, reflect.DeepEqual, malformed)
}

func TestJSONCodecUnicodeEncode(t *testing.T) {
	invalid := string([]byte{0xff})
	pointer := pointerText(invalid)
	for name, value := range map[string]any{
		"string": invalid, "nested": []any{map[string]any{"value": invalid}},
		"key": map[string]int{invalid: 1}, "raw_surrogate": json.RawMessage(`"\ud800"`),
		"raw_utf8":    json.RawMessage([]byte{'"', 0xff, '"'}),
		"custom_json": rawJSONOutput(`{"key":"\udc00"}`), "custom_text": textOutput(1),
		"custom_key": map[textOutput]int{1: 2}, "pointer_text": &pointer, "custom_byte_value": []namedByte{1}, "custom_byte_pointer": []pointerByte{1},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange / Act.
			raw, err := (memy.JSONCodec[any]{}).Encode(value)
			// Assert: replacement must not produce accepted bytes.
			if !errors.Is(err, memy.ErrInvalid) || len(raw) != 0 {
				t.Fatalf("accepted %q: %v", raw, err)
			}
		})
	}
}

func TestJSONCodecUnicodeDecode(t *testing.T) {
	for _, raw := range [][]byte{
		{'"', 0xff, '"'}, []byte(`{"\ud800":1}`), []byte(`{"a":["\udc00"]}`),
		[]byte(`"\ud800x"`), []byte(`"\ud800\u0041"`),
	} {
		// Arrange / Act.
		_, err := (memy.JSONCodec[any]{}).Decode(raw)
		// Assert.
		if !errors.Is(err, memy.ErrInvalid) {
			t.Fatalf("accepted %q: %v", raw, err)
		}
	}
	for _, raw := range []string{`"�"`, `"😀"`, `"\ud83d\ude00"`, `"\\ud800"`} {
		// Arrange / Act.
		value, err := (memy.JSONCodec[string]{}).Decode([]byte(raw))
		// Assert: valid replacement scalar and escaped backslash are real data.
		if err != nil || value == "" {
			t.Fatalf("rejected %s: %v", raw, err)
		}
	}
}

func TestJSONCodecTypedNumbers(t *testing.T) {
	// Arrange.
	type numbers struct {
		Integer int64   `json:"integer"`
		Decimal float64 `json:"decimal"`
	}
	codec := memy.JSONCodec[numbers]{}
	// Act.
	got, err := codec.Decode([]byte(`{"integer":9007199254740993,"decimal":1.25}`))
	// Assert.
	if err != nil || got.Integer != 9007199254740993 || got.Decimal != 1.25 {
		t.Fatalf("%+v: %v", got, err)
	}
}

func TestJSONCodecOwnership(t *testing.T) {
	// Arrange.
	codec := memy.JSONCodec[map[string]any]{}
	raw := []byte(`{"items":[{"n":9007199254740993}]}`)
	first, err := codec.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := codec.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Act: mutate caller input and one decoded tree independently.
	raw[0] = '!'
	first["items"].([]any)[0].(map[string]any)["n"] = json.Number("0")
	encoded, err := codec.Encode(second)
	if err != nil {
		t.Fatal(err)
	}
	encoded[0] = '!'
	again, err := codec.Encode(second)
	// Assert.
	if err != nil || string(again) != `{"items":[{"n":9007199254740993}]}` {
		t.Fatalf("aliasing: %s %v", again, err)
	}
}

func TestJSONCodecConcurrentNumbers(t *testing.T) {
	// Arrange.
	codec := memy.JSONCodec[any]{}
	raw := []byte(`{"n":9007199254740993,"s":"😀"}`)
	var workers sync.WaitGroup
	// Act: share only immutable input and the stateless codec.
	for range 8 {
		workers.Go(func() {
			value, err := codec.Decode(raw)
			if err != nil {
				t.Error(err)
				return
			}
			encoded, err := codec.Encode(value)
			// Assert.
			if err != nil || !bytes.Equal(encoded, raw) {
				t.Errorf("roundtrip: %s %v", encoded, err)
			}
		})
	}
	workers.Wait()
}

func FuzzJSONCodecGenericNumbers(f *testing.F) {
	for _, raw := range []string{`9007199254740993`, `{"a":[1.2300,1e99,-9223372036854775808]}`, `"\ud800"`, `"😀"`} {
		f.Add([]byte(raw))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		// Arrange / Act.
		generic := memy.JSONCodec[any]{}
		value, err := generic.Decode(raw)
		if err != nil {
			return
		}
		encoded, err := generic.Encode(value)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := (memy.JSONCodec[json.RawMessage]{}).Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		// Assert: generic decode cannot round accepted numeric lexemes.
		if !bytes.Equal(encoded, expected) {
			t.Fatalf("changed representation: %s -> %s", expected, encoded)
		}
	})
}

func TestJSONCodecSharedSliceUnicode(t *testing.T) {
	// Arrange: the same backing array has distinct visible ranges.
	values := []string{"valid", string([]byte{0xff})}
	payload := []any{values[:1], values[:2]}
	// Act.
	_, err := (memy.JSONCodec[any]{}).Encode(payload)
	// Assert: visiting the shorter alias cannot skip the longer string range.
	if !errors.Is(err, memy.ErrInvalid) {
		t.Fatalf("shared slice accepted: %v", err)
	}
}

func TestJSONCodecRepresentationBoundaries(t *testing.T) {
	// Arrange.
	type hidden struct {
		Visible string `json:"visible"`
		Ignored string `json:"-"`
	}
	codec := memy.JSONCodec[any]{}
	// Act / Assert: binary bytes and excluded fields are not Unicode strings on wire.
	for _, value := range []any{[]byte{0xff}, hidden{Visible: "valid", Ignored: string([]byte{0xff})}, (*pointerText)(nil), map[*pointerText]int{nil: 1}} {
		if _, err := codec.Encode(value); err != nil {
			t.Fatalf("rejected representation: %v", err)
		}
	}
	cycle := make(map[string]any)
	cycle["self"] = cycle
	if _, err := codec.Encode(cycle); !errors.Is(err, memy.ErrInvalid) {
		t.Fatalf("cycle accepted: %v", err)
	}
}

func TestJSONCodecOwnershipConformance(t *testing.T) {
	// Arrange / Act / Assert: host mutation exercises the common codec suite.
	conformance.CodecSuite(
		t,
		memy.JSONCodec[map[string]any]{},
		[]map[string]any{{"items": []any{map[string]any{"n": json.Number("9007199254740993")}}}},
		func(a, b map[string]any) bool { return reflect.DeepEqual(a, b) },
		[][]byte{[]byte(`{`)},
		func(value map[string]any) {
			value["items"].([]any)[0].(map[string]any)["n"] = json.Number("0")
		},
	)
}
