package mundane

import (
	"bytes"
	"testing"
)

func TestCheckJSONRoundtrip(t *testing.T) {
	good := []any{
		nil,
		true,
		42,
		3.14,
		"hello",
		[]any{1, 2, 3},
		map[string]any{"k": "v", "n": 1.0},
		[]any{map[string]any{"x": 1.0}, "y"},
	}
	for _, v := range good {
		if _, err := CheckJSONRoundtrip(v); err != nil {
			t.Errorf("CheckJSONRoundtrip(%v) = %v, want ok", v, err)
		}
	}
}

func TestDecodeResultJSONNumber(t *testing.T) {
	v, err := DecodeResult(EncJSON, []byte("1700000000000"))
	if err != nil {
		t.Fatal(err)
	}
	// json numbers decode to float64; the sleep path parses raw bytes instead.
	if v.(float64) != 1700000000000 {
		t.Errorf("json number: %v", v)
	}
}

func TestDecodeResultBytes(t *testing.T) {
	v, err := DecodeResult(EncBytes, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v.([]byte), []byte("hello")) {
		t.Errorf("bytes: %v", v)
	}
}
