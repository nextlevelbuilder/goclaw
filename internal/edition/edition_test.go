package edition

import (
	"testing"
)

// TestCurrent_ReturnsValidEdition verifies the default edition is valid.
func TestCurrent_ReturnsValidEdition(t *testing.T) {
	e := Current()

	if e.Name == "" {
		t.Error("Current() returned edition with empty name")
	}

	// Should be one of the known presets
	if e.Name != "standard" && e.Name != "lite" {
		t.Errorf("Current().Name = %q; want 'standard' or 'lite'", e.Name)
	}
}

// TestSetCurrent_CanUpdateEdition verifies edition can be changed.
func TestSetCurrent_CanUpdateEdition(t *testing.T) {
	original := Current()
	defer SetCurrent(original) // restore after test

	// Switch to Lite
	SetCurrent(Lite)
	e := Current()

	if e.Name != "lite" {
		t.Errorf("After SetCurrent(Lite), Current().Name = %q; want 'lite'", e.Name)
	}
	if e.MaxAgents != 5 {
		t.Errorf("After SetCurrent(Lite), MaxAgents = %d; want 5", e.MaxAgents)
	}

	// Switch to Standard
	SetCurrent(Standard)
	e = Current()

	if e.Name != "standard" {
		t.Errorf("After SetCurrent(Standard), Current().Name = %q; want 'standard'", e.Name)
	}
	if e.MaxAgents != 0 {
		t.Errorf("After SetCurrent(Standard), MaxAgents = %d; want 0 (unlimited)", e.MaxAgents)
	}
}

// TestIsLimited_CorrectlyIdentifiesLimitedEditions.
func TestIsLimited_CorrectlyIdentifiesLimitedEditions(t *testing.T) {
	tests := []struct {
		name     string
		edition  Edition
		wantBool bool
	}{
		{
			name:     "Standard has no limits",
			edition:  Standard,
			wantBool: false,
		},
		{
			name:     "Lite has limits",
			edition:  Lite,
			wantBool: true,
		},
		{
			name:     "Custom edition with MaxAgents is limited",
			edition:  Edition{Name: "custom", MaxAgents: 10},
			wantBool: true,
		},
		{
			name:     "Custom edition with MaxTeams is limited",
			edition:  Edition{Name: "custom", MaxTeams: 5},
			wantBool: true,
		},
		{
			name:     "Custom edition with no limits is not limited",
			edition:  Edition{Name: "custom"},
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.edition.IsLimited()
			if result != tt.wantBool {
				t.Errorf("%s: IsLimited() = %v; want %v", tt.name, result, tt.wantBool)
			}
		})
	}
}

// TestChannelLimit_ReturnsCorrectLimits tests channel-specific limits.
func TestChannelLimit_ReturnsCorrectLimits(t *testing.T) {
	tests := []struct {
		name        string
		edition     Edition
		channelType string
		wantLimit   int
	}{
		{
			name:        "Standard has no channel limit for Telegram",
			edition:     Standard,
			channelType: "telegram",
			wantLimit:   0,
		},
		{
			name:        "Standard has no channel limit for Discord",
			edition:     Standard,
			channelType: "discord",
			wantLimit:   0,
		},
		{
			name:        "Lite limits Telegram to 1",
			edition:     Lite,
			channelType: "telegram",
			wantLimit:   1,
		},
		{
			name:        "Lite limits Discord to 1",
			edition:     Lite,
			channelType: "discord",
			wantLimit:   1,
		},
		{
			name:        "Lite has no limit for unknown channel",
			edition:     Lite,
			channelType: "slack",
			wantLimit:   0,
		},
		{
			name:        "Edition with nil MaxChannels returns 0 for all",
			edition:     Edition{Name: "test", MaxChannels: nil},
			channelType: "telegram",
			wantLimit:   0,
		},
		{
			name:        "Edition with empty MaxChannels returns 0 for all",
			edition:     Edition{Name: "test", MaxChannels: map[string]int{}},
			channelType: "telegram",
			wantLimit:   0,
		},
		{
			name:        "Custom edition with channel limits",
			edition:     Edition{Name: "custom", MaxChannels: map[string]int{"whatsapp": 2, "slack": 3}},
			channelType: "whatsapp",
			wantLimit:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.edition.ChannelLimit(tt.channelType)
			if result != tt.wantLimit {
				t.Errorf("ChannelLimit(%q) = %d; want %d", tt.channelType, result, tt.wantLimit)
			}
		})
	}
}

