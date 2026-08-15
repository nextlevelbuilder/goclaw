package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Sentinel outcomes. Callers fall back to the cloud vision LLM chain on any of
// these — they distinguish "this parser cannot/should-not handle the document"
// from a genuine extraction success.
var (
	ErrParserUnsupported = errors.New("document_parser: unsupported mime")
	ErrParserUnavailable = errors.New("document_parser: extractor binary not installed")
	ErrParserEmpty       = errors.New("document_parser: extraction produced no usable text")
)

// MIME types the local extractor recognizes.
const (
	mimePDF  = "application/pdf"
	mimeDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

// docTruncationMarker matches the marker read_document.go appends so truncated
// local output reads identically to truncated direct-text output downstream.
const docTruncationMarker = "\n\n[... truncated at 500KB ...]"

// Default config values applied when a field is left zero.
const (
	defaultMaxPages   = 200
	defaultTimeout    = 30 * time.Second
	defaultMinTextLen = 16
	// killGrace is how long a process group has to exit after SIGTERM before
	// it is escalated to SIGKILL.
	killGrace = 3 * time.Second
	// stderrCap bounds captured stderr so a chatty extractor cannot block on a
	// full pipe; only used for error messages.
	stderrCap = 8 * 1024
)

// DocumentParser extracts plain text from a document file. Implementations MUST
// be safe on untrusted input and MUST NOT execute embedded content. A future
// LiteParse backend can satisfy this same interface without touching callers.
type DocumentParser interface {
	// Extract returns plain text, or a sentinel/wrapped error signalling the
	// caller to fall back to the cloud vision path. mime selects the tool.
	Extract(ctx context.Context, path, mime string) (string, error)
	// Supports reports whether this parser handles the mime AND its binary is
	// currently available (LookPath succeeds at call time).
	Supports(mime string) bool
}

// LocalExtractConfig tunes LocalExtractParser. Zero values get sane defaults.
type LocalExtractConfig struct {
	Enabled    bool          // local-first on/off (default OFF — opt-in)
	MaxPages   int           // 0 => 200; passed to pdftotext -l
	Timeout    time.Duration // per-extraction timeout (0 => 30s)
	MinTextLen int           // below this (after trim) => ErrParserEmpty (0 => 16)
}

// LocalExtractParser extracts text from PDF (pdftotext) and DOCX (pandoc) via
// no-shell subprocess exec, with binary detection, process-group timeout kill,
// a minimal environment, and output limits.
type LocalExtractParser struct {
	cfg LocalExtractConfig
	// lookPath is injectable so tests can stub binary discovery without a
	// PATH-prepend race. Defaults to exec.LookPath. Re-resolved per call so a
	// binary installed at runtime is picked up without a restart.
	lookPath func(string) (string, error)
}

// NewLocalExtractParser builds a parser, filling zero-valued config fields with
// defaults. Enabled is taken as given — the construction site owns the toggle.
func NewLocalExtractParser(cfg LocalExtractConfig) *LocalExtractParser {
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = defaultMaxPages
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MinTextLen <= 0 {
		cfg.MinTextLen = defaultMinTextLen
	}
	return &LocalExtractParser{cfg: cfg, lookPath: exec.LookPath}
}

// commandFor maps a mime to its extractor binary name and arguments.
//
// pdftotext takes NO "--" option terminator: poppler's argument parser does not
// treat "--" as one and would consume it as the input-file positional, pushing
// the real path into the output-file slot (silently overwriting the source).
// The path is the sole positional; the trailing "-" routes text to stdout.
//
// pandoc runs with --sandbox so an untrusted DOCX cannot make pandoc fetch
// remote resources or read arbitrary local files during conversion.
func (p *LocalExtractParser) commandFor(mime, path string) (bin string, args []string, err error) {
	switch mime {
	case mimePDF:
		return "pdftotext", []string{"-q", "-enc", "UTF-8", "-l", strconv.Itoa(p.cfg.MaxPages), path, "-"}, nil
	case mimeDOCX:
		return "pandoc", []string{"--sandbox", "-t", "plain", "--wrap=none", path}, nil
	default:
		return "", nil, ErrParserUnsupported
	}
}

// Supports reports whether the mime is handled and its binary is resolvable now.
func (p *LocalExtractParser) Supports(mime string) bool {
	if !p.cfg.Enabled {
		return false
	}
	bin, _, err := p.commandFor(mime, "")
	if err != nil {
		return false
	}
	_, err = p.lookPath(bin)
	return err == nil
}

// Extract runs the extractor for mime against path and returns plain text.
// A timeout or non-zero exit always yields an error (never partial text) so the
// caller falls back rather than treating a half-extracted document as complete.
func (p *LocalExtractParser) Extract(ctx context.Context, path, mime string) (string, error) {
	if !p.cfg.Enabled {
		return "", ErrParserUnsupported
	}
	bin, args, err := p.commandFor(mime, path)
	if err != nil {
		return "", err
	}
	binPath, err := p.lookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrParserUnavailable, bin)
	}

	cmd := exec.Command(binPath, args...) // NOT CommandContext — see kill() below
	setProcessGroup(cmd)
	// Minimal allowlist environment (PATH/HOME/LANG/USER); never inherit the
	// gateway process env, which holds DB DSNs, API keys, and encryption keys.
	cmd.Env = buildCredentialedEnv(nil)

	stdout := &cappedWriter{cap: documentMaxTextBytes}
	stderr := &cappedWriter{cap: stderrCap}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("document_parser: start %s: %w", bin, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(p.cfg.Timeout)
	defer timer.Stop()

	select {
	case werr := <-done:
		if werr != nil {
			return "", fmt.Errorf("document_parser: %s failed: %w (stderr: %s)",
				bin, werr, strings.TrimSpace(stderr.buf.String()))
		}
		// Clean exit 0 — the only path that may return text.
	case <-ctx.Done():
		p.kill(cmd, done)
		return "", fmt.Errorf("document_parser: %s cancelled: %w", bin, ctx.Err())
	case <-timer.C:
		p.kill(cmd, done)
		return "", fmt.Errorf("document_parser: %s timed out after %s", bin, p.cfg.Timeout)
	}

	text := stdout.buf.String()
	if len(strings.TrimSpace(text)) < p.cfg.MinTextLen {
		// Scanned/image-only PDFs yield whitespace; route to vision fallback.
		return "", ErrParserEmpty
	}
	if stdout.overflow {
		text += docTruncationMarker
	}
	return text, nil
}

// kill terminates the whole process group: SIGTERM, a grace period, then
// SIGKILL. Killing the group (not just the direct child) reaps helper children
// that pandoc/pdftotext may fork, so a timeout cannot orphan grandchildren.
func (p *LocalExtractParser) kill(cmd *exec.Cmd, done <-chan error) {
	_ = killProcessGroup(cmd, syscallSIGTERM)
	select {
	case <-done:
	case <-time.After(killGrace):
		_ = killProcessGroup(cmd, syscallSIGKILL)
		<-done
	}
}

// cappedWriter buffers up to cap bytes and silently discards the rest, while
// always reporting a full write. Discarding past the cap keeps the child from
// blocking on a full pipe; overflow records that truncation occurred.
type cappedWriter struct {
	buf      bytes.Buffer
	cap      int
	overflow bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if remaining := w.cap - w.buf.Len(); remaining > 0 {
		if len(p) <= remaining {
			w.buf.Write(p)
		} else {
			w.buf.Write(p[:remaining])
			w.overflow = true
		}
	} else if len(p) > 0 {
		w.overflow = true
	}
	return len(p), nil
}
