package kv

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"

	"github.com/skosovsky/memy"
)

const cursorVersion byte = 2
const maximumKeyBytes = 4096
const cursorGenerationBytes = 8
const cursorLengthBytes = 2
const cursorHeaderBytes = 1 + sha256.Size + cursorGenerationBytes + cursorLengthBytes

// Raw base64url uses ceil(8*n/6) characters, without padding.
const cursorByteBits = 8
const cursorBase64Bits = 6
const maxCursorBytes = ((cursorHeaderBytes+maximumKeyBytes)*cursorByteBits+cursorBase64Bits-1)/cursorBase64Bits +
	1 + (sha256.Size*cursorByteBits+cursorBase64Bits-1)/cursorBase64Bits

// Binding identifies an exact scoped snapshot; its secret is owned by the store.
type Binding struct {
	Secret     []byte
	Scope      string
	Generation memy.Version
}

func (b Binding) Resume(options memy.ScanOptions) (string, error) {
	if err := validScanOptions(options); err != nil {
		return "", err
	}
	if options.Cursor == "" {
		return options.After, nil
	}
	raw, err := b.cursorPayload(options.Cursor)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", memy.ErrInvalid
	}
	if raw[0] != cursorVersion {
		return "", memy.ErrStaleCursor
	}
	if len(raw) < cursorHeaderBytes {
		return "", memy.ErrInvalid
	}
	expected := b.scanBinding(options)
	if !hmac.Equal(raw[1:1+sha256.Size], expected[:]) {
		return "", memy.ErrStaleCursor
	}
	offset := 1 + sha256.Size
	generation := binary.BigEndian.Uint64(raw[offset : offset+cursorGenerationBytes])
	if generation != uint64(b.Generation) {
		return "", memy.ErrStaleCursor
	}
	length := int(binary.BigEndian.Uint16(raw[cursorHeaderBytes-cursorLengthBytes : cursorHeaderBytes]))
	last := string(raw[cursorHeaderBytes:])
	if length < 1 || length > maximumKeyBytes || length != len(last) || strings.ContainsRune(last, '\x00') ||
		!strings.HasPrefix(last, options.Prefix) || last <= options.After {
		return "", memy.ErrInvalid
	}
	return last, nil
}

func (b Binding) cursorPayload(cursor string) ([]byte, error) {
	if len(cursor) > maxCursorBytes {
		return nil, memy.ErrInvalid
	}
	encoded, signature, found := strings.Cut(cursor, ".")
	if !found {
		return nil, memy.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return nil, memy.ErrInvalid
	}
	macBytes, err := base64.RawURLEncoding.Strict().DecodeString(signature)
	if err != nil || len(macBytes) != sha256.Size || base64.RawURLEncoding.EncodeToString(macBytes) != signature {
		return nil, memy.ErrInvalid
	}
	if !hmac.Equal(macBytes, b.signCursor(raw)) {
		return nil, memy.ErrStaleCursor
	}
	return raw, nil
}

func (b Binding) Next(options memy.ScanOptions, last string) (string, error) {
	if err := validScanOptions(options); err != nil {
		return "", err
	}
	length := len(last)
	if length == 0 || length > maximumKeyBytes {
		return "", memy.ErrInvalid
	}
	if strings.ContainsRune(last, '\x00') ||
		!strings.HasPrefix(last, options.Prefix) || last <= options.After {
		return "", memy.ErrInvalid
	}
	binding := b.scanBinding(options)
	raw := make([]byte, 0, cursorHeaderBytes+len(last))
	raw = append(raw, cursorVersion)
	raw = append(raw, binding[:]...)
	raw = binary.BigEndian.AppendUint64(raw, uint64(b.Generation))
	raw = binary.BigEndian.AppendUint16(raw, uint16(length))
	raw = append(raw, last...)
	return base64.RawURLEncoding.EncodeToString(
		raw,
	) + "." + base64.RawURLEncoding.EncodeToString(
		b.signCursor(raw),
	), nil
}

func (b Binding) scanBinding(options memy.ScanOptions) [sha256.Size]byte {
	hash := sha256.New()
	for _, value := range []string{b.Scope, options.Prefix, options.After, options.Plan} {
		var length [cursorGenerationBytes]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func (b Binding) signCursor(raw []byte) []byte {
	mac := hmac.New(sha256.New, b.Secret)
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

func validScanOptions(options memy.ScanOptions) error {
	if options.Limit < 1 || options.Limit > 1024 || options.MaxBytes <= 0 || len(options.Prefix) > maximumKeyBytes ||
		len(options.After) > maximumKeyBytes || len(options.Plan) > 1024 ||
		strings.ContainsRune(options.Prefix, '\x00') || strings.ContainsRune(options.After, '\x00') {
		return memy.ErrInvalid
	}
	return nil
}