// TestStandardEditionFeatures verifies all Standard edition features are enabled.
func TestStandardEditionFeatures(t *testing.T) {
	e := Standard

	tests := []struct {
		name     string
		got      bool
		want     bool
		property string
	}{
		{
			name:     "KGEnabled",
			got:      e.KGEnabled,
			want:     true,
			property: "KGEnabled",
		},
		{
			name:     "RBACEnabled",
			got:      e.RBACEnabled,
			want:     true,
			property: "RBACEnabled",
		},
		{
			name:     "TeamFullMode",
			got:      e.TeamFullMode,
			want:     true,
			property: "TeamFullMode",
		},
		{
			name:     "VectorSearch",
			got:      e.VectorSearch,
			want:     true,
			property: "VectorSearch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Standard.%s = %v; want %v", tt.property, tt.got, tt.want)
			}
		})
	}
}

// TestLiteEditionFeatures verifies Lite edition constraints.
func TestLiteEditionFeatures(t *testing.T) {
	e := Lite

	tests := []struct {
		name     string
		got      int
		want     int
		property string
	}{
		{
			name:     "MaxAgents = 5",
			got:      e.MaxAgents,
			want:     5,
			property: "MaxAgents",
		},
		{
			name:     "MaxTeams = 1",
			got:      e.MaxTeams,
			want:     1,
			property: "MaxTeams",
		},
		{
			name:     "MaxTeamMembers = 5",
			got:      e.MaxTeamMembers,
			want:     5,
			property: "MaxTeamMembers",
		},
		{
			name:     "MaxSubagentConcurrent = 2",
			got:      e.MaxSubagentConcurrent,
			want:     2,
			property: "MaxSubagentConcurrent",
		},
		{
			name:     "MaxSubagentDepth = 1",
			got:      e.MaxSubagentDepth,
			want:     1,
			property: "MaxSubagentDepth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Lite.%s = %d; want %d", tt.property, tt.got, tt.want)
			}
		})
	}

	// Test disabled features
	featureTests := []struct {
		name     string
		got      bool
		want     bool
		property string
	}{
		{
			name:     "KGEnabled = false",
			got:      e.KGEnabled,
			want:     false,
			property: "KGEnabled",
		},
		{
			name:     "RBACEnabled = false",
			got:      e.RBACEnabled,
			want:     false,
			property: "RBACEnabled",
		},
		{
			name:     "TeamFullMode = false",
			got:      e.TeamFullMode,
			want:     false,
			property: "TeamFullMode",
		},
		{
			name:     "VectorSearch = false",
			got:      e.VectorSearch,
			want:     false,
			property: "VectorSearch",
		},
	}

	for _, tt := range featureTests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Lite.%s = %v; want %v", tt.property, tt.got, tt.want)
			}
		})
	}
}

// TestLiteEditionChannelLimits verifies Lite channel constraints.
func TestLiteEditionChannelLimits(t *testing.T) {
	e := Lite

	tests := []struct {
		name        string
		channelType string
		wantLimit   int
	}{
		{
			name:        "telegram limited to 1",
			channelType: "telegram",
			wantLimit:   1,
		},
		{
			name:        "discord limited to 1",
			channelType: "discord",
			wantLimit:   1,
		},
		{
			name:        "other channels not limited",
			channelType: "slack",
			wantLimit:   0,
		},
		{
			name:        "whatsapp not limited",
			channelType: "whatsapp",
			wantLimit:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := e.ChannelLimit(tt.channelType)
			if result != tt.wantLimit {
				t.Errorf("Lite.ChannelLimit(%q) = %d; want %d", tt.channelType, result, tt.wantLimit)
			}
		})
	}
}

// TestEditionConcurrentSafety verifies SetCurrent/Current are thread-safe.
func TestEditionConcurrentSafety(t *testing.T) {
	original := Current()
	defer SetCurrent(original)

	// This is a basic sanity test; full concurrency testing would require more setup.
	done := make(chan bool)

	// Goroutine 1: repeatedly read
	go func() {
		for i := 0; i < 100; i++ {
			_ = Current()
		}
		done <- true
	}()

	// Goroutine 2: repeatedly write
	go func() {
		editions := []Edition{Standard, Lite, Standard}
		for _, e := range editions {
			SetCurrent(e)
		}
		done <- true
	}()

	<-done
	<-done
	// If this completes without panic, the test passes
}

// TestCustomEdition_PartialConfiguration allows custom editions.
func TestCustomEdition_PartialConfiguration(t *testing.T) {
	custom := Edition{
		Name:         "custom",
		MaxAgents:    20,
		MaxTeams:     3,
		KGEnabled:    true,
		RBACEnabled:  false,
		TeamFullMode: true,
	}

	if custom.IsLimited() != true {
		t.Error("Custom edition with MaxAgents should be limited")
	}

	if custom.ChannelLimit("telegram") != 0 {
		t.Error("Custom edition with no channel limits should return 0")
	}
}
