package telegram

import "testing"

// EditMessage must recognize the "no text to edit" error to fall back to caption editing.
func TestNoTextToEditRegex(t *testing.T) {
	yes := `telego: editMessageText: api: 400 "Bad Request: there is no text in the message to edit"`
	if !noTextToEditRe.MatchString(yes) {
		t.Error("expected match for 'no text in the message to edit'")
	}
	if noTextToEditRe.MatchString("Bad Request: message can't be edited") {
		t.Error("unrelated edit error must not match")
	}
}
