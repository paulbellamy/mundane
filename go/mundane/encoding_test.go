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

func TestCheckJSONRoundtripTyped(t *testing.T) {
	// Multi-field struct with non-alphabetical field order must round-trip
	// (decoding into `any` would sort keys and spuriously reject it).
	type User struct {
		Email string
		Name  string
		ID    int64
	}
	if _, err := CheckJSONRoundtrip(User{"a@b.c", "alice", 7}); err != nil {
		t.Errorf("struct round-trip: %v", err)
	}
	// int64 beyond 2^53 must round-trip through int64 without float precision loss.
	if _, err := CheckJSONRoundtrip(int64(9007199254740993)); err != nil {
		t.Errorf("large int64 round-trip: %v", err)
	}
}

func TestParseEpochTolerant(t *testing.T) {
	cases := map[string]int64{
		"1700000000000":     1700000000000,
		"1700000000000.0":   1700000000000,
		" 1700000000000 \n": 1700000000000,
		"1.7e12":            1700000000000,
	}
	for in, want := range cases {
		got, err := parseEpoch([]byte(in))
		if err != nil {
			t.Errorf("parseEpoch(%q): unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseEpoch(%q) = %d, want %d", in, got, want)
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
