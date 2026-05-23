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
	text := trimTrailingNewline(enc.String())

	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return "", &SerializationError{Detail: err.Error()}
	}
	reEnc := &bytes.Buffer{}
	reEncoder := json.NewEncoder(reEnc)
	reEncoder.SetEscapeHTML(false)
	if err := reEncoder.Encode(decoded); err != nil {
		return "", &SerializationError{Detail: err.Error()}
	}
	if text != trimTrailingNewline(reEnc.String()) {
		return "", &SerializationError{Detail: "value does not round-trip through JSON"}
	}
	return text, nil
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
