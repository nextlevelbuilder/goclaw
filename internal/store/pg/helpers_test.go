package pg

import (
	"testing"
)

func TestPqStringArray(t *testing.T) {
	tests := []struct {
		name string
		arr  []string
		want string
	}{
		{"nil returns nil", nil, ""},
		{"simple elements", []string{"a", "b"}, `{"a","b"}`},
		{"element with comma", []string{"a,b", "c"}, `{"a,b","c"}`},
		{"element with quote", []string{`say "hi"`}, `{"say \"hi\""}`},
		{"element with backslash", []string{`a\b`}, `{"a\\b"}`},
		{"empty slice", []string{}, `{}`},
		{"empty string element", []string{""}, `{""}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pqStringArray(tt.arr)
			if tt.arr == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			s, ok := got.(string)
			if !ok {
				t.Fatalf("expected string, got %T", got)
			}
			if s != tt.want {
				t.Errorf("got %q, want %q", s, tt.want)
			}
		})
	}
}

func TestScanStringArray(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want []string
	}{
		{"nil data", nil, nil},
		{"empty braces", []byte("{}"), nil},
		{"simple unquoted", []byte("{a,b,c}"), []string{"a", "b", "c"}},
		{"quoted with comma", []byte(`{"a,b","c"}`), []string{"a,b", "c"}},
		{"quoted with escaped quote", []byte(`{"say \"hi\"","ok"}`), []string{`say "hi"`, "ok"}},
		{"quoted with backslash", []byte(`{"a\\b"}`), []string{`a\b`}},
		{"mixed quoted unquoted", []byte(`{foo,"bar,baz",qux}`), []string{"foo", "bar,baz", "qux"}},
		{"single element", []byte("{hello}"), []string{"hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			scanStringArray(tt.data, &got)
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v (len %d), want %v (len %d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPqStringArrayRoundtrip(t *testing.T) {
	// Verify encode→decode roundtrip for tricky values.
	inputs := [][]string{
		{"simple"},
		{"a,b", "c"},
		{`say "hello"`, `back\slash`},
		{"", "empty"},
	}

	for _, arr := range inputs {
		encoded := pqStringArray(arr).(string)
		var decoded []string
		scanStringArray([]byte(encoded), &decoded)
		if len(decoded) != len(arr) {
			t.Fatalf("roundtrip len mismatch: input %v, encoded %q, decoded %v", arr, encoded, decoded)
		}
		for i := range arr {
			if decoded[i] != arr[i] {
				t.Errorf("roundtrip mismatch at %d: input %q, got %q (encoded: %q)", i, arr[i], decoded[i], encoded)
			}
		}
	}
}
