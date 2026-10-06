package kv

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/skosovsky/memy"
)

// Binding identifies an exact scoped snapshot; its secret is owned by the store.
const maxCursorBytes = 32768

type Binding struct {
	Secret     []byte
	Scope      string
	Generation memy.Version
}

type cursor struct {
	Scope      string       `json:"scope"`
	Prefix     []byte       `json:"prefix"`
	Plan       []byte       `json:"plan"`
	Generation memy.Version `json:"generation"`
	Last       []byte       `json:"last"`
	After      []byte       `json:"after"`
}

func (b Binding) Resume(options memy.ScanOptions) (string, error) {
	if options.Limit < 1 || options.Limit > 1024 || options.MaxBytes <= 0 || len(options.Prefix) > 4096 ||
		len(options.After) > 4096 ||
		len(options.Plan) > 1024 ||
		strings.ContainsRune(options.Prefix, '\x00') ||
		strings.ContainsRune(options.After, '\x00') {
		return "", memy.ErrInvalid
	}
	if options.Cursor == "" {
		return options.After, nil
	}
	if len(options.Cursor) > maxCursorBytes {
		return "", memy.ErrInvalid
	}
	parts := strings.Split(options.Cursor, ".")
	if len(parts) != 2 {
		return "", memy.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", memy.ErrInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", memy.ErrInvalid
	}
	mac := hmac.New(sha256.New, b.Secret)
	_, _ = mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", memy.ErrStaleCursor
	}
	var c cursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return "", memy.ErrInvalid
	}
	if c.Scope != b.Scope || string(c.Prefix) != options.Prefix || string(c.After) != options.After ||
		len(c.Last) == 0 ||
		len(c.Last) > 4096 ||
		!strings.HasPrefix(string(c.Last), options.Prefix) {
		return "", memy.ErrInvalid
	}
	if c.Generation != b.Generation || string(c.Plan) != options.Plan {
		return "", memy.ErrStaleCursor
	}
	return string(c.Last), nil
}

func (b Binding) Next(options memy.ScanOptions, last string) string {
	raw, _ := json.Marshal(
		cursor{
			Scope:      b.Scope,
			Prefix:     []byte(options.Prefix),
			After:      []byte(options.After),
			Plan:       []byte(options.Plan),
			Generation: b.Generation,
			Last:       []byte(last),
		},
	)
	mac := hmac.New(sha256.New, b.Secret)
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
