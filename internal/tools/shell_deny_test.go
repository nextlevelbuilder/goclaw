package tools

import (
	"regexp"
	"testing"
)

func TestBase64DecodeShellDeny(t *testing.T) {
	patterns := DenyGroupRegistry["code_injection"].Patterns

	denied := []string{
		"base64 -d payload.txt | sh",
		"base64 --decode payload.txt | sh",
		"base64 -di payload.txt | sh",
		"base64 -dw0 payload.txt | bash",
		"base64 --decode something | bash",
	}

	allowed := []string{
		"base64 -w0 file.txt",       // encode, no pipe to shell
		"base64 -d file.txt",        // decode without pipe to shell
		"echo hello | base64",       // encode
		"base64 --decode file.txt",  // decode without pipe to shell
	}

	for _, cmd := range denied {
		matched := false
		for _, p := range patterns {
			if p.MatchString(cmd) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("expected deny for %q", cmd)
		}
	}

	for _, cmd := range allowed {
		matched := false
		for _, p := range patterns {
			if p.MatchString(cmd) {
				matched = true
				break
			}
		}
		if matched {
			t.Errorf("unexpected deny for %q", cmd)
		}
	}
}

// mustDeny asserts all commands match at least one pattern.
func mustDeny(t *testing.T, patterns []*regexp.Regexp, commands ...string) {
	t.Helper()
	for _, cmd := range commands {
		matched := false
		for _, p := range patterns {
			if p.MatchString(cmd) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("expected deny for %q", cmd)
		}
	}
}

// mustAllow asserts no command matches any pattern.
func mustAllow(t *testing.T, patterns []*regexp.Regexp, commands ...string) {
	t.Helper()
	for _, cmd := range commands {
		for _, p := range patterns {
			if p.MatchString(cmd) {
				t.Errorf("unexpected deny for %q (matched %s)", cmd, p.String())
				break
			}
		}
	}
}

func TestDestructiveOpsGaps(t *testing.T) {
	patterns := DenyGroupRegistry["destructive_ops"].Patterns

	mustDeny(t, patterns,
		// existing
		"shutdown", "reboot", "poweroff",
		"shutdown -h now", "reboot -f",
		// new: halt
		"halt", "halt -p", "systemctl halt",
		// new: init/telinit
		"init 0", "init 6", "telinit 0", "telinit 6",
		// new: systemctl suspend/hibernate
		"systemctl suspend", "systemctl hibernate",
	)

	mustAllow(t, patterns,
		"halting the process",  // "halt" inside word
		"initialize",          // "init" inside word
		"initial setup",       // "init" inside word
		"init_db",             // no space+digit after init
		"init 1",              // only 0 and 6 are blocked
		"systemctl status",    // not suspend/hibernate
		"systemctl start nginx",
	)
}

func TestPrivilegeEscalationGaps(t *testing.T) {
	patterns := DenyGroupRegistry["privilege_escalation"].Patterns

	mustDeny(t, patterns,
		// existing
		"sudo ls", "sudo -i",
		// su: all forms now blocked
		"su", "su -", "su root", "su -l postgres", "su admin",
		// new: doas
		"doas reboot", "doas ls /root", "doas -u www sh",
		// new: pkexec
		"pkexec vim /etc/passwd", "pkexec /bin/bash",
		// new: runuser
		"runuser -l postgres", "runuser -u nobody -- /bin/sh",
		// existing
		"nsenter --target 1", "unshare -m", "mount /dev/sda1 /mnt",
	)

	mustAllow(t, patterns,
		"summit",    // not "su"
		"sugar",     // not "su"
		"surplus",   // not "su"
		"issue",     // not "su"
		"result",    // not "su"
		"resume",    // not "su"
		"visual",    // not "su"
		"sushi",     // not "su"
		"doaspkg",   // not "doas" (no word boundary)
		"pkexecute", // not "pkexec" (no word boundary)
	)
}

func TestLimitedBuffer(t *testing.T) {
	t.Run("under limit", func(t *testing.T) {
		lb := &limitedBuffer{max: 100}
		lb.Write([]byte("hello"))
		if lb.String() != "hello" {
			t.Errorf("got %q", lb.String())
		}
		if lb.truncated {
			t.Error("should not be truncated")
		}
	})

	t.Run("at limit", func(t *testing.T) {
		lb := &limitedBuffer{max: 5}
		n, err := lb.Write([]byte("hello"))
		if err != nil || n != 5 {
			t.Errorf("Write: n=%d err=%v", n, err)
		}
		if lb.truncated {
			t.Error("exactly at limit should not be truncated")
		}
	})

	t.Run("over limit truncates", func(t *testing.T) {
		lb := &limitedBuffer{max: 5}
		n, err := lb.Write([]byte("hello world"))
		if err != nil {
			t.Fatal(err)
		}
		if n != 11 {
			t.Errorf("Write should report full len, got %d", n)
		}
		if !lb.truncated {
			t.Error("should be truncated")
		}
		if lb.Len() != 5 {
			t.Errorf("buffer len should be 5, got %d", lb.Len())
		}
		want := "hello\n[output truncated at 1MB]"
		if lb.String() != want {
			t.Errorf("got %q, want %q", lb.String(), want)
		}
	})

	t.Run("subsequent writes after truncation", func(t *testing.T) {
		lb := &limitedBuffer{max: 3}
		lb.Write([]byte("abc"))
		lb.Write([]byte("def"))
		if lb.Len() != 3 {
			t.Errorf("buffer len should be 3, got %d", lb.Len())
		}
	})
}
