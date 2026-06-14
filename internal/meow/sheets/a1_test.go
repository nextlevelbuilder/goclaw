package sheets

import "testing"

func TestColLetter(t *testing.T) {
	cases := map[int]string{0: "A", 1: "B", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA"}
	for idx, want := range cases {
		if got := colLetter(idx); got != want {
			t.Errorf("colLetter(%d) = %q, want %q", idx, got, want)
		}
	}
}

func TestA1Quoting(t *testing.T) {
	if got := a1Range("ton-slot"); got != "'ton-slot'" {
		t.Errorf("a1Range = %q", got)
	}
	if got := a1Cell("ton-slot", "B2"); got != "'ton-slot'!B2" {
		t.Errorf("a1Cell = %q", got)
	}
	if got := a1Range("a'b"); got != "'a''b'" {
		t.Errorf("a1Range quote-escape = %q", got)
	}
}

func TestHeaderIndex(t *testing.T) {
	headers := []string{" Date ", "STATUS", "manager_approved"}
	if i, ok := headerIndex(headers, "status"); !ok || i != 1 {
		t.Errorf("headerIndex(status) = %d,%v", i, ok)
	}
	if _, ok := headerIndex(headers, "nope"); ok {
		t.Error("absent header should not match")
	}
}

func TestToStrings(t *testing.T) {
	got := toStrings([]any{" hi ", 123, true})
	want := []string{"hi", "123", "true"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("toStrings[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
