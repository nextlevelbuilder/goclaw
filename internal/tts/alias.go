// Package tts is a thin backward-compat alias over internal/audio.
//
// The real implementation lives in internal/audio (TTS + STT + Music + SFX
// unified manager). This file preserves the legacy 24-symbol surface so that
// pre-Phase-1 callers continue to compile unchanged. New code should import
// internal/audio directly — the aliases here will be removed after all
// callers migrate.
//
// Contract: every exported symbol that existed in the pre-Phase-1 tts package
// has an alias or constructor below. If you add a new exported symbol to
// internal/audio that previously lived in tts, also add it here — and to the
// alias_test.go symbol-coverage test.
package tts

import (
	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/edge"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
	"github.com/nextlevelbuilder/goclaw/internal/audio/openai"
)

// --- Types (15) ---

type Manager = audio.Manager
type Provider = audio.TTSProvider
type Options = audio.TTSOptions
type SynthResult = audio.SynthResult
type AutoMode = audio.AutoMode
type Mode = audio.Mode
type ManagerConfig = audio.ManagerConfig
type EdgeConfig = edge.Config
type EdgeProvider = edge.Provider
type ElevenLabsConfig = elevenlabs.Config
type ElevenLabsProvider = elevenlabs.TTSProvider
type MiniMaxConfig = minimax.Config
type MiniMaxProvider = minimax.Provider
type OpenAIConfig = openai.Config
type OpenAIProvider = openai.Provider

// --- Constants (6) ---

const (
	AutoOff     = audio.AutoOff
	AutoAlways  = audio.AutoAlways
	AutoInbound = audio.AutoInbound
	AutoTagged  = audio.AutoTagged
	ModeFinal   = audio.ModeFinal
	ModeAll     = audio.ModeAll
)

// --- Constructors (5) ---

var (
	NewManager            = audio.NewManager
	NewEdgeProvider       = edge.NewProvider
	NewElevenLabsProvider = elevenlabs.NewTTSProvider
	NewMiniMaxProvider    = minimax.NewProvider
	NewOpenAIProvider     = openai.NewProvider
)

// --- Compile-time signature guards (5) ---
//
// These catch silent argument-type divergence when an audio.* constructor is
// refactored. Go's package-level `var x = fn` elides signature checks — these
// explicit typed assignments restore them.

var (
	_ func(audio.ManagerConfig) *audio.Manager        = NewManager
	_ func(edge.Config) *edge.Provider                = NewEdgeProvider
	_ func(elevenlabs.Config) *elevenlabs.TTSProvider = NewElevenLabsProvider
	_ func(minimax.Config) *minimax.Provider          = NewMiniMaxProvider
	_ func(openai.Config) *openai.Provider            = NewOpenAIProvider
)
