package nodehost

import "testing"

func TestSanitizeEnv_BlocksPATH(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	env := SanitizeEnv(map[string]string{"PATH": "/tmp/evil:/usr/bin"})
	if env["PATH"] != "/usr/bin" {
		t.Errorf("PATH = %q, want /usr/bin", env["PATH"])
	}
}

func TestSanitizeEnv_BlocksDangerousKeys(t *testing.T) {
	t.Setenv("FOO", "")
	env := SanitizeEnv(map[string]string{
		"PYTHONPATH": "/tmp/pwn",
		"LD_PRELOAD": "/tmp/pwn.so",
		"BASH_ENV":   "/tmp/pwn.sh",
		"SHELLOPTS":  "xtrace",
		"PS4":        "$(touch /tmp/pwned)",
		"FOO":        "bar",
	})
	if env["FOO"] != "bar" {
		t.Errorf("FOO should be preserved: %q", env["FOO"])
	}
	for _, key := range []string{"PYTHONPATH", "LD_PRELOAD", "BASH_ENV", "SHELLOPTS", "PS4"} {
		if _, ok := env[key]; ok {
			t.Errorf("%s should be blocked", key)
		}
	}
}

func TestSanitizeEnv_BlocksOverrideOnlyKeys(t *testing.T) {
	t.Setenv("HOME", "/Users/trusted")
	t.Setenv("ZDOTDIR", "/Users/trusted/.zdot")
	env := SanitizeEnv(map[string]string{
		"HOME":    "/tmp/evil-home",
		"ZDOTDIR": "/tmp/evil-zdotdir",
	})
	if env["HOME"] != "/Users/trusted" {
		t.Errorf("HOME = %q, want /Users/trusted", env["HOME"])
	}
	if env["ZDOTDIR"] != "/Users/trusted/.zdot" {
		t.Errorf("ZDOTDIR = %q, want /Users/trusted/.zdot", env["ZDOTDIR"])
	}
}

func TestSanitizeEnv_DropsDangerousInheritedKeys(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("BASH_ENV", "/tmp/pwn.sh")
	env := SanitizeEnv(nil)
	if env["PATH"] != "/usr/bin:/bin" {
		t.Errorf("PATH should be preserved")
	}
	if _, ok := env["BASH_ENV"]; ok {
		t.Errorf("BASH_ENV should be dropped from inherited env")
	}
}

func TestSanitizeEnv_BlocksDYLDPrefix(t *testing.T) {
	env := SanitizeEnv(map[string]string{
		"DYLD_INSERT_LIBRARIES": "/tmp/evil.dylib",
		"DYLD_LIBRARY_PATH":    "/tmp",
	})
	if _, ok := env["DYLD_INSERT_LIBRARIES"]; ok {
		t.Error("DYLD_INSERT_LIBRARIES should be blocked")
	}
	if _, ok := env["DYLD_LIBRARY_PATH"]; ok {
		t.Error("DYLD_LIBRARY_PATH should be blocked")
	}
}

func TestSanitizeEnv_BlocksLDPrefix(t *testing.T) {
	env := SanitizeEnv(map[string]string{
		"LD_PRELOAD":      "/tmp/evil.so",
		"LD_LIBRARY_PATH": "/tmp",
	})
	if _, ok := env["LD_PRELOAD"]; ok {
		t.Error("LD_PRELOAD should be blocked")
	}
}

func TestSanitizeEnv_PreservesNormalOverrides(t *testing.T) {
	env := SanitizeEnv(map[string]string{
		"MY_VAR":  "hello",
		"LANG":    "en_US.UTF-8",
		"TERM":    "xterm-256color",
	})
	if env["MY_VAR"] != "hello" {
		t.Error("MY_VAR should be preserved")
	}
	if env["LANG"] != "en_US.UTF-8" {
		t.Error("LANG should be preserved")
	}
}

func TestSanitizeSystemRunEnvOverrides_ShellWrapper(t *testing.T) {
	overrides := map[string]string{
		"TERM":       "xterm",
		"LANG":       "en_US.UTF-8",
		"MY_VAR":     "blocked-in-shell",
		"NODE_DEBUG": "should-not-pass",
	}
	result := SanitizeSystemRunEnvOverrides(overrides, true)
	if result["TERM"] != "xterm" {
		t.Error("TERM should pass through for shell wrapper")
	}
	if result["LANG"] != "en_US.UTF-8" {
		t.Error("LANG should pass through for shell wrapper")
	}
	if _, ok := result["MY_VAR"]; ok {
		t.Error("MY_VAR should be blocked in shell wrapper context")
	}
}

func TestSanitizeSystemRunEnvOverrides_NonShell(t *testing.T) {
	overrides := map[string]string{"MY_VAR": "hello", "FOO": "bar"}
	result := SanitizeSystemRunEnvOverrides(overrides, false)
	if result["MY_VAR"] != "hello" || result["FOO"] != "bar" {
		t.Error("non-shell overrides should pass through unchanged")
	}
}

func TestSanitizeSystemRunEnvOverrides_NilInput(t *testing.T) {
	result := SanitizeSystemRunEnvOverrides(nil, false)
	if result != nil {
		t.Error("nil input should return nil")
	}
}
