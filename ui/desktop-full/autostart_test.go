//go:build windows

package main

import "testing"

func TestAutostartCommandAndParse(t *testing.T) {
	const exe = `C:\Program Files\GoClaw\GoClawDesktop.exe`
	cases := []struct {
		minimized bool
		want      string
	}{
		{false, `"C:\Program Files\GoClaw\GoClawDesktop.exe" --background`},
		{true, `"C:\Program Files\GoClaw\GoClawDesktop.exe" --minimized`},
	}
	for _, c := range cases {
		got := autostartCommand(exe, c.minimized)
		if got != c.want {
			t.Errorf("command(minimized=%v) = %q, want %q", c.minimized, got, c.want)
		}
		enabled, minimized := parseAutostart(got)
		if !enabled || minimized != c.minimized {
			t.Errorf("parse(%q) = %v, %v", got, enabled, minimized)
		}
	}
	if enabled, minimized := parseAutostart(""); enabled || minimized {
		t.Error("an empty value must read as disabled")
	}
}
