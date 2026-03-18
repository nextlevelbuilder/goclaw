package tools

import (
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
