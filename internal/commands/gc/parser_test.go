package gc

import (
	"reflect"
	"testing"
)

func TestParse_RecognizedKinds(t *testing.T) {
	cases := []struct {
		message string
		kind    CommandKind
		input   string
		flags   []string
	}{
		{"/gc:plan build a feature", KindPlan, "build a feature", nil},
		{"/gc:fix the flaky test", KindFix, "the flaky test", nil},
		{"/gc:cook implement the plan", KindCook, "implement the plan", nil},
		{"/gc:review the PR", KindReview, "the PR", nil},
		{"/gc:test the parser", KindTest, "the parser", nil},
		{"/gc:debug the timezone bug", KindDebug, "the timezone bug", nil},
		{"/gc:docs update the contract", KindDocs, "update the contract", nil},
		{"/gc:architect the new store", KindArchitect, "the new store", nil},
		{"/gc:uiux review the chat screen", KindUIUX, "review the chat screen", nil},
	}
	for _, tc := range cases {
		cmd, ok := Parse(tc.message)
		if !ok {
			t.Fatalf("%q: expected recognized command", tc.message)
		}
		if cmd.Kind != tc.kind {
			t.Errorf("%q: kind = %q, want %q", tc.message, cmd.Kind, tc.kind)
		}
		if cmd.Input != tc.input {
			t.Errorf("%q: input = %q, want %q", tc.message, cmd.Input, tc.input)
		}
		if !reflect.DeepEqual(cmd.Flags, tc.flags) {
			t.Errorf("%q: flags = %v, want %v", tc.message, cmd.Flags, tc.flags)
		}
	}
}

func TestParse_CaseInsensitivePrefixAndKind(t *testing.T) {
	cmd, ok := Parse("/GC:PLAN build a feature")
	if !ok {
		t.Fatal("expected uppercase /GC:PLAN to be recognized")
	}
	if cmd.Kind != KindPlan {
		t.Errorf("kind = %q, want plan", cmd.Kind)
	}
	if cmd.Input != "build a feature" {
		t.Errorf("input = %q", cmd.Input)
	}

	cmd, ok = Parse("  /Gc:FiX   repair the build  ")
	if !ok {
		t.Fatal("expected mixed-case /Gc:FiX to be recognized")
	}
	if cmd.Kind != KindFix {
		t.Errorf("kind = %q, want fix", cmd.Kind)
	}
	if cmd.Input != "repair the build" {
		t.Errorf("input = %q", cmd.Input)
	}

	cmd, ok = Parse("/GC:UIUX review the chat screen")
	if !ok {
		t.Fatal("expected uppercase /GC:UIUX to be recognized")
	}
	if cmd.Kind != KindUIUX {
		t.Errorf("kind = %q, want uiux", cmd.Kind)
	}
	if cmd.Input != "review the chat screen" {
		t.Errorf("input = %q", cmd.Input)
	}

	cmd, ok = Parse("/Gc:Architect the new store")
	if !ok {
		t.Fatal("expected mixed-case /Gc:Architect to be recognized")
	}
	if cmd.Kind != KindArchitect {
		t.Errorf("kind = %q, want architect", cmd.Kind)
	}
	if cmd.Input != "the new store" {
		t.Errorf("input = %q", cmd.Input)
	}
}

func TestParse_FlagsExtractedFromInput(t *testing.T) {
	cmd, ok := Parse("/gc:plan --deep build a feature")
	if !ok {
		t.Fatal("expected recognized command")
	}
	if !reflect.DeepEqual(cmd.Flags, []string{"--deep"}) {
		t.Errorf("flags = %v, want [--deep]", cmd.Flags)
	}
	if cmd.Input != "build a feature" {
		t.Errorf("input = %q, want %q", cmd.Input, "build a feature")
	}

	cmd, ok = Parse("/gc:fix --fast --strict flaky test")
	if !ok {
		t.Fatal("expected recognized command")
	}
	if !reflect.DeepEqual(cmd.Flags, []string{"--fast", "--strict"}) {
		t.Errorf("flags = %v, want [--fast --strict]", cmd.Flags)
	}
	if cmd.Input != "flaky test" {
		t.Errorf("input = %q, want %q", cmd.Input, "flaky test")
	}

	cmd, ok = Parse("/gc:review --hard")
	if !ok {
		t.Fatal("expected recognized command")
	}
	if !reflect.DeepEqual(cmd.Flags, []string{"--hard"}) {
		t.Errorf("flags = %v, want [--hard]", cmd.Flags)
	}
	if cmd.Input != "" {
		t.Errorf("input = %q, want empty", cmd.Input)
	}
}

func TestParse_FlagsAnywhereInInput(t *testing.T) {
	cmd, ok := Parse("/gc:cook implement plan --deep now")
	if !ok {
		t.Fatal("expected recognized command")
	}
	if !reflect.DeepEqual(cmd.Flags, []string{"--deep"}) {
		t.Errorf("flags = %v, want [--deep]", cmd.Flags)
	}
	if cmd.Input != "implement plan now" {
		t.Errorf("input = %q, want %q", cmd.Input, "implement plan now")
	}
}

func TestParse_UnknownFlagLeftInInput(t *testing.T) {
	cmd, ok := Parse("/gc:plan --urgent build")
	if !ok {
		t.Fatal("expected recognized command")
	}
	if len(cmd.Flags) != 0 {
		t.Errorf("flags = %v, want empty (unknown flag kept in input)", cmd.Flags)
	}
	if cmd.Input != "--urgent build" {
		t.Errorf("input = %q, want %q", cmd.Input, "--urgent build")
	}
}

func TestParse_NoInputStillRecognized(t *testing.T) {
	cmd, ok := Parse("/gc:plan")
	if !ok {
		t.Fatal("expected /gc:plan to be recognized")
	}
	if cmd.Kind != KindPlan || cmd.Input != "" || len(cmd.Flags) != 0 {
		t.Errorf("unexpected command: kind=%q input=%q flags=%v", cmd.Kind, cmd.Input, cmd.Flags)
	}
}

func TestParse_NotACommand(t *testing.T) {
	cases := []string{
		"",
		"hello there",
		"/gc:",
		"/gc:unknown build",
		"/other:plan build",
		"/gc: plan build", // space between prefix and kind
		"/skill plan build",
	}
	for _, msg := range cases {
		if _, ok := Parse(msg); ok {
			t.Errorf("%q: expected passthrough (not a /gc: command)", msg)
		}
	}
}

func TestCommandKind_String(t *testing.T) {
	if got := KindPlan.String(); got != "plan" {
		t.Errorf("KindPlan.String() = %q, want plan", got)
	}
}

func TestCommandKind_Valid(t *testing.T) {
	for _, k := range knownKinds {
		if !k.Valid() {
			t.Errorf("kind %q should be valid", k)
		}
	}
	for _, k := range []CommandKind{"bogus", ""} {
		if k.Valid() {
			t.Errorf("kind %q should be invalid", k)
		}
	}
}