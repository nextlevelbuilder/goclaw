package providers

import "testing"

func TestStripANSISequences(t *testing.T) {
	in := "\x1b[2K\x1b[GUser Email          a@b.com"
	got := stripANSISequences(in)
	if got != "User Email          a@b.com" {
		t.Fatalf("got %q", got)
	}
}

func TestParseStatusStdoutJSON(t *testing.T) {
	raw := `{"loggedIn":true,"email":"a@b.com"}`
	st := parseStatusStdout(raw)
	if st == nil || !st.LoggedIn || st.Email != "a@b.com" {
		t.Fatalf("got %+v", st)
	}
}

func TestParseStatusStdoutText(t *testing.T) {
	raw := "\n ✓ Logged in as tranchinh0718@gmail.com\n"
	st := parseStatusStdout(stripANSISequences(raw))
	if st == nil || !st.LoggedIn || st.Email != "tranchinh0718@gmail.com" {
		t.Fatalf("got %+v", st)
	}
}

func TestParseAboutText(t *testing.T) {
	raw := "About Cursor CLI\n\nUser Email          user@example.com\n"
	st := parseAboutText(stripANSISequences(raw))
	if st == nil || !st.LoggedIn || st.Email != "user@example.com" {
		t.Fatalf("got %+v", st)
	}
}

func TestParseStatusTextNotLoggedIn(t *testing.T) {
	raw := "Not logged in. Run agent login.\n"
	st := parseStatusText(stripANSISequences(raw))
	if st == nil || st.LoggedIn {
		t.Fatalf("got %+v", st)
	}
}
