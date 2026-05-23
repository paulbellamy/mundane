package mundane

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
)

// Encoding values match the on-disk schema.
const (
	EncJSON  = "json"
	EncText  = "text"
	EncB64   = "b64"
	EncEpoch = "epoch"
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

// CheckJSONRoundtrip verifies that v survives json.Marshal -> json.Unmarshal
// -> deep-equal, matching the Py/TS first-write contract. Returns the JSON
// text on success.
func CheckJSONRoundtrip(v any) (string, error) {
	enc := &bytes.Buffer{}
	encoder := json.NewEncoder(enc)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return "", &SerializationError{Detail: err.Error()}
	}
	text := enc.String()
	// Encoder appends a trailing newline; strip for storage parity with Py/TS.
	if len(text) > 0 && text[len(text)-1] == '\n' {
		text = text[:len(text)-1]
	}

	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return "", &SerializationError{Detail: err.Error()}
	}
	// Re-encode the decoded value via the same path so we compare apples to
	// apples (json decodes numbers to float64, etc. — checking the *original*
	// can disagree on type even when the JSON round-trip itself is faithful).
	// Strategy: re-marshal v, marshal decoded, compare text.
	reEnc := &bytes.Buffer{}
	reEncoder := json.NewEncoder(reEnc)
	reEncoder.SetEscapeHTML(false)
	if err := reEncoder.Encode(decoded); err != nil {
		return "", &SerializationError{Detail: err.Error()}
	}
	reText := reEnc.String()
	if len(reText) > 0 && reText[len(reText)-1] == '\n' {
		reText = reText[:len(reText)-1]
	}
	if text != reText {
		return "", &SerializationError{Detail: "value does not round-trip through JSON"}
	}
	return text, nil
}

// DecodeResult turns an on-disk (encoding, raw) pair into the in-process
// Go value: JSON -> any (map/slice/etc.); text -> string; b64 -> []byte;
// epoch -> int64.
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
	case EncText:
		return string(raw), nil
	case EncB64:
		dec, err := base64.StdEncoding.DecodeString(string(raw))
		if err != nil {
			return nil, fmt.Errorf("decode b64: %w", err)
		}
		return dec, nil
	case EncEpoch:
		var n int64
		for _, c := range raw {
			if c < '0' || c > '9' {
				return nil, fmt.Errorf("epoch payload not numeric: %q", raw)
			}
			n = n*10 + int64(c-'0')
		}
		return n, nil
	}
	return nil, fmt.Errorf("unknown encoding: %q", encoding)
}

// B64Decode is a convenience export for the CLI inspect path.
func B64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// DeepEqualJSON compares two Go values for "would they produce the same JSON",
// stricter than reflect.DeepEqual for the cases that matter to mundane.
func DeepEqualJSON(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return reflect.DeepEqual(a, b)
	}
	return bytes.Equal(ja, jb)
}
