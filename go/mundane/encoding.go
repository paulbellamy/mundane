package mundane

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Encoding values match the on-disk schema. v1.1 collapsed the former
// {json, text, b64, epoch} set to just two: `json` for structured values
// (SDK steps + sleep wake-times) and `bytes` for raw payloads (shell stdout).
const (
	EncJSON  = "json"
	EncBytes = "bytes"
)

// Kind values.
const (
	KindStep  = "step"
	KindSleep = "sleep"
)

// Status values.
const (
	StatusPending = "pending"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// CheckJSONRoundtrip verifies that v survives a marshal -> unmarshal -> marshal
// cycle unchanged and returns the canonical JSON text. The round-trip goes
// through the concrete type T (not `any`), so struct field order and int64
// precision are preserved — decoding into `any` would sort map keys and route
// integers through float64, spuriously rejecting ordinary values.
func CheckJSONRoundtrip[T any](v T) (string, error) {
	text, err := marshalCanonical(v)
	if err != nil {
		return "", &SerializationError{Detail: err.Error()}
	}
	var back T
	if err := json.Unmarshal([]byte(text), &back); err != nil {
		return "", &SerializationError{Detail: err.Error()}
	}
	reEnc, err := marshalCanonical(back)
	if err != nil {
		return "", &SerializationError{Detail: err.Error()}
	}
	if text != reEnc {
		return "", &SerializationError{Detail: "value does not round-trip through JSON"}
	}
	return text, nil
}

// marshalCanonical encodes v as compact JSON with HTML escaping disabled and no
// trailing newline — the on-disk form for json-encoded rows.
func marshalCanonical(v any) (string, error) {
	enc := &bytes.Buffer{}
	encoder := json.NewEncoder(enc)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return "", err
	}
	return trimTrailingNewline(enc.String()), nil
}

func trimTrailingNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}
	return s
}

// DecodeResult turns an on-disk (encoding, raw) pair into the in-process
// Go value: json -> any (map/slice/number/etc.); bytes -> []byte.
func DecodeResult(encoding string, raw []byte) (any, error) {
	if raw == nil {
		return nil, nil
	}
	switch encoding {
	case EncJSON:
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("decode json: %w", err)
		}
		return v, nil
	case EncBytes:
		return append([]byte(nil), raw...), nil
	}
	return nil, fmt.Errorf("unknown encoding: %q", encoding)
}
