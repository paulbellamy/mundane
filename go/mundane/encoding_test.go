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

func TestDecodeResultEpoch(t *testing.T) {
	v, err := DecodeResult(EncEpoch, []byte("1700000000000"))
	if err != nil {
		t.Fatal(err)
	}
	if v.(int64) != 1700000000000 {
		t.Errorf("epoch: %v", v)
	}
}

func TestDecodeResultB64(t *testing.T) {
	v, err := DecodeResult(EncB64, []byte("aGVsbG8="))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v.([]byte), []byte("hello")) {
		t.Errorf("b64: %v", v)
	}
}
